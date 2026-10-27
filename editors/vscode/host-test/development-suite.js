"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { execFile } = require("node:child_process");
const { promisify } = require("node:util");
const vscode = require("vscode");
const execute = promisify(execFile);

async function run() {
  const installed = vscode.extensions.getExtension("goxide-lang.goxide");
  await installed.activate();
  await require("../extension").deactivate();
  const records = [];
  // Delegate every call to the real VS Code Testing API while retaining the
  // observations that its public TestRun object does not expose for reading.
  const testsAPI = { createTestController(...args) {
    const controller = vscode.tests.createTestController(...args);
    return new Proxy(controller, { get(target, property) {
      if (property === "createTestRun") return (...args) => {
        const run = target.createTestRun(...args);
        return new Proxy(run, { get(target, property) {
          if (["passed", "failed", "errored", "skipped", "appendOutput"].includes(property)) return (...args) => {
            records.push({ action: property, args }); return target[property](...args);
          };
          const value = target[property]; return typeof value === "function" ? value.bind(target) : value;
        } });
      };
      const value = target[property]; return typeof value === "function" ? value.bind(target) : value;
    } });
  } };
  const debugAPI = new Proxy(vscode.debug, { get(target, property) {
    // Test profile lifecycle can be exercised through real Delve DAP even on
    // machines where macOS developer permissions prevent attaching a debugger.
    if (property === "startDebugging") return (folder, config, options) => target.startDebugging(folder, config,
      config.__goxideTestRun ? { ...options, noDebug: true } : options);
    return target[property];
  } });
  const api = new Proxy(vscode, { get(target, property) { return property === "tests" ? testsAPI : property === "debug" ? debugAPI : target[property]; } });
  const { Extension } = require("../extension");
  const extension = new Extension(api, require("vscode-languageclient/node").LanguageClient);
  try {
    await extension.start();
    assert.equal(vscode.extensions.getExtension("golang.go"), undefined, "test must not depend on the Go extension");
    const root = process.env.GOXIDE_VSCODE_TEST_ROOT;
    const source = await vscode.workspace.openTextDocument(path.join(root, "cmd/main/main.gox"));
    await vscode.window.showTextDocument(source);
    const taskDone = new Promise((resolve, reject) => {
      const timer = setTimeout(() => { listener.dispose(); reject(new Error("Run task did not complete")); }, 20000);
      const listener = vscode.tasks.onDidEndTaskProcess(event => {
        if (event.execution.task.definition.type !== "goxide") return;
        clearTimeout(timer); listener.dispose(); resolve(event.exitCode);
      });
    });
    await vscode.commands.executeCommand("goxide.run");
    assert.equal(await taskDone, 0, "Run command failed");
    await extension.development.refresh();
    const leaves = extension.development.leaves(new vscode.TestRunRequest());
    assert.deepEqual(leaves.map(item => item.label).sort(), ["TestFail", "TestNotRun", "TestPass"], JSON.stringify([...extension.development.projects.values()].map(project => project.item.error)));
    assert.equal(leaves.find(item => item.label === "TestFail").range.start.line, 5);
    await vscode.commands.executeCommand("goxide.test");
    assert.ok(records.some(record => record.action === "passed" && record.args[0].label === "TestPass"));
    assert.ok(records.some(record => record.action === "skipped" && record.args[0].label === "TestNotRun"));
    const failure = records.find(record => record.action === "failed" && record.args[0].label === "TestFail");
    assert.ok(failure, "failed test was not reported to the real Testing API");
    const messages = Array.isArray(failure.args[1]) ? failure.args[1] : [failure.args[1]];
    assert.ok(messages.some(message => message.location?.uri.fsPath === path.join(root, "value/value_test.gox") && message.location.range.start.line === 6));
    assert.ok(records.some(record => record.action === "appendOutput" && record.args[0].includes("host failure location")));
    if (process.env.GOXIDE_TEST_DLV) {
      await runNoDebug(root);
      for (const name of ["TestPass", "TestFail", "TestNotRun"]) {
        records.length = 0;
        const item = extension.development.leaves(new vscode.TestRunRequest()).find(item => item.label === name);
        const cancellation = new vscode.CancellationTokenSource();
        const timeout = setTimeout(() => cancellation.cancel(), 30000);
        try { await extension.development.debugTests(new vscode.TestRunRequest([item]), cancellation.token); }
        finally { clearTimeout(timeout); cancellation.dispose(); }
        const result = records.find(record => record.action === (name === "TestPass" ? "passed" : name === "TestFail" ? "failed" : "skipped") && record.args[0].label === name);
        assert.ok(result, `debug test did not finish: ${JSON.stringify(records)}`);
        if (name === "TestFail") {
          const messages = Array.isArray(result.args[1]) ? result.args[1] : [result.args[1]];
          assert.ok(messages.some(message => message.location?.uri.fsPath === path.join(root, "value/value_test.gox") && message.location.range.start.line === 6), "debug test failure lost its original .gox location");
        }
      }
    }
    await runEditing(extension, root);
    console.log(`Goxide development host test passed: Go extension absent; real multi-package run; real test discovery, pass/fail, and original .gox failure location${process.env.GOXIDE_TEST_DLV ? "; real Delve noDebug DAP launch/output/exit and Test Explorer debug profile results" : ""}; real code action, signature help, cross-file references/rename, formatting, and generated Go execution.`);
  } finally { await extension.dispose(); }
}

