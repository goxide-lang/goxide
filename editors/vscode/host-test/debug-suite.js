"use strict";

const assert = require("node:assert/strict");
const path = require("node:path");
const vscode = require("vscode");

async function run() {
  await vscode.extensions.getExtension("goxide-lang.goxide").activate();
  assert.equal(vscode.extensions.getExtension("golang.go"), undefined, "debugger must be independent of the Go extension");
  const root = process.env.GOXIDE_VSCODE_TEST_ROOT;
  const main = path.join(root, "cmd/main/main.gox");
  const value = path.join(root, "value/value.gox");
  const events = [];
  let session;
  const tracker = vscode.debug.registerDebugAdapterTrackerFactory("goxide", { createDebugAdapterTracker: candidate => {
    if (!candidate.configuration.__goxideHostDebug) return;
    session = candidate;
    return { onDidSendMessage: message => { events.push(message); } };
  } });
  const breakpoints = [main, value].map(filename => new vscode.SourceBreakpoint(new vscode.Location(vscode.Uri.file(filename), new vscode.Position(3, 0))));
  vscode.debug.addBreakpoints(breakpoints);
  try {
    await vscode.window.showTextDocument(await vscode.workspace.openTextDocument(main));
    assert.equal(await vscode.debug.startDebugging(vscode.workspace.workspaceFolders[0], {
      type: "goxide", request: "launch", name: "Host real breakpoints", input: root, bin: "main", __goxideHostDebug: true,
    }), true);
    const first = await until(() => events.find(message => message.event === "stopped"), "main source breakpoint", events);
    let stack = await session.customRequest("stackTrace", { threadId: first.body.threadId });
    assert.equal(stack.stackFrames[0].source.path, main, "main breakpoint points at generated Go");
    assert.equal(stack.stackFrames[0].line, 4);
    assert.equal(first.body.reason, "breakpoint");

    let nextEvent = events.length;
    await session.customRequest("continue", { threadId: first.body.threadId });
    const second = await until(() => events.slice(nextEvent).find(message => message.event === "stopped"), "dependency source breakpoint", events);
    assert.equal(second.body.reason, "breakpoint");
    stack = await session.customRequest("stackTrace", { threadId: second.body.threadId });
    assert.equal(stack.stackFrames[0].source.path, value, "dependency breakpoint points at generated Go");
    assert.equal(stack.stackFrames[0].line, 4);
    assert.ok(stack.stackFrames.some(frame => frame.source?.path === main), "cross-package stack lost authored main.gox");
    assert.equal(await localValue(session, stack.stackFrames[0].id, "answer"), "40");

    nextEvent = events.length;
    await session.customRequest("next", { threadId: second.body.threadId });
    const stepped = await until(() => events.slice(nextEvent).find(message => message.event === "stopped"), "step over assignment", events);
    stack = await session.customRequest("stackTrace", { threadId: stepped.body.threadId });
    assert.equal(stack.stackFrames[0].source.path, value);
    assert.equal(stack.stackFrames[0].line, 5);
    assert.equal(await localValue(session, stack.stackFrames[0].id, "answer"), "42");

    await session.customRequest("continue", { threadId: stepped.body.threadId });
    await until(() => events.find(message => message.event === "terminated"), "program termination", events);
    assert.ok(events.some(message => message.event === "output" && message.body.output.includes("42")), "program output was not forwarded");
    assert.ok(events.some(message => message.event === "exited" && message.body.exitCode === 0), "program did not exit successfully");
    const verified = events.filter(message => message.type === "response" && message.command === "setBreakpoints")
      .flatMap(message => message.body?.breakpoints || []).filter(breakpoint => breakpoint.verified);
    assert.ok(verified.length >= 2, "Delve did not verify both authored source breakpoints");
    console.log("Goxide debug host test passed: real VS Code and Delve; multi-package .gox breakpoints; original stack paths and lines; locals 40 -> 42 after next; program output and exit.");
  } finally {
    vscode.debug.removeBreakpoints(breakpoints);
    if (session && !events.some(message => message.event === "terminated")) await vscode.debug.stopDebugging(session);
    tracker.dispose();
  }
}

async function localValue(session, frameId, name) {
  const scopes = await session.customRequest("scopes", { frameId });
  for (const scope of scopes.scopes) {
    const { variables } = await session.customRequest("variables", { variablesReference: scope.variablesReference });
    const variable = variables.find(variable => variable.name === name);
    if (variable) return variable.value;
  }
  throw new Error(`Local variable ${name} was not available`);
}

async function until(read, label, events) {
  const deadline = Date.now() + 40000;
  while (Date.now() < deadline) {
    const result = read();
    if (result) return result;
    // VS Code can finish a scopes request for the previous stop after next has
    // invalidated its frame. Our own customRequest promises validate their
    // responses; only session startup failures abort an event wait globally.
    const failure = events.find(message => message.type === "response" && message.success === false &&
      ["initialize", "launch", "configurationDone"].includes(message.command));
    if (failure) throw new Error(`${label}: ${JSON.stringify(failure)}`);
    await new Promise(resolve => setTimeout(resolve, 20));
  }
  throw new Error(`Timed out waiting for ${label}: ${JSON.stringify(events.slice(-12))}`);
}

module.exports = { run };
