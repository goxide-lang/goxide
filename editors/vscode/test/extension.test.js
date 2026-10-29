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
const turn = () => new Promise(resolve => setImmediate(resolve));
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}

async function fixture(t, options = {}) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "goxide-vscode-"));
  t.after(() => fs.rm(root, { recursive: true, force: true }));
  if (options.project !== false) await fs.writeFile(path.join(root, "goxide.toml"), "[package]\nname = 'test'\n");
  const settings = Object.fromEntries(Object.entries(manifest.contributes.configuration.properties)
    .map(([key, value]) => [key.slice("goxide.".length), structuredClone(value.default)]));
  Object.assign(settings, options.settings);
  const events = Object.fromEntries(["open", "close", "folders", "configuration", "rename", "extensions", "trust", "save"].map(name => [name, new Emitter()]));
  const clients = [], watchers = [], warnings = [], errors = [], updates = [], commands = new Map(), providers = new Map();
  const folder = { uri: fileURI(root), name: "test", index: 0 };
  const vscode = {
    Uri: { file: fileURI, parse: uri }, EventEmitter: Emitter,
    RelativePattern: class { constructor(base, pattern) { this.base = base; this.pattern = pattern; } },
    ViewColumn: { Beside: 2 }, ConfigurationTarget: { WorkspaceFolder: 3 },
    TestRunProfileKind: { Run: 1, Debug: 2 },
    tests: { createTestController: () => ({ createRunProfile() {}, dispose() {} }) },
    debug: { registerDebugConfigurationProvider: () => ({ dispose() {} }), registerDebugAdapterDescriptorFactory: () => ({ dispose() {} }) },
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
      onDidSaveTextDocument: events.save.event,
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
    async start() { await options.start?.(); this.started = true; }
    async stop() { this.stopped = true; }
    async sendNotification(method, params) { this.notifications.push({ method, params }); }
    async sendRequest(method, params) {
      this.requests.push({ method, params });
      return { uri: "goxide-generated:///test/main.go", content: "package main\n" };
    }
  }
  const extension = new Extension(vscode, Client, fileURI(path.join(root, "storage")));
  const preparations = [];
  extension.tools.prepare = async (...args) => {
    preparations.push(args);
    return options.prepare ? options.prepare(...args) : args[1] ? "off" : args[0];
  };
  t.after(() => extension.dispose());
  const document = (name, languageId = "goxide") => ({ uri: fileURI(path.join(root, name)), languageId });
  return { root, settings, events, clients, watchers, warnings, errors, updates, commands, providers, vscode, extension, document, preparations };
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

function close(f, document, next) {
  document.isClosed = true;
  f.vscode.workspace.textDocuments = f.vscode.workspace.textDocuments.filter(other => other !== document);
  f.events.close.fire(document);
  // LanguageClient registers after Extension and sends within the same event.
  const client = f.clients.at(-1);
  return next && (client.clientOptions.middleware?.didClose(document, next) || next(document));
}

test("standalone shutdown waits until didClose has been sent", async t => {
  const f = await fixture(t, { project: false });
  const document = f.document("single.gox");
  f.vscode.workspace.textDocuments.push(document);
  await f.extension.start();
  const gate = deferred();
  const sending = close(f, document, () => gate.promise);
  await turn();
  const stoppedEarly = f.clients[0].stopped;
  gate.resolve();
  await sending;
  await f.extension.pending;
  assert.equal(stoppedEarly, false, "server stopped while didClose was still sending");
  assert.equal(f.clients[0].stopped, true);
  assert.equal(f.extension.sessions.size, 0);
});

test("reopening the same URI while didClose sends keeps its session", async t => {
  const f = await fixture(t, { project: false });
  const document = f.document("single.gox");
  f.vscode.workspace.textDocuments.push(document);
  await f.extension.start();
  const gate = deferred();
  const sending = close(f, document, () => gate.promise);
  await turn();
  const reopened = f.document("single.gox");
  f.vscode.workspace.textDocuments.push(reopened);
  f.events.open.fire(reopened);
  gate.resolve();
  await sending;
  await f.extension.pending;
  assert.equal(f.clients.length, 1);
  assert.equal(f.clients[0].stopped, false);
});

test("configuration restart drains additional didClose sends and preserves their errors", async t => {
  const f = await fixture(t);
  const first = f.document("first.gox"), second = f.document("second.gox");
  f.vscode.workspace.textDocuments.push(first, second);
  await f.extension.start();
  const firstGate = deferred(), secondGate = deferred();
  const firstSending = close(f, first, () => firstGate.promise);
  f.events.configuration.fire({ affectsConfiguration: section => section === "goxide" });
  await turn();
  const stoppedBeforeFirst = f.clients[0].stopped;
  const secondSending = close(f, second, () => secondGate.promise);
  const rejected = assert.rejects(secondSending, /close send failed/);
  firstGate.resolve();
  await firstSending;
  await turn();
  const stoppedBeforeSecond = f.clients[0].stopped;
  secondGate.reject(new Error("close send failed"));
  await rejected;
  await f.extension.pending;
  assert.equal(stoppedBeforeFirst, false);
  assert.equal(stoppedBeforeSecond, false);
  assert.equal(f.clients[0].stopped, true);
  assert.equal(f.clients.length, 2);
  assert.equal(f.clients[1].stopped, false);
});