async function runNoDebug(root) {
  const events = [];
  let timer, listener;
  const tracker = vscode.debug.registerDebugAdapterTrackerFactory("goxide", { createDebugAdapterTracker: session => session.configuration.__goxideHostNoDebug ? {
    onDidSendMessage: message => { events.push(message); },
  } : undefined });
  const terminated = new Promise((resolve, reject) => {
    listener = vscode.debug.onDidTerminateDebugSession(session => { if (session.configuration.__goxideHostNoDebug) resolve(); });
    timer = setTimeout(() => reject(new Error("Delve noDebug session did not terminate")), 30000);
  });
  try {
    assert.equal(await vscode.debug.startDebugging(vscode.workspace.workspaceFolders[0], {
      type: "goxide", request: "launch", name: "Host noDebug", input: root, bin: "main", __goxideHostNoDebug: true,
    }, { noDebug: true }), true);
    await terminated;
    assert.ok(events.some(message => message.event === "output" && message.body.output.includes("42")), "Delve did not forward original program output");
    assert.ok(events.some(message => message.event === "exited" && message.body.exitCode === 0), "Delve did not forward successful program exit");
  } finally { clearTimeout(timer); listener.dispose(); tracker.dispose(); }
}

async function runEditing(extension, project) {
  // A second ordinary project keeps the earlier manifest Run/Test/Debug checks
  // intact and gives the editor actions the same pinned runtime as real builds.
  const root = path.join(path.dirname(project), "editing");
  const repository = path.resolve(__dirname, "../../..");
  const module = await fs.readFile(path.join(repository, "go.mod"), "utf8");
  const runtimeVersion = module.match(/^\s*github\.com\/goxide-lang\/std\s+(\S+)/m)?.[1];
  const goVersion = module.match(/^go\s+(\S+)/m)?.[1];
  assert.ok(runtimeVersion && goVersion, "compiler runtime and Go version pins are required");
  const files = {
    "go.mod": `module example.org/editorloop\n\ngo ${goVersion}\n\nrequire github.com/goxide-lang/std ${runtimeVersion}\n`,
    "go.sum": await fs.readFile(path.join(repository, "go.sum"), "utf8"),
    "main.gox": "package main\ntrait Read {func read(self &Self, value int) int}\ntype Value struct{}\nimpl Read for Value{}\nfunc main(){println(add(20, 22))}\n",
    "arithmetic.gox": "package main\nfunc add(left int, right int) int{return left+right}\n",
  };
  for (const [name, text] of Object.entries(files)) {
    const filename = path.join(root, name);
    await fs.mkdir(path.dirname(filename), { recursive: true });
    await fs.writeFile(filename, text);
  }
  // Switching a single-folder VS Code window to a multi-root workspace restarts
  // its extension host. Use the supported explicit project setting instead.
  const settingsPath = path.join(project, ".vscode/settings.json");
  const settings = JSON.parse(await fs.readFile(settingsPath, "utf8"));
  let timer, listener;
  const configured = new Promise((resolve, reject) => {
    listener = vscode.workspace.onDidChangeConfiguration(event => {
      if (event.affectsConfiguration("goxide.input", vscode.Uri.file(project)) &&
          vscode.workspace.getConfiguration("goxide", vscode.Uri.file(project)).get("input") === root) resolve();
    });
    timer = setTimeout(() => reject(new Error("Editing project configuration was not observed")), 10000);
  });
  try {
    await fs.writeFile(settingsPath, JSON.stringify({ ...settings, "goxide.bin": "", "goxide.input": root,
      "goxide.gopls": process.env.GOXIDE_TEST_GOPLS || "gopls",
    }));
    await configured;
  } finally { clearTimeout(timer); listener.dispose(); }
  await extension.pending;
  const source = await vscode.workspace.openTextDocument(path.join(root, "main.gox"));
  const arithmetic = await vscode.workspace.openTextDocument(path.join(root, "arithmetic.gox"));
  await vscode.window.showTextDocument(source);
  await extension.pending;
  const session = extension.sessionFor(source.uri);
  assert.equal(session?.plan.root, root, "real editor language client did not discover the second project");
  const rawSelector = session.client.clientOptions.documentSelector;
  const convertedSelector = session.client.protocol2CodeConverter.asDocumentSelector(rawSelector);
  assert.ok(convertedSelector.every(filter => filter.pattern), `language client lost project selector: ${JSON.stringify({ rawSelector, convertedSelector })}`);
  assert.ok(vscode.languages.match(convertedSelector, source) > 0, "language client selector excludes its project source");
  const previousSource = await vscode.workspace.openTextDocument(path.join(project, "cmd/main/main.gox"));
  assert.equal(vscode.languages.match(convertedSelector, previousSource), 0, "language client selector claims another project source");
  const symbols = await editorCommand("vscode.executeDocumentSymbolProvider", source.uri);
  const context = () => JSON.stringify({ input: session.plan.input, state: session.client.state, language: source.languageId,
    symbols: symbols?.map(symbol => symbol.name), diagnostics: vscode.languages.getDiagnostics().flatMap(([uri, items]) => items.map(item => `${uri.fsPath}: ${item.message}`)) });
  assert.ok(symbols?.some(symbol => symbol.name === "Read") && symbols.some(symbol => symbol.name === "Value"), `new editor session did not analyze the fixture: ${context()}`);

  const implementation = positionAt(source, "impl Read");
  const actions = await editorCommand("vscode.executeCodeActionProvider", source.uri,
    new vscode.Range(implementation, implementation), vscode.CodeActionKind.QuickFix.value);
  const action = actions?.find(item => item.title === "Implement read");
  assert.ok(action?.edit && !action.disabled, `missing trait implementation quickfix: ${JSON.stringify(actions)} ${context()}`);
  assert.equal(await vscode.workspace.applyEdit(action.edit), true);
  assert.match(source.getText(), /TODO: implement read/);
  assert.equal(await fs.readFile(source.uri.fsPath, "utf8"), files["main.gox"], "editor code action unexpectedly saved the source buffer");

  const signature = await editorCommand("vscode.executeSignatureHelpProvider", source.uri, positionAt(source, "add(20, ", true));
  assert.equal(signature?.activeParameter, 1);
  assert.equal(signature.signatures[0]?.label, "add(left int, right int) int");
  const call = positionAt(source, "add(20");
  const references = await editorCommand("vscode.executeReferenceProvider", source.uri, call);
  assert.deepEqual(references?.map(item => path.basename(item.uri.fsPath)).sort(), ["arithmetic.gox", "main.gox"]);
  const rename = await editorCommand("vscode.executeDocumentRenameProvider", source.uri, call, "total");
  assert.deepEqual(rename?.entries().map(([uri]) => path.basename(uri.fsPath)).sort(), ["arithmetic.gox", "main.gox"]);
  assert.equal(await vscode.workspace.applyEdit(rename), true);
  assert.match(source.getText(), /println\(total\(20, 22\)\)/);
  assert.match(arithmetic.getText(), /func total\(left int, right int\)/);

  const formatOptions = { tabSize: 4, insertSpaces: true };
  for (const document of [source, arithmetic]) {
    const edits = await editorCommand("vscode.executeFormatDocumentProvider", document.uri, formatOptions);
    assert.ok(edits?.length, `no formatting edit for ${document.uri.fsPath}`);
    const formatting = new vscode.WorkspaceEdit();
    formatting.set(document.uri, edits);
    assert.equal(await vscode.workspace.applyEdit(formatting), true);
    const repeated = await editorCommand("vscode.executeFormatDocumentProvider", document.uri, formatOptions);
    // VS Code returns undefined when every formatter returns an empty edit list.
    assert.equal(repeated, undefined, "formatting was not stable on the current unsaved buffer");
    assert.equal(await document.save(), true);
  }
  const options = { cwd: root, env: { ...process.env, GOTOOLCHAIN: "local", GOWORK: "off" }, timeout: 30000 };
  await execute(process.env.GOXIDE_TEST_COMPILER, ["gen", root], options);
  const generated = await fs.readFile(path.join(root, "arithmetic_gox.go"), "utf8");
  assert.match(generated, /func total\(left int, right int\) int/);
  const output = await execute("go", ["run", "-mod=readonly", "."], options);
  assert.equal((output.stdout + output.stderr).trim(), "42", "edited and generated program did not run successfully");
}

async function editorCommand(command, ...args) {
  try { return await vscode.commands.executeCommand(command, ...args); }
  catch (error) { throw new Error(`Editing host ${command}: ${error.message}`, { cause: error }); }
}

function positionAt(document, needle, after = false) {
  const offset = document.getText().indexOf(needle);
  assert.notEqual(offset, -1, `missing editor position ${needle}`);
  return document.positionAt(offset + (after ? needle.length : 0));
}

module.exports = { run };
