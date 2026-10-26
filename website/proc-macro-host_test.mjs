import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { test } from "node:test";
import { compiler } from "./ui/compiler.js";

const bytes = new Uint8Array([0, 97, 115, 109]);
const hash = createHash("sha256").update(bytes).digest("hex");
const flush = async () => {
  for (let index = 0; index < 4; index++) await new Promise(setImmediate);
};
const waitFor = async (predicate, description) => {
  // WebCrypto completion is independent of the mocked watchdog clock.
  const deadline = performance.now() + 5000;
  while (!predicate()) {
    assert.ok(performance.now() < deadline, `timed out waiting for ${description}`);
    await new Promise(setImmediate);
  }
};

function environment(t) {
  const original = { Worker: globalThis.Worker, fetch, setTimeout, clearTimeout };
  const workers = [], fetches = [], reports = [], timers = new Map();
  let now = 0, nextTimer = 0;
  globalThis.setTimeout = (callback, delay) => {
    const id = ++nextTimer;
    timers.set(id, { callback, due: now + delay });
    return id;
  };
  globalThis.clearTimeout = (id) => timers.delete(id);
  globalThis.Worker = class {
    constructor(url) { this.url = String(url); this.messages = []; workers.push(this); }
    postMessage(message, transfer = []) { this.messages.push(structuredClone(message, { transfer })); }
    terminate() { this.terminated = true; }
    emit(data) { this.onmessage?.({ data }); }
  };
  globalThis.fetch = (url, { signal }) => new Promise((resolve, reject) => {
    fetches.push({ url, signal, resolve });
    signal.addEventListener("abort", () => reject(new Error("fetch aborted")), { once: true });
  });
  const service = compiler({ websiteCommit: "site", commit: "compiler", wasmSha256: "compiler-hash",
    dependencyContext: { sha256: "roots" }, compileTimeoutMs: 70000 }, (report) => reports.push(report));
  t.after(() => { service.dispose(); Object.assign(globalThis, original); });
  const currentCompiler = () => workers.findLast((worker) => worker.url.startsWith("/worker.js?"));
  const currentMacro = () => workers.findLast((worker) => worker.url.startsWith("/proc-macro-worker.js?"));
  async function start() {
    service.update({ files: [{ path: "playground.gox", source: "package main" }], packageMode: false }, true);
    const worker = currentCompiler();
    worker.emit({ type: "ready", rootsSha256: "roots", macroArtifacts: [hash] });
    await flush();
    return worker;
  }
  async function prepared(content = bytes) {
    const worker = currentCompiler(), previousMacro = currentMacro();
    fetches.at(-1).resolve(new Response(content));
    await waitFor(() => worker.terminated ||
      (currentMacro() && currentMacro() !== previousMacro && !currentMacro().terminated), "macro preparation");
    if (worker.terminated) {
      await flush();
      return;
    }
    currentMacro().emit({ type: "ready" });
    await waitFor(() => worker.terminated || worker.messages.some((message) => message.type === "compile"),
      "compilation after macro readiness");
  }
  async function advance(milliseconds) {
    const end = now + milliseconds;
    for (;;) {
      const next = [...timers].filter(([, timer]) => timer.due <= end).sort((a, b) => a[1].due - b[1].due)[0];
      if (!next) break;
      now = next[1].due; timers.delete(next[0]); next[1].callback(); await flush();
    }
    now = end;
  }
  return { service, workers, fetches, reports, currentCompiler, currentMacro, start, prepared, advance };
}

test("cold macro download may exceed invocation budget before Go compilation starts", async (t) => {
  const env = environment(t), worker = await env.start();
  assert.equal(env.fetches.length, 1);
  await env.advance(6000);
  assert.equal(worker.messages.length, 0, "Go compile waits for verified macro bytes and Worker readiness");
  assert.ok(!worker.terminated);
  env.fetches[0].resolve(new Response(bytes));
  await waitFor(() => env.currentMacro() || worker.terminated, "macro Worker creation");
  assert.equal(worker.messages.length, 0, "download completion still waits for Worker readiness");
  assert.equal(new URL(env.currentMacro().url, "http://localhost").searchParams.get("site"), "site");
  env.currentMacro().emit({ type: "ready" });
  await waitFor(() => worker.terminated || worker.messages.some((message) => message.type === "compile"),
    "compilation after macro readiness");
  assert.equal(worker.messages.at(-1).type, "compile");
  assert.ok(!env.reports.some((report) => report.error));
});

