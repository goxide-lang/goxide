import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { runInThisContext } from "node:vm";
import { parentPort, workerData } from "node:worker_threads";

const source = new URL(workerData.source);
const listeners = [];
globalThis.self = globalThis;
globalThis.location = { href: source.href };
globalThis.addEventListener = (name, listener) => {
  if (name === "message") listeners.push(listener);
};
globalThis.postMessage = (message, transfer) => parentPort.postMessage(message, transfer);
globalThis.importScripts = (...names) => {
  for (const name of names) {
    const file = new URL(name, source);
    runInThisContext(readFileSync(file, "utf8"), { filename: fileURLToPath(file) });
  }
};
runInThisContext(readFileSync(source, "utf8"), { filename: fileURLToPath(source) });
parentPort.on("message", (data) => {
  parentPort.postMessage({ type: "test-dispatch", id: data.id });
  globalThis.onmessage?.({ data });
  for (const listener of listeners) listener({ data });
});
parentPort.postMessage({ type: "test-ready" });
