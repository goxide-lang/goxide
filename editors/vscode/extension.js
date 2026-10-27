"use strict";

const fs = require("node:fs/promises");
const path = require("node:path");
const { settingNames, contains, projectPlan, launchOptions, documentSelector, owns, watched } = require("./config");
const { Development } = require("./development");

const generatedScheme = "goxide-generated";
const goExtensionIDs = ["golang.go", "golang.go-nightly"];
let active;

class Extension {
  constructor(vscode, LanguageClient) {
    this.vscode = vscode;
    this.LanguageClient = LanguageClient;
    this.sessions = new Map();
    this.previews = new Map();
    this.disposables = [];
    this.pending = Promise.resolve();
    this.disposed = false;
    this.warnedGo = false;
    this.lastGoConflict = this.conflict();
    this.output = vscode.window.createOutputChannel("Goxide", { log: true });
    this.previewChanged = new vscode.EventEmitter();
    this.development = new Development(this);
    this.disposables.push(this.output, this.previewChanged);
  }

  async start() {
    const { vscode } = this;
    this.disposables.push(
      ...this.development.register(),
      vscode.workspace.registerTextDocumentContentProvider(generatedScheme, {
        onDidChange: this.previewChanged.event,
        provideTextDocumentContent: (uri, token) => this.generatedContent(uri, token),
      }),
      vscode.commands.registerCommand("goxide.restart", () => this.queue(() => this.restart())),
      vscode.commands.registerCommand("goxide.generatedSource", () => this.showGenerated()),
      vscode.commands.registerCommand("goxide.enableManagedGo", () => this.enableManagedGo()),
      vscode.workspace.onDidOpenTextDocument(document => this.queue(() => this.openDocument(document))),
      vscode.workspace.onDidCloseTextDocument(document => {
        if (document.uri.scheme === generatedScheme) this.previews.delete(document.uri.toString());
        else this.queue(() => this.closeDocument(document));
      }),
      vscode.workspace.onDidChangeWorkspaceFolders(() => this.queue(() => this.restart())),
      vscode.workspace.onDidChangeConfiguration(event => {
        if (event.affectsConfiguration("goxide")) this.queue(() => this.restart(true));
      }),
      vscode.workspace.onDidRenameFiles(event => {
        for (const session of this.sessions.values()) {
          const changes = event.files.flatMap(file => [
            { uri: file.oldUri, type: 3 }, { uri: file.newUri, type: 1 },
          ]).filter(change => change.uri.scheme === "file" && watched(session.plan, change.uri.fsPath));
          this.sendFiles(session, changes);
        }
      }),
      vscode.extensions.onDidChange(() => {
        const conflict = this.conflict();
        if (this.lastGoConflict !== conflict) {
          this.lastGoConflict = conflict;
          this.queue(() => this.restart());
        }
      }),
      vscode.workspace.onDidGrantWorkspaceTrust(() => this.queue(() => this.discover())),
    );
    await this.queue(() => this.discover());
  }

  queue(work) {
    this.pending = this.pending.then(() => this.disposed ? undefined : work()).catch(error => {
      this.output.appendLine(String(error?.stack || error));
      void this.vscode.window.showErrorMessage(`Goxide: ${error.message || error}`);
    });
    return this.pending;
  }

  settings(uri) {
    const config = this.vscode.workspace.getConfiguration("goxide", uri);
    return Object.fromEntries(settingNames.map(name => [name, config.get(name)]));
  }

  conflict() {
    return goExtensionIDs.some(id => this.vscode.extensions.getExtension(id));
  }

  async discover() {
    if (!this.vscode.workspace.isTrusted) return;
    for (const folder of this.vscode.workspace.workspaceFolders || []) await this.openFolder(folder);
    for (const document of this.vscode.workspace.textDocuments) await this.openDocument(document);
  }

  async openFolder(folder) {
    if (folder.uri.scheme !== "file") return;
    const settings = this.settings(folder.uri);
    if (!settings.enabled) return;
    if (settings.input || await exists(path.join(folder.uri.fsPath, "goxide.toml"))) {
      await this.openPlan(projectPlan(folder.uri.fsPath, settings.input), folder, true);
    }
  }

  async openDocument(document) {
    if (!this.vscode.workspace.isTrusted || document.uri.scheme !== "file" || document.languageId !== "goxide") return;
    if (this.sessionFor(document.uri)) return;
    const folder = this.vscode.workspace.getWorkspaceFolder(document.uri);
    const settings = this.settings(folder?.uri || document.uri);
    if (!settings.enabled) return;
    if (folder) {
      await this.openFolder(folder);
      if (this.sessionFor(document.uri) || settings.input) return;
      // Only inspect ancestors of an opened file; no workspace tree scanning.
      for (let root = path.dirname(document.uri.fsPath); contains(folder.uri.fsPath, root); root = path.dirname(root)) {
        if (await exists(path.join(root, "goxide.toml"))) {
          await this.openPlan(projectPlan(root, "."), folder);
          return;
        }
        if (root === folder.uri.fsPath || path.dirname(root) === root) break;
      }
    }
    await this.openPlan(projectPlan(path.dirname(document.uri.fsPath), document.uri.fsPath, true), folder);
  }

