// Keep the existing compiler protocol and watchdogs independent of the UI/editor state.
export function compiler(manifest, report) {
  let worker = null,
    ready = false,
    busy = false,
    revision = 0,
    timer,
    debounce,
    input;
  function kill() {
    clearTimeout(timer);
    worker?.terminate();
    worker = null;
    ready = busy = false;
  }
  function fail(error) {
    kill();
    report({ status: "转译未完成", error, files: [] });
  }
  function send() {
    if (!ready || !worker || !input) return;
    if (
      input.files.reduce(
        (sum, file) => sum + new TextEncoder().encode(file.source).length,
        0,
      ) > 32768
    ) {
      fail("源码集合超过 32 KiB UTF-8 限制。");
      return;
    }
    busy = true;
    report({ status: "正在转译…", files: [] });
    timer = setTimeout(() => fail("编译超过 10 秒，Worker 已终止。"), 10000);
    worker.postMessage({
      type: "compile",
      id: revision,
      ...(input.packageMode
        ? { files: input.files }
        : { source: input.files[0].source }),
    });
  }
  function compile() {
    clearTimeout(debounce);
    revision++;
    report({ status: "正在载入浏览器编译器…", files: [] });
    if (busy) kill();
    if (ready) {
      send();
      return;
    }
    if (worker) return;
    const instance = new Worker(
      `/worker.js?site=${manifest.websiteCommit}&compiler=${manifest.commit}&wasm=${manifest.wasmSha256}`,
    );
    worker = instance;
    timer = setTimeout(() => fail("编译器加载超过 30 秒。"), 30000);
    instance.onerror = (e) => {
      if (worker === instance) fail(e.message || "Worker 启动失败");
    };
    instance.onmessage = ({ data }) => {
      if (worker !== instance) return;
      if (data.type === "fatal") {
        fail(data.error);
        return;
      }
      if (data.type === "ready") {
        if (data.rootsSha256 !== manifest.dependencyContext.sha256) {
          fail("项目依赖根与编译器不匹配，请刷新。");
          return;
        }
        clearTimeout(timer);
        ready = true;
        send();
        return;
      }
      if (data.type !== "result" || data.id !== revision) return;
      clearTimeout(timer);
      busy = false;
      if (data.error) {
        report({
          status: "转译失败 · 未保留旧结果",
          error: data.error,
          files: [],
        });
        return;
      }
      report({
        status: "转译完成 · 未执行",
        files: data.files || [{ path: "playground.go", source: data.go }],
      });
    };
  }
  function invalidate() {
    revision++;
    clearTimeout(debounce);
    if (busy) kill();
    input = null;
    report({ status: "源码已更新，等待转译…", files: [] });
  }
  return {
    invalidate,
    update(next, immediate = false) {
      invalidate();
      input = next;
      if (immediate) compile();
      else debounce = setTimeout(compile, 450);
    },
    compile,
    stop() {
      revision++;
      clearTimeout(debounce);
      kill();
      report({ status: "已中断。点击转译可恢复。", files: [] });
    },
    dispose() {
      clearTimeout(debounce);
      kill();
    },
  };
}
export function diagnostics(error, files) {
  const markers = [];
  // Only explicit compiler filename:line:column positions become editor markers.
  for (const line of (error || "").split("\n")) {
    const match = line.match(/(?:^|\s)([^\s:]+\.gox):(\d+):(\d+):\s*(.*)/);
    if (!match) continue;
    const path = files.find((file) => file.path === match[1])?.path;
    if (path)
      markers.push({
        path,
        startLineNumber: Number(match[2]),
        startColumn: Number(match[3]),
        endLineNumber: Number(match[2]),
        endColumn: Number(match[3]) + 1,
        message: match[4] || line,
      });
  }
  return markers;
}
