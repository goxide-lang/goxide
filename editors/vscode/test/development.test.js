"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const { Development, execute } = require("../development");

test("cancelling a test kills a process tree that ignores SIGTERM", { skip: process.platform === "win32", timeout: 10000 }, async t => {
  let cancel;
  const token = { isCancellationRequested: false, onCancellationRequested(listener) { cancel = () => { token.isCancellationRequested = true; listener(); }; return { dispose() {} }; } };
  let parent, child;
  t.after(() => {
    if (parent) { try { process.kill(-parent, "SIGKILL"); } catch (error) { if (error.code !== "ESRCH") throw error; } }
  });
  const childScript = "process.on('SIGTERM',()=>{}); console.log(JSON.stringify({child:process.pid})); setInterval(()=>{},10000)";
  const script = `process.on('SIGTERM',()=>{}); console.log(JSON.stringify({parent:process.pid})); require('node:child_process').spawn(process.execPath, ['-e', ${JSON.stringify(childScript)}], {stdio:'inherit'}); setInterval(()=>{},10000)`;
  let ready;
  const started = new Promise(resolve => { ready = resolve; });
  const finished = execute({ command: process.execPath, args: ["-e", script], cwd: process.cwd() }, token, message => {
    parent ||= message.parent; child ||= message.child;
    if (parent && child) ready();
  });
  await started;
  cancel();
  const result = await finished;
  assert.equal(result.cancelled, true);
  for (const pid of [parent, child]) assert.throws(() => process.kill(pid, 0), { code: "ESRCH" });
});

test("cancellation also kills background children after their parent exits", { skip: process.platform === "win32", timeout: 10000 }, async t => {
  let cancel, parent, child, ready;
  const token = { isCancellationRequested: false, onCancellationRequested(listener) { cancel = () => { token.isCancellationRequested = true; listener(); }; return { dispose() {} }; } };
  t.after(() => {
    if (parent) { try { process.kill(-parent, "SIGKILL"); } catch (error) { if (error.code !== "ESRCH") throw error; } }
  });
  const childScript = "process.on('SIGTERM',()=>{}); process.send('ready'); setInterval(()=>{},10000)";
  const script = `const child=require('node:child_process').spawn(process.execPath,['-e',${JSON.stringify(childScript)}],{stdio:['ignore','ignore','ignore','ipc']}); child.on('message',()=>console.log(JSON.stringify({parent:process.pid,child:child.pid})))`;
  const started = new Promise(resolve => { ready = resolve; });
  const finished = execute({ command: process.execPath, args: ["-e", script], cwd: process.cwd() }, token, message => {
    parent = message.parent; child = message.child; ready();
  });
  await started;
  cancel();
  assert.equal((await finished).cancelled, true);
  // The child can briefly remain a zombie while its adoptive parent reaps it.
  await new Promise(resolve => setTimeout(resolve, 100));
  assert.throws(() => process.kill(child, 0), { code: "ESRCH" });
});

function collection() {
  const items = new Map();
  return { add: item => items.set(item.id, item), delete: id => items.delete(id), forEach: callback => items.forEach(callback),
    replace(values) { items.clear(); for (const item of values) items.set(item.id, item); } };
}