  async openPlan(plan, folder, persistent = false) {
    if (this.sessions.has(plan.input)) return;
    for (const previous of this.sessions.values()) {
      if (!previous.plan.singleFile && (contains(previous.plan.root, plan.root) || contains(plan.root, previous.plan.root))) {
        this.output.appendLine(`Skipping overlapping Goxide project ${plan.input}; ${previous.plan.input} already owns its source tree.`);
        return;
      }
    }
    // A discovered project replaces standalone sessions already opened below
    // it, so the same buffer never belongs to two Goxide clients.
    if (!plan.singleFile) {
      for (const previous of [...this.sessions.values()]) {
        if (previous.plan.singleFile && contains(plan.root, previous.plan.input)) await this.stopSession(previous);
      }
    }
    const { vscode } = this;
    const rootURI = vscode.Uri.file(plan.root);
    const settings = this.settings(folder?.uri || rootURI);
    const goConflict = this.conflict();
    const managedGo = settings.enableManagedGo && !goConflict && settings.gopls !== "off";
    const options = launchOptions(folder?.uri.fsPath || plan.root, plan, settings, goConflict);
    const session = { plan, folder, settings, persistent, managedGo, client: undefined, disposables: [] };
    const client = new this.LanguageClient("goxide", `Goxide (${path.basename(plan.root)})`, options, {
      documentSelector: documentSelector(vscode, plan, managedGo),
      workspaceFolder: { uri: rootURI, name: folder?.name || path.basename(plan.root), index: folder?.index || 0 },
      outputChannel: this.output,
      markdown: { isTrusted: false },
    });
    session.client = client;
    try {
      await client.start();
      const watcher = vscode.workspace.createFileSystemWatcher(new vscode.RelativePattern(plan.root, "**/*"));
      session.disposables.push(watcher,
        watcher.onDidCreate(uri => this.fileChanged(session, uri, 1)),
        watcher.onDidChange(uri => this.fileChanged(session, uri, 2)),
        watcher.onDidDelete(uri => this.fileChanged(session, uri, 3)),
      );
      this.sessions.set(plan.input, session);
    } catch (error) {
      this.sessions.delete(plan.input);
      for (const disposable of session.disposables) disposable.dispose();
      await client.stop().catch(() => {});
      throw error;
    }
    if (goConflict && !this.warnedGo) {
      this.warnedGo = true;
      void vscode.window.showWarningMessage(
        "Goxide is serving .gox source features with its Go backend off because the Go extension is enabled. Disable Go for this workspace, then run Goxide: Enable Managed Go for This Project to share .go files and Go-backed navigation.",
        "Show Go Extensions",
      ).then(choice => {
        if (choice) void vscode.commands.executeCommand("workbench.extensions.search", "@installed go");
      });
    }
  }

  fileChanged(session, uri, type) {
    if (uri.scheme !== "file" || !watched(session.plan, uri.fsPath)) return;
    // Open source buffers already use didChange; saving unchanged text must
    // not force a second project reload. Create/delete still affect topology.
    if (type === 2 && /\.(gox|go)$/.test(uri.fsPath) &&
      owns(session.plan, uri.fsPath) && this.vscode.workspace.textDocuments.some(document =>
        document.uri.toString() === uri.toString() &&
        (document.languageId === "goxide" || session.managedGo && document.languageId === "go"))) return;
    this.sendFiles(session, [{ uri, type }]);
    if (type !== 2 && path.basename(uri.fsPath) === "goxide.toml") this.queue(() => this.restart());
  }

  sendFiles(session, changes) {
    if (!changes.length || this.disposed) return;
    void session.client.sendNotification("workspace/didChangeWatchedFiles", {
      changes: changes.map(change => ({ uri: change.uri.toString(), type: change.type })),
    }).catch(error => this.output.appendLine(String(error)));
  }

  sessionFor(uri) {
    if (uri.scheme === generatedScheme) return this.previews.get(uri.toString());
    if (uri.scheme !== "file") return undefined;
    return [...this.sessions.values()].filter(session => owns(session.plan, uri.fsPath))
      .sort((a, b) => b.plan.root.length - a.plan.root.length)[0];
  }

