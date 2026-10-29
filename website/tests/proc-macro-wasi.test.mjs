import assert from "node:assert/strict";
import { execFile } from "node:child_process";
import { mkdtemp, readFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import { runInThisContext } from "node:vm";
import { Worker } from "node:worker_threads";

const repo = fileURLToPath(new URL("../../", import.meta.url));
const fixture = (name) => fileURLToPath(new URL(`fixtures/${name}`, import.meta.url));
const hostScript = new URL("../static/proc-macro-wasi.js", import.meta.url);
const workerScript = new URL("../static/proc-macro-worker.js", import.meta.url);
const encoder = new TextEncoder();
const decoder = new TextDecoder();
const bytes = (text) => encoder.encode(text);
const equalBytes = (actual, expected, message) =>
  assert.deepEqual(Buffer.from(actual), Buffer.from(expected), message);
let api, directory, harness, sdkWASM, sdkCases, environment;

function command(executable, args, { input, ...options } = {}) {
  return new Promise((resolve, reject) => {
    const child = execFile(executable, args, {
      cwd: repo, env: environment, encoding: "buffer", timeout: 90_000,
      maxBuffer: 16 << 20, ...options,
    }, (error, stdout, stderr) => {
      if (error) {
        error.stdout = stdout;
        error.stderr = stderr;
        reject(error);
      } else resolve({ stdout, stderr });
    });
    child.stdin.end(input);
  });
}

before(async () => {
  runInThisContext(await readFile(hostScript, "utf8"), { filename: fileURLToPath(hostScript) });
  api = globalThis.GoxideProcMacroWASI;
  directory = await mkdtemp(join(tmpdir(), "goxide-browser-wasi-"));
  harness = join(directory, "harness");
  environment = { ...process.env };
  for (const key of ["GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS"]) delete environment[key];
  Object.assign(environment, { GOTOOLCHAIN: "local", GOWORK: "off", GOPROXY: "off", GOSUMDB: "off" });
  const go = process.env.GO_BIN || "go";
  const wasm = join(directory, "guest.wasm");
  await Promise.all([
    command(go, ["build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", harness, fixture("proc-macro-harness.go")]),
    command(go, ["build", "-mod=readonly", "-trimpath", "-buildvcs=false", "-o", wasm, fixture("proc-macro-guest.go")], {
      env: { ...environment, GOOS: "wasip1", GOARCH: "wasm", CGO_ENABLED: "0" },
    }),
  ]);
  sdkWASM = new Uint8Array(await readFile(wasm));
  const { stdout } = await command(harness, ["suite", wasm]);
  sdkCases = Object.fromEntries(Object.entries(JSON.parse(stdout)).map(([name, value]) => [name, {
    request: Buffer.from(value.request, "base64"),
    response: value.response && Buffer.from(value.response, "base64"),
  }]));
}, { timeout: 120_000 });

after(async () => {
  if (directory) await rm(directory, { recursive: true, force: true });
});

function profile(input = bytes("abc"), pages = 2) {
  const state = api.createWASI(input);
  const memory = new WebAssembly.Memory({ initial: pages, maximum: pages + 1 });
  state.setMemory(memory);
  return { state, memory, call: state.imports.wasi_snapshot_preview1, view: new DataView(memory.buffer) };
}

function iovec(view, descriptor, pointer, length) {
  view.setUint32(descriptor, pointer, true);
  view.setUint32(descriptor + 4, length, true);
}

test("fixed imports, empty environment, virtual descriptors and bounds match the Go profile", () => {
  const { state, memory, call, view } = profile();
  assert.deepEqual(Object.keys(call).sort(), [
    "args_sizes_get", "args_get", "environ_sizes_get", "environ_get", "fd_read", "fd_write",
    "fd_close", "fd_fdstat_get", "fd_fdstat_set_flags", "fd_prestat_get", "fd_prestat_dir_name",
    "clock_time_get", "random_get", "poll_oneoff", "sched_yield", "proc_exit",
  ].sort());
  assert.equal(call.args_sizes_get(0, 4), 0);
  assert.equal(view.getUint32(0, true), 1);
  assert.equal(view.getUint32(4, true), 18);
  assert.equal(call.args_get(8, 64), 0);
  assert.equal(view.getUint32(8, true), 64);
  equalBytes(new Uint8Array(memory.buffer, 64, 18), bytes("goxide-proc-macro\0"));
  assert.equal(call.environ_sizes_get(0, 4), 0);
  assert.equal(view.getUint32(0, true), 0);
  assert.equal(view.getUint32(4, true), 0);
  assert.equal(call.environ_get(-1, -1), 0);
  assert.equal(call.fd_prestat_get(3, 0), 8);
  assert.equal(call.fd_prestat_dir_name(3, 0, 100), 8);
  assert.equal(call.sched_yield(), 0);

  iovec(view, 0, 100, 8);
  assert.equal(call.fd_read(0, 0, 1, 16), 0);
  assert.equal(view.getUint32(16, true), 3);
  equalBytes(new Uint8Array(memory.buffer, 100, 3), bytes("abc"));
  assert.equal(call.fd_read(0, 0, 1, 16), 0);
  assert.equal(view.getUint32(16, true), 0, "EOF is a successful zero-byte read");
  assert.equal(call.fd_read(1, 0, 1, 16), 8);
  assert.equal(call.fd_write(0, 0, 1, 16), 8);
  assert.equal(call.fd_read(0, 0, 0x80000000, 16), 21);
  assert.equal(call.fd_read(0, 0, 1, -1), 21);
  assert.equal(call.fd_fdstat_set_flags(0, 4), 0);
  assert.equal(call.fd_fdstat_set_flags(0, 1), 58);
  assert.equal(call.fd_fdstat_get(0, 32), 0);
  assert.equal(view.getUint8(32), 2);
  assert.equal(view.getUint16(34, true), 4);
  assert.equal(view.getBigUint64(40, true), (1n << 1n) | (1n << 3n) | (1n << 27n));
  assert.equal(call.fd_fdstat_set_flags(0, 0), 0);
  assert.equal(call.fd_close(1), 0);
  assert.equal(call.fd_close(1), 8);
  assert.equal(call.fd_write(1, 0, 1, 16), 8);
  assert.equal(call.fd_fdstat_get(1, 32), 8);
  assert.equal(call.fd_fdstat_set_flags(1, 0), 8);
  equalBytes(state.finish().response, new Uint8Array());
  assert.throws(() => call.proc_exit(0), (error) => error.exitCode === 0);
  assert.throws(() => profile().call.proc_exit(7), (error) => error.exitCode === 7);
});

test("random stream, logical clock and grown memory preserve the fixed profile", () => {
  const first = profile();
  assert.equal(first.call.random_get(0, 17), 0);
  equalBytes(new Uint8Array(first.memory.buffer, 0, 17), Buffer.from("1f19289dcce78c1742ceefbe0a1fa96afc", "hex"));
  const second = profile();
  for (const [offset, length] of [[0, 3], [3, 0], [3, 7], [10, 7]]) {
    assert.equal(second.call.random_get(offset, length), 0);
  }
  equalBytes(new Uint8Array(second.memory.buffer, 0, 17), new Uint8Array(first.memory.buffer, 0, 17));
  for (const [index, clock] of [0, 1, 0].entries()) {
    assert.equal(second.call.clock_time_get(clock, 999999n, 32), 0);
    assert.equal(second.view.getBigUint64(32, true), BigInt(index) * 1_000_000n);
  }
  assert.equal(second.call.clock_time_get(2, 0n, 32), 28);
  assert.equal(second.call.clock_time_get(0, 0n, -1), 21);
  assert.equal(second.call.clock_time_get(0, 0n, 32), 0);
  assert.equal(second.view.getBigUint64(32, true), 3_000_000n);
  assert.equal(second.call.random_get(-1, 1), 21);
  second.memory.grow(1);
  assert.equal(second.call.random_get(2 * 65536, 1), 0, "host views must follow memory.grow");
});

function subscription(id, kind, value, flags = 0) {
  const data = new Uint8Array(48), view = new DataView(data.buffer);
  view.setBigUint64(0, BigInt(id), true);
  data[8] = kind;
  view.setUint32(16, kind === 0 ? 1 : value, true);
  if (kind === 0) view.setBigUint64(24, BigInt(value), true);
  view.setUint16(40, flags, true);
  return data;
}

function poll(profile, subscriptions, output = 1024) {
  subscriptions.forEach((value, index) => new Uint8Array(profile.memory.buffer).set(value, index * 48));
  assert.equal(profile.call.poll_oneoff(0, output, subscriptions.length, 2048), 0);
  const count = profile.view.getUint32(2048, true), events = [];
  for (let index = 0; index < count; index++) {
    const start = output + index * 32;
    events.push({ id: profile.view.getBigUint64(start, true), errno: profile.view.getUint16(start + 8, true), bytes: profile.view.getBigUint64(start + 16, true) });
  }
  return events;
}

test("poll keeps subscription order, EOF readiness, aliased input and saturated virtual time", () => {
  for (const output of [1024, 0]) {
    const p = profile(new Uint8Array());
    assert.deepEqual(poll(p, [subscription(10, 0, 20), subscription(11, 1, 0), subscription(12, 0, 0), subscription(13, 2, 0)], output), [
      { id: 11n, errno: 0, bytes: 0n }, { id: 12n, errno: 0, bytes: 0n }, { id: 13n, errno: 8, bytes: 0n },
    ]);
    assert.deepEqual(poll(p, [subscription(1, 0, 30), subscription(2, 0, 20), subscription(3, 0, 20)]).map((event) => event.id), [2n, 3n]);
    assert.equal(p.call.clock_time_get(0, 0n, 4096), 0);
    assert.equal(p.view.getBigUint64(4096, true), 20n);
    assert.equal(poll(p, [subscription(4, 0, 1, 1)])[0].errno, 58);
    assert.equal(poll(p, [subscription(5, 0, 1, 2)])[0].errno, 28);
    assert.equal(p.call.fd_close(0), 0);
    assert.equal(poll(p, [subscription(6, 1, 0)])[0].errno, 8);
    assert.equal(p.call.poll_oneoff(0, 1024, 0, 2048), 28);
    assert.equal(p.call.poll_oneoff(0, 1024, 0x80000000, 2048), 21);
    new Uint8Array(p.memory.buffer)[8] = 3;
    assert.equal(p.call.poll_oneoff(0, 1024, 1, 2048), 28);
    assert.equal(p.call.clock_time_get(0, 0n, 4096), 0);
    assert.equal(p.view.getBigUint64(4096, true), 1_000_020n, "immediate/error subscriptions must not advance time");
  }
  const p = profile();
  assert.deepEqual(poll(p, [subscription(1, 1, 0)]), [{ id: 1n, errno: 0, bytes: 3n }]);
  poll(p, [subscription(1, 0, 0xffffffffffffffffn)]);
  for (let index = 0; index < 2; index++) {
    assert.equal(p.call.clock_time_get(0, 0n, 4096), 0);
    assert.equal(p.view.getBigUint64(4096, true), 0xffffffffffffffffn);
  }
});

test("writes are validated before capture and stdout/stderr enforce their individual caps", () => {
  const p = profile();
  new Uint8Array(p.memory.buffer).set(bytes("abc"), 100);
  iovec(p.view, 0, 100, 3);
  iovec(p.view, 8, 0xffffffff, 1);
  assert.equal(p.call.fd_write(1, 0, 2, 32), 21);
  equalBytes(p.state.finish().response, new Uint8Array(), "a rejected write must not publish its valid prefix");
  assert.equal(p.call.fd_write(1, 0, 1, 32), 0);
  assert.equal(p.call.fd_write(2, 0, 1, 32), 0);
  assert.equal(p.view.getUint32(32, true), 3);
  equalBytes(p.state.finish().response, bytes("abc"));
  equalBytes(p.state.finish().stderr, bytes("abc"));
  for (const [fd, limit, name] of [[1, 8 << 20, "stdout"], [2, 64 << 10, "stderr"]]) {
    const bounded = profile(new Uint8Array(), Math.ceil((limit + 64) / 65536));
    iovec(bounded.view, 0, 32, limit);
    assert.equal(bounded.call.fd_write(fd, 0, 1, 16), 0);
    iovec(bounded.view, 0, 32, 1);
    assert.throws(() => bounded.call.fd_write(fd, 0, 1, 16), new RegExp(name));
  }
});

function u32(value) {
  const result = [];
  do {
    let part = value & 127;
    value >>>= 7;
    if (value) part |= 128;
    result.push(part);
  } while (value);
  return result;
}
const name = (text) => [...u32(bytes(text).length), ...bytes(text)];
const moduleBytes = (...sections) => Uint8Array.from([
  0, 97, 115, 109, 1, 0, 0, 0,
  ...sections.flatMap(([id, ...data]) => [id, ...u32(data.length), ...data]),
]);
const table = [4, 1, 0x70, 0, 0], memory = [5, 1, 0, 1];
const exportEntry = (text, kind, index) => [...name(text), kind, ...u32(index)];
const functionBody = (instructions) => [0, ...instructions, 0x0b];
const codeSection = (bodies) => [10, ...u32(bodies.length), ...bodies.flatMap((body) => [...u32(body.length), ...body])];
const callWrite = (descriptor) => [0x41, 1, 0x41, descriptor, 0x41, 1, 0x41, 16, 0x10, 0, 0x1a];

function responseModule(response, { start = "none", conflicts = false } = {}) {
  const data = new Uint8Array(32 + response.length + (start === "write" ? 6 : 0));
  const view = new DataView(data.buffer);
  iovec(view, 0, 32, response.length);
  iovec(view, 8, 32 + response.length, 6);
  data.set(response, 32);
  if (start === "write") data.set(bytes("start/"), 32 + response.length);
  const expected = start === "write" ? 1 : 0;
  const bodies = [functionBody([
    0x23, 0, 0x41, expected, 0x47, 0x04, 0x40, 0, 0x0b,
    0x41, expected + 1, 0x24, 0, ...callWrite(0),
  ])];
  if (start !== "none") bodies.push(functionBody(start === "trap" ? [0] : [...callWrite(8), 0x41, 1, 0x24, 0]));
  const exports = [exportEntry("_start", 0, 1)];
  if (conflicts) {
    for (const prefix of ["__goxide_proc_memory", "__goxide_proc_start"]) {
      exports.push(exportEntry(prefix, 0, 1), exportEntry(`${prefix}_1`, 3, 0));
    }
  }
  return moduleBytes(
    [1, 2, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 0, 0],
    [2, 1, ...name("wasi_snapshot_preview1"), ...name("fd_write"), 0, 0],
    [3, bodies.length, ...bodies.map(() => 1)], table, memory,
    [6, 1, 0x7f, 1, 0x41, 0, 0x0b], [7, exports.length, ...exports.flat()],
    ...(start === "none" ? [] : [[8, 2]]), codeSection(bodies),
    [11, 1, 0, 0x41, 0, 0x0b, ...u32(data.length), ...data],
  );
}

test("WASM framing and resource bounding agree byte-for-byte with Go PrepareWASM", async () => {
  assert.equal(api.PROFILE_ID, "goxide-wasi-command-1");
  assert.deepEqual([api.MAX_WASM_BYTES, api.MAX_MEMORY_PAGES, api.MAX_TABLE_ELEMENTS, api.MAX_MESSAGE_BYTES, api.MAX_DIAGNOSTIC_BYTES], [64 << 20, 4096, 65536, 8 << 20, 64 << 10]);
  for (const input of [sdkWASM, responseModule(bytes("{}")), moduleBytes([4, 1, 0x70, 1, 2, 9], [5, 1, 1, 17, ...u32(9000)])]) {
    const { stdout } = await command(harness, ["prepare"], { input });
    const prepared = api.prepareWASM(input);
    equalBytes(prepared, stdout);
    equalBytes(api.prepareWASM(prepared), prepared);
    const padded = new Uint8Array(input.length + 12);
    padded.set(input, 7);
    equalBytes(api.prepareWASM(padded.subarray(7, 7 + input.length)), prepared);
  }
  for (const input of [
    bytes("not wasm"),
    Uint8Array.from([0, 97, 115, 109, 1, 0, 0, 0, 5, 255]),
    moduleBytes(table, memory, memory), moduleBytes(memory, table),
    moduleBytes(table, [5, 1, 3, 1, 2]), moduleBytes(table, [5, 1, 0, ...u32(4097)]),
    moduleBytes([4, 1, 0x70, 0, ...u32(65537)], memory),
    moduleBytes([4, 1, 0x6f, 0, 0], memory), moduleBytes(table, memory, [13]),
    moduleBytes([2, 1, ...name("wasi_snapshot_preview1"), ...name("path_open"), 0, 0], table, memory),
    moduleBytes([2, 1, 1, 0xff, ...name("fd_write"), 0, 0], table, memory),
    moduleBytes([2, 1, ...name("env"), ...name("memory"), 2, 0, 1], table, memory),
    new Uint8Array((64 << 20) + 1),
  ]) assert.throws(() => api.prepareWASM(input));
});

test("Host creates fresh instances, wires memory before start and avoids private export collisions", async () => {
  const host = new api.Host();
  try {
    const first = responseModule(bytes("fresh"));
    const second = Uint8Array.from([...first, 0, 3, 1, 120, 0]);
    for (const wasm of [first, second, first, second]) {
      const result = await host.run(wasm, bytes("request"));
      equalBytes(result.response, bytes("fresh"));
      equalBytes(result.stderr, new Uint8Array());
    }
    const result = await host.run(responseModule(bytes("main"), { start: "write", conflicts: true }), new ArrayBuffer(0));
    equalBytes(result.response, bytes("start/main"));
    await assert.rejects(host.run(responseModule(bytes("not reached"), { start: "trap" }), bytes("{}")), /trap|unreachable/i);
    equalBytes((await host.run(first, bytes("{}"))).response, bytes("fresh"));
  } finally {
    host.close();
  }
  await assert.rejects(host.run(responseModule(bytes("{}")), bytes("{}")), /closed/i);
});

test("Host caches both artifacts and evicts in FIFO order at 64 MiB", async () => {
  function paddedModule(response, length) {
    const prefix = api.prepareWASM(responseModule(bytes(response)));
    const payloadLength = length - prefix.length - 1 - u32(length).length;
    const custom = [0, ...u32(payloadLength), ...name("padding")];
    assert.equal(prefix.length + 1 + u32(payloadLength).length + payloadLength, length);
    const result = new Uint8Array(length);
    result.set(prefix);
    result.set(custom, prefix.length);
    return result;
  }
  const host = new api.Host();
  const originalCompile = WebAssembly.compile;
  let compiles = 0;
  WebAssembly.compile = function (...args) {
    compiles++;
    return Reflect.apply(originalCompile, this, args);
  };
  try {
    const first = paddedModule("first", 32 << 20);
    const second = paddedModule("second", 32 << 20);
    const third = responseModule(bytes("third"));
    async function run(wasm, response, expectedCompiles) {
      equalBytes((await host.run(wasm, bytes("{}"))).response, bytes(response));
      assert.equal(compiles, expectedCompiles);
    }
    await run(first, "first", 1);
    await run(second, "second", 2);
    await run(second, "second", 2);
    await run(first, "first", 2);
    // The two cached modules total exactly 64 MiB. A cache hit must not
    // refresh insertion order: even though first was used last, it leaves.
    await run(third, "third", 3);
    await run(second, "second", 3);
    await run(first, "first", 4);
    await run(third, "third", 4);
    await run(second, "second", 5);
    await run(first, "first", 5);
  } finally {
    WebAssembly.compile = originalCompile;
    host.close();
  }
});

test("Host rejects wrong command, original start and WASI import signatures", async () => {
  const host = new api.Host();
  try {
    for (const signature of [[0x60, 1, 0x7f, 0], [0x60, 0, 1, 0x7f]]) {
      const instructions = signature.at(-1) === 0x7f ? [0x41, 0] : [];
      const invalid = moduleBytes([1, 1, ...signature], [3, 1, 0], table, memory,
        [7, 1, ...exportEntry("_start", 0, 0)], codeSection([functionBody(instructions)]));
      await assert.rejects(host.run(invalid, bytes("{}")), /start|signature|parameter|result/i);
    }
    const invalidStart = moduleBytes([1, 2, 0x60, 0, 0, 0x60, 1, 0x7f, 0], [3, 2, 0, 1], table, memory,
      [7, 1, ...exportEntry("_start", 0, 0)], [8, 1], codeSection([functionBody([]), functionBody([])]));
    await assert.rejects(host.run(invalidStart, bytes("{}")), /start|signature|parameter/i);
    const invalidImport = moduleBytes([1, 1, 0x60, 0, 0],
      [2, 1, ...name("wasi_snapshot_preview1"), ...name("fd_write"), 0, 0], [3, 1, 0], table, memory,
      [7, 1, ...exportEntry("_start", 0, 1)], codeSection([functionBody([])]));
    await assert.rejects(host.run(invalidImport, bytes("{}")), /fd_write|signature|import/i);
    await assert.rejects(host.run(responseModule(bytes("{}")), new Uint8Array((8 << 20) + 1)), /request|input|limit|exceed/i);
  } finally {
    host.close();
  }
});

test("the real Go SDK command has the same native and browser responses and fresh state", async () => {
  const host = new api.Host();
  try {
    for (const entry of ["echo", "state", "state", "echo", "diagnostic"]) {
      const expected = sdkCases[entry];
      const result = await host.run(sdkWASM, expected.request);
      equalBytes(result.response, expected.response, `${entry} differs from the production Go WASI host`);
      equalBytes(result.stderr, new Uint8Array());
    }
    for (const [entry, message] of [["stdout_limit", /stdout/i], ["stderr_limit", /stderr/i], ["panic", /browser panic fixture/], ["exit", /7/]]) {
      await assert.rejects(host.run(sdkWASM, sdkCases[entry].request), message);
      equalBytes((await host.run(sdkWASM, sdkCases.echo.request)).response, sdkCases.echo.response);
    }
  } finally {
    host.close();
  }
});

test("raw candidate responses still require the caller's strict Go SDK admission", async () => {
  const host = new api.Host();
  try {
    const valid = await host.run(sdkWASM, sdkCases.echo.request);
    const admitted = await command(harness, ["admit", "echo"], { input: valid.response });
    assert.equal(decoder.decode(admitted.stdout).trim(), "42");
    for (const entry of ["trailing_output", "unknown_reference"]) {
      const result = await host.run(sdkWASM, sdkCases[entry].request);
      await assert.rejects(command(harness, ["admit", entry], { input: result.response }), (error) => {
        assert.match(decoder.decode(error.stderr), /trailing|invalid|reference|JSON/i);
        return true;
      });
    }
  } finally {
    host.close();
  }
});

function workerMessage(worker, predicate, timeout = 10_000) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => finish(new Error("worker response timed out")), timeout);
    const message = (value) => { if (predicate(value)) finish(null, value); };
    const errorListener = (value) => finish(value);
    const exit = (code) => finish(new Error(`worker exited (${code})`));
    function finish(error, value) {
      clearTimeout(timer);
      worker.off("message", message);
      worker.off("error", errorListener);
      worker.off("exit", exit);
      if (error) reject(error); else resolve(value);
    }
    worker.on("message", message);
    worker.on("error", errorListener);
    worker.on("exit", exit);
  });
}