test("old test selections rediscover current settings before run and debug", { timeout: 10000 }, async t => {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "goxide-development-config-"));
  t.after(() => fs.rm(root, { recursive: true, force: true }));
  await fs.writeFile(path.join(root, "dev"), `const fs=require('node:fs');const args=process.argv.slice(2);fs.appendFileSync('calls.jsonl',JSON.stringify(args)+'\\n');
if(args[0]==='targets')console.log(JSON.stringify([{kind:'bin',name:'old-target'}]));
if(args[0]==='tests')console.log(JSON.stringify(args.includes('removed-target')?[]:[{name:'TestSelected',package:'example.org/app',file:${JSON.stringify(path.join(root, "main_test.gox"))},line:1}]));
if(args[0]==='test')console.log(JSON.stringify({Action:'pass',Test:'TestSelected',Elapsed:0}));`);
  let settings = { enabled: true, executable: process.execPath, input: ".", bin: "", example: "", lib: false, features: ["old-feature"], tags: [], noDefaultFeatures: false, target: "" };
  const folder = { uri: { scheme: "file", fsPath: root } }, records = [], configs = [];
  let startListener, terminateListener, trackerFactory, debugOutput = "--- PASS: TestSelected (0.00s)\n";
  const disposable = { dispose() {} };
  const vscode = {
    Uri: { file: fsPath => ({ scheme: "file", fsPath }) }, Range: class {},
    TestRunRequest: class { constructor(include, exclude) { Object.assign(this, { include, exclude }); } },
    TestMessage: class { constructor(message) { this.message = message; } },
    workspace: { isTrusted: true, workspaceFolders: [folder], textDocuments: [] },
    debug: {
      registerDebugAdapterTrackerFactory(_type, factory) { trackerFactory = factory; return disposable; },
      onDidStartDebugSession(listener) { startListener = listener; return disposable; },
      onDidTerminateDebugSession(listener) { terminateListener = listener; return disposable; },
      async startDebugging(_folder, config) {
        configs.push(config);
        const session = { configuration: config }, tracker = trackerFactory.createDebugAdapterTracker(session);
        startListener(session);
        if (debugOutput) tracker.onDidSendMessage({ type: "event", event: "output", body: { output: debugOutput } });
        tracker.onDidSendMessage({ type: "event", event: "exited", body: { exitCode: 0 } }); terminateListener(session);
        return true;
      },
    },
  };
  const extension = { vscode, sessions: new Map(), settings: () => ({ ...settings }) };
  const development = new Development(extension);
  development.controller = {
    items: collection(),
    createTestItem(id, label, uri) { return { id, label, uri, children: collection() }; },
    createTestRun() { return Object.fromEntries(["enqueued", "started", "passed", "failed", "skipped", "errored", "appendOutput", "end"].map(action => [action, (...args) => records.push({ action, args })])); },
  };
  await development.refresh();
  const oldItem = development.leaves(new vscode.TestRunRequest())[0];
  settings = { ...settings, features: ["new-feature"], tags: ["new-tag"], bin: "new-target" };
  await development.runTests(new vscode.TestRunRequest([oldItem]));
  let calls = (await fs.readFile(path.join(root, "calls.jsonl"), "utf8")).trim().split("\n").map(JSON.parse);
  const executed = calls.find(args => args[0] === "test");
  assert.ok(executed.includes("new-feature") && executed.includes("new-tag") && executed.includes("new-target"), JSON.stringify(executed));
  assert.ok(!executed.includes("old-feature") && !executed.includes("old-target"));
  assert.ok(records.some(record => record.action === "passed"));
  settings = { ...settings, features: ["debug-feature"], bin: "debug-target" };
  await development.debugTests(new vscode.TestRunRequest([oldItem]));
  assert.deepEqual(configs[0].features, ["debug-feature"]);
  assert.equal(configs[0].bin, "debug-target");
  for (const [output, expected] of [["", "skipped"], ["--- PASS: TestSelected/child (0.00s)\n", "skipped"], ["--- PASS: TestOther (0.00s)\n", "skipped"], ["--- FAIL: TestSelected (0.00s)\n", "failed"]]) {
    debugOutput = output;
    const before = records.length;
    await development.debugTests(new vscode.TestRunRequest([oldItem]));
    assert.ok(records.slice(before).some(record => record.action === expected), `debug output ${JSON.stringify(output)} did not report ${expected}`);
    assert.ok(!records.slice(before).some(record => record.action === "passed"), "process success must not invent a test pass");
  }
  settings = { ...settings, bin: "removed-target" };
  await development.runTests(new vscode.TestRunRequest([oldItem]));
  calls = (await fs.readFile(path.join(root, "calls.jsonl"), "utf8")).trim().split("\n").map(JSON.parse);
  assert.equal(calls.filter(args => args[0] === "test").length, 1, "removed test must not execute stale metadata");
  assert.ok(records.some(record => record.action === "appendOutput" && record.args[0].includes("no longer available")));
});