test("discovered projects stay open for another Goxide file but not unsynchronized files", async t => {
  const f = await fixture(t, { project: false });
  await fs.mkdir(path.join(f.root, "nested"));
  await fs.writeFile(path.join(f.root, "nested/goxide.toml"), "[package]\nname = 'nested'\n");
  const source = f.document("nested/main.gox"), other = f.document("nested/other.gox");
  const readme = f.document("nested/README.md", "markdown"), native = f.document("nested/native.go", "go");
  f.vscode.workspace.textDocuments.push(source, other, readme, native);
  await f.extension.start();
  await close(f, source, async () => {});
  await f.extension.pending;
  assert.equal(f.clients[0].stopped, false);
  await close(f, other, async () => {});
  await f.extension.pending;
  assert.equal(f.clients[0].stopped, true);
});

test("changing the last Goxide file to plaintext releases its session", async t => {
  const f = await fixture(t, { project: false });
  const document = f.document("single.gox");
  f.vscode.workspace.textDocuments.push(document);
  await f.extension.start();
  const sending = close(f, document, async () => {});
  f.vscode.workspace.textDocuments.push(f.document("single.gox", "plaintext"));
  await sending;
  await f.extension.pending;
  assert.equal(f.clients[0].stopped, true);
});

test("managed Go keeps a discovered session open until its last selected file closes", async t => {
  const f = await fixture(t, { project: false, settings: { enableManagedGo: true } });
  await fs.mkdir(path.join(f.root, "nested"));
  await fs.writeFile(path.join(f.root, "nested/goxide.toml"), "[package]\nname = 'nested'\n");
  const source = f.document("nested/main.gox"), native = f.document("nested/native.go", "go");
  f.vscode.workspace.textDocuments.push(source, native,
    f.document("nested/notes.md"), f.document("nested/snippet.txt", "go"));
  await f.extension.start();
  await close(f, source, async () => {});
  await f.extension.pending;
  assert.equal(f.clients[0].stopped, false);
  await close(f, native, async () => {});
  await f.extension.pending;
  assert.equal(f.clients[0].stopped, true);
});

test("closing a standalone document during startup still reclaims the completed client", async t => {
  const ready = deferred(), gate = deferred();
  const f = await fixture(t, { project: false, start: () => { ready.resolve(); return gate.promise; } });
  const document = f.document("single.gox");
  f.vscode.workspace.textDocuments.push(document);
  const starting = f.extension.start();
  await ready.promise;
  close(f, document);
  gate.resolve();
  await starting;
  await f.extension.pending;
  assert.equal(f.clients[0].started, true);
  assert.equal(f.clients[0].stopped, true);
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
  assert.deepEqual(f.preparations, []);
  f.vscode.workspace.isTrusted = true;
  f.events.trust.fire();
  await f.extension.pending;
  assert.equal(f.clients.length, 1);
});

test("prepared gopls path reaches the server without changing compiler settings", async t => {
  const backend = path.join(os.tmpdir(), "extension storage", "gopls");
  const f = await fixture(t, { prepare: () => backend, settings: { executable: "tools/goxide" } });
  await f.extension.start();
  assert.deepEqual(f.preparations, [["gopls", false, f.root]]);
  assert.equal(f.clients[0].serverOptions.args[2], backend);
  assert.equal(f.clients[0].serverOptions.command, path.join(f.root, "tools/goxide"));
  assert.equal(f.settings.gopls, "gopls");
  assert.deepEqual(f.updates, []);
});

test("failed tool preparation keeps source services and restart retries preparation", async t => {
  let attempts = 0;
  const f = await fixture(t, { prepare: () => ++attempts === 1 ? "off" : "/private/gopls", settings: { enableManagedGo: true } });
  await f.extension.start();
  assert.equal(f.clients[0].serverOptions.args[2], "off");
  assert.deepEqual(f.clients[0].clientOptions.documentSelector.map(value => value.language), ["goxide"]);
  await f.commands.get("goxide.restart")();
  assert.equal(attempts, 2);
  assert.equal(f.clients[1].serverOptions.args[2], "/private/gopls");
  assert.deepEqual(f.clients[1].clientOptions.documentSelector.map(value => value.language), ["goxide", "go"]);
});

test("disposal waits for tool cancellation without starting a late client", async t => {
  let started, cancelled;
  const ready = new Promise(resolve => { started = resolve; });
  const f = await fixture(t, { prepare: () => new Promise(resolve => { cancelled = resolve; started(); }) });
  f.extension.tools.dispose = () => cancelled("off");
  const starting = f.extension.start();
  await ready;
  await f.extension.dispose();
  await starting;
  assert.equal(f.clients.length, 0);
});
