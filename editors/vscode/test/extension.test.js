"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL, fileURLToPath } = require("node:url");
const { Extension } = require("../extension");
const manifest = require("../package.json");

class Emitter {
  listeners = new Set();
  event = listener => { this.listeners.add(listener); return { dispose: () => this.listeners.delete(listener) }; };
  fire(value) { for (const listener of [...this.listeners]) listener(value); }
  dispose() { this.listeners.clear(); }
}

function uri(value) {
  const url = new URL(value);
  return { scheme: url.protocol.slice(0, -1), fsPath: url.protocol === "file:" ? fileURLToPath(url) : url.pathname, toString: () => url.href };
}
const fileURI = filename => uri(pathToFileURL(filename).href);

async function fixture(t, options = {}) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "goxide-vscode-"));
  t.after(() => fs.rm(root, { recursive: true, force: true }));
  if (options.project !== false) await fs.writeFile(path.join(root, "goxide.toml"), "[package]\nname = 'test'\n");
  const settings = Object.fromEntries(Object.entries(manifest.contributes.configuration.properties)
    .map(([key, value]) => [key.slice("goxide.".length), structuredClone(value.default)]));
  Object.assign(settings, options.settings);
  const events = Object.fromEntries(["open", "close", "folders", "configuration", "rename", "extensions", "trust"].map(name => [name, new Emitter()]));
  const clients = [], watchers = [], warnings = [], errors = [], updates = [], commands = new Map(), providers = new Map();
  const folder = { uri: fileURI(root), name: "test", index: 0 };
  const vscode = {
    Uri: { file: fileURI, parse: uri }, EventEmitter: Emitter,
    RelativePattern: class { constructor(base, pattern) { this.base = base; this.pattern = pattern; } },
    ViewColumn: { Beside: 2 }, ConfigurationTarget: { WorkspaceFolder: 3 },
    workspace: {
      isTrusted: options.trusted !== false,
      workspaceFolders: options.folder === false ? [] : [folder], textDocuments: [],
      getWorkspaceFolder: documentURI => options.folder === false || !documentURI.fsPath.startsWith(root + path.sep) ? undefined : folder,
      getConfiguration: () => ({ get: key => settings[key], update: async (...args) => { updates.push(args); settings[args[0]] = args[1]; events.configuration.fire({ affectsConfiguration: () => true }); } }),
      registerTextDocumentContentProvider: (scheme, provider) => { providers.set(scheme, provider); return { dispose: () => providers.delete(scheme) }; },
      createFileSystemWatcher: pattern => {
        const create = new Emitter(), change = new Emitter(), remove = new Emitter();
        const watcher = { pattern, create, change, remove, disposed: false,
          onDidCreate: create.event, onDidChange: change.event, onDidDelete: remove.event,
          dispose() { this.disposed = true; create.dispose(); change.dispose(); remove.dispose(); },
        };
        watchers.push(watcher); return watcher;
      },
      onDidOpenTextDocument: events.open.event, onDidCloseTextDocument: events.close.event,
      onDidChangeWorkspaceFolders: events.folders.event, onDidChangeConfiguration: events.configuration.event,
      onDidRenameFiles: events.rename.event, onDidGrantWorkspaceTrust: events.trust.event,
      openTextDocument: async documentURI => {
        const content = await providers.get(documentURI.scheme).provideTextDocumentContent(documentURI, { isCancellationRequested: false });
        const document = { uri: documentURI, content, languageId: "plaintext" };
        vscode.workspace.textDocuments.push(document); return document;
      },
    },
    window: {
      createOutputChannel: (_name, outputOptions) => { assert.deepEqual(outputOptions, { log: true }); return { appendLine() {}, dispose() {} }; },
      showWarningMessage: async message => { warnings.push(message); },
      showErrorMessage: async message => { errors.push(message); }, showInformationMessage: async () => {},
      showTextDocument: async document => { vscode.window.activeTextEditor = { document }; },
    },
    languages: { setTextDocumentLanguage: async (document, language) => { document.languageId = language; return document; } },
    extensions: { getExtension: id => options.goExtension && id === "golang.go" ? {} : undefined, onDidChange: events.extensions.event },
    commands: {
      registerCommand: (name, command) => { commands.set(name, command); return { dispose: () => commands.delete(name) }; },
      executeCommand: async () => {},
    },
  };
  class Client {
    constructor(id, name, serverOptions, clientOptions) { Object.assign(this, { id, name, serverOptions, clientOptions, notifications: [], requests: [], stopped: false }); clients.push(this); }
    async start() { this.started = true; }
    async stop() { this.stopped = true; }
    async sendNotification(method, params) { this.notifications.push({ method, params }); }
    async sendRequest(method, params) {
      this.requests.push({ method, params });
      return { uri: "goxide-generated:///test/main.go", content: "package main\n" };
    }
  }
  const extension = new Extension(vscode, Client);
  t.after(() => extension.dispose());
  const document = (name, languageId = "goxide") => ({ uri: fileURI(path.join(root, name)), languageId });
  return { root, settings, events, clients, watchers, warnings, errors, updates, commands, providers, vscode, extension, document };
}

