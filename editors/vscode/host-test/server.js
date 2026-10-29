"use strict";

const fs = require("node:fs");
const log = value => fs.appendFileSync(process.env.GOXIDE_VSCODE_TEST_LOG, JSON.stringify({ pid: process.pid, ...value }) + "\n");
log({ argv: process.argv.slice(2), cwd: process.cwd() });
let buffer = Buffer.alloc(0);
function send(message) {
  const body = Buffer.from(JSON.stringify({ jsonrpc: "2.0", ...message }));
  process.stdout.write(`Content-Length: ${body.length}\r\n\r\n`);
  process.stdout.write(body);
}
function receive(message) {
  log(message);
  const { method, params, id } = message;
  const range = { start: { line: 0, character: 0 }, end: { line: 0, character: 7 } };
  if (method === "exit") return process.exit(0);
  if (method === "textDocument/didOpen") send({ method: "textDocument/publishDiagnostics", params: {
    uri: params.textDocument.uri, version: params.textDocument.version,
    diagnostics: [{ range, message: "fixture diagnostic", severity: 2, source: "goxide" }],
  } });
  if (id === undefined) return;
  let result = null;
  if (method === "initialize") result = { capabilities: {
    positionEncoding: "utf-16", textDocumentSync: { openClose: true, change: 2, save: {} },
    hoverProvider: true, definitionProvider: true, completionProvider: {}, documentSymbolProvider: true,
  } };
  if (method === "textDocument/hover") result = { contents: { kind: "plaintext", value: "fixture hover" } };
  if (method === "textDocument/definition") result = { uri: params.textDocument.uri, range };
  if (method === "textDocument/completion") result = [{ label: "fixtureCompletion", kind: 6 }];
  if (method === "textDocument/documentSymbol") result = [{ name: "fixtureSymbol", kind: 12, range, selectionRange: range }];
  if (method === "goxide/generatedSource") result = { uri: "goxide-generated:example.org/fixture/main.gox", content: "package main\nfunc main() {}\n" };
  send({ id, result });
}
process.stdin.on("data", data => {
  buffer = Buffer.concat([buffer, data]);
  for (;;) {
    const end = buffer.indexOf("\r\n\r\n");
    if (end < 0) return;
    const length = Number(/Content-Length:\s*(\d+)/i.exec(buffer.subarray(0, end).toString())[1]);
    if (buffer.length < end + 4 + length) return;
    const body = buffer.subarray(end + 4, end + 4 + length);
    buffer = buffer.subarray(end + 4 + length);
    receive(JSON.parse(body.toString()));
  }
});