  async closeDocument(document) {
    const session = this.sessionFor(document.uri);
    if (!session || session.persistent) return;
    const stillOpen = this.vscode.workspace.textDocuments.some(other =>
      other.uri.scheme === "file" && other.uri.toString() !== document.uri.toString() && owns(session.plan, other.uri.fsPath));
    if (!stillOpen) await this.stopSession(session);
  }

  async generatedContent(uri, token) {
    const known = this.previews.get(uri.toString());
    for (const session of known ? [known] : this.sessions.values()) {
      let result;
      try {
        result = await session.client.sendRequest("goxide/generatedSource", { uri: uri.toString() }, token);
      } catch (error) {
        if (known || token?.isCancellationRequested) throw error;
        continue;
      }
      if (result && typeof result.content === "string") {
        this.previews.set(uri.toString(), session);
        return result.content;
      }
    }
    throw new Error("This generated preview is no longer available. Reopen it from the Goxide source file.");
  }

  async showGenerated() {
    const uri = this.vscode.window.activeTextEditor?.document.uri;
    try {
      if (uri?.scheme === generatedScheme && !this.sessionFor(uri)) await this.generatedContent(uri);
      const session = uri && this.sessionFor(uri);
      if (!session) return this.vscode.window.showInformationMessage("Open a Goxide source file before showing generated Go.");
      const result = await session.client.sendRequest("goxide/generatedSource", { uri: uri.toString() });
      if (!result || typeof result.content !== "string" || typeof result.uri !== "string") {
        throw new Error("Generated Go is unavailable for the current source.");
      }
      const generated = this.vscode.Uri.parse(result.uri);
      if (generated.scheme !== generatedScheme) throw new Error("The language server returned an invalid preview URI.");
      this.previews.set(generated.toString(), session);
      this.previewChanged.fire(generated);
      const document = await this.vscode.workspace.openTextDocument(generated);
      const goDocument = await this.vscode.languages.setTextDocumentLanguage(document, "go");
      await this.vscode.window.showTextDocument(goDocument, { preview: true, viewColumn: this.vscode.ViewColumn.Beside });
    } catch (error) {
      void this.vscode.window.showErrorMessage(`Goxide: ${error.message || error}`);
    }
  }

  async enableManagedGo() {
    const uri = this.vscode.window.activeTextEditor?.document.uri;
    const session = uri && this.sessionFor(uri);
    if (!session?.plan.managed || !session.folder) {
      return this.vscode.window.showInformationMessage("Open a workspace with goxide.toml or configure goxide.input for a project directory first.");
    }
    if (this.conflict()) {
      const choice = await this.vscode.window.showWarningMessage(
        "Disable the Go extension for this workspace before enabling Goxide-managed Go. Goxide will not change your Go settings.", "Show Go Extensions");
      if (choice) await this.vscode.commands.executeCommand("workbench.extensions.search", "@installed go");
      return;
    }
    if (session.settings.gopls === "off") {
      return this.vscode.window.showInformationMessage("Configure goxide.gopls before enabling Goxide-managed Go.");
    }
    await this.vscode.workspace.getConfiguration("goxide", session.folder.uri)
      .update("enableManagedGo", true, this.vscode.ConfigurationTarget.WorkspaceFolder);
  }

  async restart(configurationChanged = false) {
    for (const session of [...this.sessions.values()]) {
      if (configurationChanged) {
        await session.client.sendNotification("workspace/didChangeConfiguration", {
          settings: { goxide: this.settings(session.folder?.uri || this.vscode.Uri.file(session.plan.root)) },
        }).catch(() => {});
      }
      await this.stopSession(session);
    }
    this.sessions.clear();
    this.previews.clear();
    await this.discover();
  }

  async stopSession(session) {
    this.sessions.delete(session.plan.input);
    for (const [uri, owner] of this.previews) if (owner === session) this.previews.delete(uri);
    for (const disposable of session.disposables) disposable.dispose();
    await session.client.stop().catch(error => this.output.appendLine(String(error)));
  }

  async dispose() {
    this.disposed = true;
    await this.pending;
    const sessions = [...this.sessions.values()];
    this.sessions.clear();
    this.previews.clear();
    await Promise.all(sessions.map(session => this.stopSession(session)));
    for (const disposable of this.disposables) disposable.dispose();
  }
}

async function exists(filename) {
  try { return (await fs.stat(filename)).isFile(); }
  catch (error) {
    if (error.code === "ENOENT" || error.code === "ENOTDIR") return false;
    throw error;
  }
}

async function activate() {
  active = new Extension(require("vscode"), require("vscode-languageclient/node").LanguageClient);
  await active.start();
}

async function deactivate() {
  if (active) await active.dispose();
  active = undefined;
}

module.exports = { activate, deactivate, Extension };