test("prepared artifacts survive recompilation while invocation timeout still terminates the guest", async (t) => {
  const env = environment(t), worker = await env.start();
  await env.prepared();
  const macro = env.currentMacro();
  const invoke = async (id) => {
    worker.emit({ type: "macro", requestId: worker.messages.findLast((message) => message.type === "compile").id,
      id, hash, request: new ArrayBuffer(0), budgetMs: 5000 });
    await flush();
  };
  await invoke(1);
  const firstRun = macro.messages.at(-1);
  macro.emit({ type: "result", id: firstRun.id, response: new ArrayBuffer(0), stderr: new ArrayBuffer(0) });
  await flush();
  worker.emit({ type: "result", id: worker.messages.findLast((message) => message.type === "compile").id, go: "first output" });
  assert.equal(env.reports.at(-1).files[0].source, "first output");
  env.service.compile(); await flush(); await invoke(2);
  assert.equal(env.fetches.length, 1, "recompilation reuses integrity-checked bytes");
  assert.equal(env.currentMacro(), macro);
  assert.deepEqual(new Uint8Array(macro.messages.at(-1).wasm), bytes, "transfers do not detach cached artifacts");
  assert.ok(!macro.terminated);
  await env.advance(4999); assert.ok(!macro.terminated);
  await env.advance(1); assert.equal(macro.terminated, true);
  assert.match(worker.messages.at(-1).error, /time budget exceeded/);
  const requestId = worker.messages.findLast((message) => message.type === "compile").id;
  worker.emit({ type: "result", id: requestId, error: "playground.gox:1:1: macro timed out" });
  assert.equal(env.reports.at(-1).error, "playground.gox:1:1: macro timed out", "Go diagnostic survives guest failure");
  assert.equal(worker.terminated, true, "failed host is discarded after the traced compiler result");
  env.service.compile();
  assert.notEqual(env.currentCompiler(), worker);
});

test("macro preparation retains the 30 second loading watchdog", async (t) => {
  const env = environment(t), worker = await env.start();
  await env.advance(30000);
  assert.match(env.reports.at(-1).error, /加载超过 30 秒/);
  assert.equal(worker.terminated, true);
  assert.equal(env.fetches[0].signal.aborted, true);
  await env.prepared();
  assert.equal(worker.messages.length, 0, "late preparation cannot revive timed-out compilation");
});

test("prepared compilation retains the separate 70 second watchdog", async (t) => {
  const env = environment(t), worker = await env.start();
  await env.prepared(); await env.advance(69999);
  assert.ok(!worker.terminated);
  await env.advance(1);
  assert.match(env.reports.at(-1).error, /编译超过 70 秒/);
  assert.equal(worker.terminated, true);
  assert.equal(env.currentMacro().terminated, true);
});

test("stopping macro preparation aborts download and ignores late readiness", async (t) => {
  const env = environment(t), worker = await env.start();
  env.service.stop();
  assert.equal(env.fetches[0].signal.aborted, true);
  await env.prepared(); await env.advance(30000);
  assert.equal(worker.messages.length, 0);
  assert.match(env.reports.at(-1).status, /已中断/);
  const retry = await env.start(); await env.prepared();
  assert.notEqual(retry, worker);
  assert.equal(retry.messages.at(-1).type, "compile");
});

test("integrity failure prevents compilation and a fresh preparation can recover", async (t) => {
  const env = environment(t), worker = await env.start();
  await env.prepared(new Uint8Array([1, 2, 3]));
  assert.match(env.reports.at(-1).error, /integrity mismatch/);
  assert.equal(worker.messages.length, 0);
  assert.equal(worker.terminated, true);
  const retry = await env.start(); await env.prepared();
  assert.notEqual(retry, worker);
  assert.equal(retry.messages.at(-1).type, "compile");
});