async function openWorker() {
  const worker = new Worker(new URL("fixtures/classic-proc-macro-worker.mjs", import.meta.url), { workerData: { source: workerScript.href } });
  try {
    await workerMessage(worker, (message) => message.type === "test-ready");
    return worker;
  } catch (error) {
    await worker.terminate();
    throw error;
  }
}

function runWorker(worker, id, entry) {
  const result = workerMessage(worker, (message) => message.type === "result" && message.id === id);
  const wasm = sdkWASM.slice().buffer, request = new Uint8Array(sdkCases[entry].request);
  worker.postMessage({ type: "run", id, wasm, request }, [wasm]);
  return result;
}

test("classic worker reports IDs, externally terminates nontermination and recovers in a new worker", { timeout: 30_000 }, async (t) => {
  const worker = await openWorker();
  t.after(() => worker.terminate());
  const first = await runWorker(worker, 101, "echo");
  assert.ok(first.response instanceof ArrayBuffer);
  assert.ok(first.stderr instanceof ArrayBuffer);
  equalBytes(first.response, sdkCases.echo.response);
  const failed = await runWorker(worker, 102, "panic");
  assert.match(failed.error, /browser panic fixture/);
  assert.equal(failed.response, undefined);

  const dispatched = workerMessage(worker, (message) => message.type === "test-dispatch" && message.id === 103);
  const pending = runWorker(worker, 103, "response_loop");
  const settled = pending.then(() => "result", () => "error");
  await dispatched;
  assert.equal(await Promise.race([settled, delay(100, "pending")]), "pending", "stdout alone must not complete a command that is still running");
  await worker.terminate();
  await assert.rejects(pending, /worker exited/);

  const recovered = await openWorker();
  t.after(() => recovered.terminate());
  equalBytes((await runWorker(recovered, 104, "echo")).response, sdkCases.echo.response);
});
