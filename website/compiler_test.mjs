import assert from "node:assert/strict";
import { compiler } from "./ui/compiler.js";
class FakeWorker {
  static instances = [];
  constructor() {
    this.requests = [];
    FakeWorker.instances.push(this);
  }
  postMessage(data) {
    this.requests.push(data);
  }
  terminate() {
    this.terminated = true;
  }
  emit(data) {
    this.onmessage({ data });
  }
}
globalThis.Worker = FakeWorker;
const manifest = {
  websiteCommit: "site",
  commit: "compiler",
  wasmSha256: "hash",
  dependencyContext: { sha256: "roots" },
};
const input = (source) => ({
  files: [{ path: "playground.gox", source }],
  packageMode: false,
});
for (const phase of ["loading", "busy"]) {
  const reports = [],
    service = compiler(manifest, (data) => reports.push(data));
  service.update(input("old"), true);
  const previous = FakeWorker.instances.at(-1);
  if (phase === "busy") previous.emit({ type: "ready", rootsSha256: "roots" });
  const id = previous.requests[0]?.id;
  // The source event precedes React's effect. Neither ready nor result may send/revive old input.
  service.invalidate();
  if (phase === "loading") {
    previous.emit({ type: "ready", rootsSha256: "roots" });
    assert.equal(previous.requests.length, 0);
  } else previous.emit({ type: "result", id, go: "old result" });
  assert.equal(reports.at(-1).files.length, 0);
  assert.ok(
    !reports.some((report) =>
      report.files.some((file) => file.source === "old result"),
    ),
  );
  service.update(input("new"), true);
  const current = FakeWorker.instances.at(-1);
  if (current !== previous)
    current.emit({ type: "ready", rootsSha256: "roots" });
  assert.equal(current.requests.at(-1).source, "new");
  current.emit({
    type: "result",
    id: current.requests.at(-1).id,
    go: "new result",
  });
  assert.equal(reports.at(-1).files[0].source, "new result");
  service.dispose();
}
console.log(
  "compiler synchronous invalidation passed: loading/busy stale events before React effect",
);
