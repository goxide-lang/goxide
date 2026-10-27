"use strict";

const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const vscode = require("vscode");

async function records() {
  try { return (await fs.readFile(process.env.GOXIDE_VSCODE_TEST_LOG, "utf8")).trim().split("\n").filter(Boolean).map(JSON.parse); }
  catch (error) { if (error.code === "ENOENT") return []; throw error; }
}
async function until(check) {
  const deadline = Date.now() + 10000;
  do {
    const result = await check();
    if (result) return result;
    await new Promise(resolve => setTimeout(resolve, 25));
  } while (Date.now() < deadline);
  throw new Error("Timed out waiting for VS Code language-client behavior");
}

async function run() {
  const extension = vscode.extensions.getExtension("goxide-lang.goxide");
  assert.ok(extension, "development extension was not installed");
  await extension.activate();
  const root = process.env.GOXIDE_VSCODE_TEST_ROOT;
  const source = await vscode.workspace.openTextDocument(path.join(root, "main.gox"));
  await vscode.window.showTextDocument(source);
  assert.equal(source.languageId, "goxide");
  const native = await vscode.workspace.openTextDocument(path.join(root, "native.go"));
  assert.equal(native.languageId, "go");
  const literalName = path.join(root, "file[1].gox");
  await fs.writeFile(literalName, "package main\n");
  const literalDocument = await vscode.workspace.openTextDocument(literalName);
  const { documentSelector, projectPlan } = require("../config");
  const selector = documentSelector(vscode, projectPlan(root, literalName, true), false).map(filter => ({ ...filter,
    pattern: new vscode.RelativePattern(vscode.Uri.parse(filter.pattern.baseUri), filter.pattern.pattern),
  }));
  assert.ok(vscode.languages.match(selector, literalDocument) > 0, "single-file glob characters were interpreted as patterns");
  assert.equal(vscode.languages.match(selector, source), 0);
  await until(async () => (await records()).filter(item => item.method === "textDocument/didOpen").length >= 2);
  const initialize = (await records()).find(item => item.method === "initialize");
  assert.deepEqual(initialize.params.capabilities.general.positionEncodings, ["utf-16"]);
  assert.equal(initialize.params.workspaceFolders.length, 1);
  assert.equal(initialize.params.rootUri, vscode.Uri.file(root).toString());
  const launched = (await records()).find(item => item.argv);
  assert.deepEqual(launched.argv, ["lsp", "--gopls", "fixture-gopls", "--lib", "--features", "fast", "--tags", "debug,trace", "--no-default-features", "--target", "linux/amd64", "--", root]);
  assert.equal(launched.cwd, root);
  const position = new vscode.Position(1, 5);
  const hover = await vscode.commands.executeCommand("vscode.executeHoverProvider", source.uri, position);
  // The standard client escapes plaintext spaces when producing Markdown.
  assert.equal(hover[0].contents[0].value, "fixture&nbsp;hover");
  const locations = await vscode.commands.executeCommand("vscode.executeDefinitionProvider", source.uri, position);
  assert.equal(locations[0].uri.toString(), source.uri.toString());
  const completion = await vscode.commands.executeCommand("vscode.executeCompletionItemProvider", source.uri, position);
  assert.ok(completion.items.some(item => item.label === "fixtureCompletion"));
  const symbols = await vscode.commands.executeCommand("vscode.executeDocumentSymbolProvider", source.uri);
  assert.equal(symbols[0].name, "fixtureSymbol");
  await until(() => vscode.languages.getDiagnostics(source.uri).some(item => item.message === "fixture diagnostic"));
  const hovered = (await records()).find(item => item.method === "textDocument/hover");
  assert.deepEqual(hovered.params.position, { line: 1, character: 5 });
  await vscode.commands.executeCommand("goxide.generatedSource");
  assert.equal(vscode.window.activeTextEditor.document.uri.scheme, "goxide-generated");
  assert.match(vscode.window.activeTextEditor.document.getText(), /package main/);
  assert.equal(vscode.window.activeTextEditor.document.languageId, "go");
  await vscode.commands.executeCommand("goxide.generatedSource");
  assert.equal(vscode.window.activeTextEditor.document.uri.scheme, "goxide-generated");
  const edit = new vscode.WorkspaceEdit();
  edit.insert(source.uri, new vscode.Position(3, 0), "// changed\n");
  assert.equal(await vscode.workspace.applyEdit(edit), true);
  await until(async () => (await records()).some(item => item.method === "textDocument/didChange"));
  await vscode.workspace.getConfiguration("goxide", source.uri).update("features", ["other"], vscode.ConfigurationTarget.WorkspaceFolder);
  await until(async () => (await records()).some(item => item.argv?.includes("other")));
  const all = await records();
  const configurationIndex = all.findIndex(item => item.method === "workspace/didChangeConfiguration");
  const shutdownIndex = all.findIndex(item => item.method === "shutdown");
  assert.ok(configurationIndex >= 0 && shutdownIndex > configurationIndex);
  await require("../extension").deactivate();

  // Exercise the actual LanguageClient converter and provider registration.
  // With a VS Code RelativePattern here the client drops its path restriction,
  // opens the outside document, and incorrectly offers the fixture's hover.
  const { LanguageClient } = require("vscode-languageclient/node");
  const boundary = new LanguageClient("goxide-boundary", "Goxide project boundary", {
    command: vscode.workspace.getConfiguration("goxide", source.uri).get("executable"), args: launched.argv, options: { cwd: root },
  }, { documentSelector: documentSelector(vscode, projectPlan(root, "."), true) });
  try {
    await boundary.start();
    const pid = (await records()).filter(item => item.method === "initialize").at(-1).pid;
    const outsidePath = path.join(path.dirname(root), "outside.gox");
    await fs.writeFile(outsidePath, "package outside\nfunc Outside() {}\n");
    const outside = await vscode.workspace.openTextDocument(outsidePath);
    const outsideHover = await vscode.commands.executeCommand("vscode.executeHoverProvider", outside.uri, position);
    assert.deepEqual(outsideHover, [], "the project client answered for an outside .gox file");
    const insideHover = await vscode.commands.executeCommand("vscode.executeHoverProvider", source.uri, position);
    assert.equal(insideHover[0].contents[0].value, "fixture&nbsp;hover");
    const boundaryRecords = (await records()).filter(item => item.pid === pid);
    assert.ok(boundaryRecords.some(item => item.method === "textDocument/didOpen" && item.params.textDocument.uri === source.uri.toString()));
    assert.equal(boundaryRecords.some(item => item.params?.textDocument?.uri === outside.uri.toString()), false, "the project client sent an outside document to its server");
  } finally { await boundary.stop(); }
  console.log("Goxide extension-host test passed: stdio, UTF-16, five capabilities, project document boundary, native Go selector, preview, edits, configuration restart, shutdown.");
}

module.exports = { run };