test("Go extension conflict disables the backend while retaining Goxide source services", async t => {
  const f = await fixture(t, { goExtension: true, settings: { enableManagedGo: true } });
  await f.extension.start();
  assert.equal(f.clients.length, 1);
  assert.equal(f.clients[0].serverOptions.args[2], "off");
  assert.deepEqual(f.clients[0].clientOptions.documentSelector.map(value => value.language), ["goxide"]);
  assert.equal(f.warnings.length, 1);
  assert.deepEqual(f.updates, []);
  f.vscode.window.activeTextEditor = { document: f.document("main.gox") };
  await f.commands.get("goxide.enableManagedGo")();
  assert.deepEqual(f.updates, []);
  assert.equal(f.clients[0].stopped, false);
});

test("managed Go opt-in changes only the current folder and restarts selectors", async t => {
  const f = await fixture(t);
  await f.extension.start();
  f.vscode.window.activeTextEditor = { document: f.document("main.gox") };
  await f.commands.get("goxide.enableManagedGo")();
  await f.extension.pending;
  assert.deepEqual(f.updates, [["enableManagedGo", true, f.vscode.ConfigurationTarget.WorkspaceFolder]]);
  assert.equal(f.clients[0].stopped, true);
  assert.deepEqual(f.clients[1].clientOptions.documentSelector.map(value => value.language), ["goxide", "go"]);
  assert.notEqual(f.clients[1].serverOptions.args[2], "off");
  assert.equal(f.clients[0].notifications[0].method, "workspace/didChangeConfiguration");
});

test("watchers send create/delete/rename and saved unsynchronized Go, without rescanning or duplicate open-buffer saves", async t => {
  const f = await fixture(t);
  const source = f.document("main.gox"), native = f.document("helper.go", "go");
  f.vscode.workspace.textDocuments.push(source, native);
  await f.extension.start();
  const watcher = f.watchers[0], client = f.clients[0];
  watcher.change.fire(source.uri);
  watcher.change.fire(native.uri);
  watcher.create.fire(f.document("new.gox").uri);
  watcher.remove.fire(f.document("deleted.gox").uri);
  watcher.change.fire(f.document("target/generated.go").uri);
  f.events.rename.fire({ files: [{ oldUri: f.document("old.gox").uri, newUri: f.document("renamed.gox").uri }] });
  assert.deepEqual(client.notifications.map(value => value.params.changes.map(change => change.type)), [[2], [1], [3], [3, 1]]);
  assert.ok(client.notifications.every(value => value.method === "workspace/didChangeWatchedFiles"));
  assert.equal(f.clients.length, 1);
  assert.equal(f.watchers.length, 1);
});

test("feature and target configuration is notified before restart with fresh CLI arguments", async t => {
  const f = await fixture(t);
  await f.extension.start();
  f.settings.features = ["fast"];
  f.settings.target = "linux/amd64";
  f.events.configuration.fire({ affectsConfiguration: section => section === "goxide" });
  await f.extension.pending;
  assert.deepEqual(f.clients[0].notifications[0].params.settings.goxide.features, ["fast"]);
  assert.equal(f.clients[0].stopped, true);
  assert.equal(f.watchers[0].disposed, true);
  assert.deepEqual(f.clients[1].serverOptions.args.slice(3, 7), ["--features", "fast", "--target", "linux/amd64"]);
});

test("ordinary Go workspaces remain unmanaged; standalone sessions stop when closed", async t => {
  const f = await fixture(t, { project: false });
  await f.extension.start();
  assert.equal(f.clients.length, 0);
  const document = f.document("single.gox");
  f.vscode.workspace.textDocuments.push(document);
  f.events.open.fire(document);
  await f.extension.pending;
  assert.equal(f.clients.length, 1);
  assert.equal(f.clients[0].serverOptions.args.at(-1), document.uri.fsPath);
  assert.equal(f.clients[0].clientOptions.documentSelector.length, 1);
  f.vscode.workspace.textDocuments = [];
  f.events.close.fire(document);
  await f.extension.pending;
  assert.equal(f.clients[0].stopped, true);
  assert.equal(f.watchers[0].disposed, true);
  assert.equal(f.extension.sessions.size, 0);
});

test("preview provider uses the server URI and readonly content provider; no file writer is registered", async t => {
  const f = await fixture(t);
  await f.extension.start();
  f.vscode.window.activeTextEditor = { document: f.document("main.gox") };
  await f.commands.get("goxide.generatedSource")();
  assert.equal(f.vscode.window.activeTextEditor.document.uri.scheme, "goxide-generated");
  assert.equal(f.vscode.window.activeTextEditor.document.content, "package main\n");
  assert.equal(f.vscode.window.activeTextEditor.document.languageId, "go");
  assert.deepEqual(f.clients[0].requests.map(request => request.params.uri), [
    f.document("main.gox").uri.toString(), "goxide-generated:///test/main.go",
  ]);
  assert.ok(f.clients[0].requests.every(request => request.method === "goxide/generatedSource"));
  assert.equal(f.providers.size, 1);
  f.events.close.fire(f.vscode.window.activeTextEditor.document);
  assert.equal(f.extension.previews.size, 0);
});

test("untrusted workspaces do not launch executable clients", async t => {
  const f = await fixture(t, { trusted: false });
  f.vscode.workspace.textDocuments.push(f.document("main.gox"));
  await f.extension.start();
  assert.equal(f.clients.length, 0);
  f.vscode.workspace.isTrusted = true;
  f.events.trust.fire();
  await f.extension.pending;
  assert.equal(f.clients.length, 1);
});
