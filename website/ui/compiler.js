import { createProcMacroHost } from "./proc-macro-host.js";

export function compileTimeoutMs(manifest) {
  const value = manifest.compileTimeoutMs;
  return Number.isInteger(value) && value > 0 && value <= 70000 ? value : 10000;
}

// Keep the compiler protocol and watchdogs independent of UI/editor state.
export function compiler(manifest, report) {
  let worker = null,
    ready = false,
    busy = false,
    revision = 0,
    timer,
    debounce,
    input,
    macroHost = null,
    macroFailed = false;
  function kill() {
    clearTimeout(timer);
    macroHost?.close();
    macroHost = null;
    macroFailed = false;
    worker?.terminate();
    worker = null;
    ready = busy = false;
  }
  function fail(error) {
    kill();
    report({ status: "转译未完成", error, files: [] });
  }
  function send() {
    if (!ready || !worker || !input || busy) return;
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
    const instance = worker, requestId = revision;
    report({ status: "正在转译…", files: [] });
    const timeout = compileTimeoutMs(manifest);
    timer = setTimeout(() => {
      if (worker === instance && revision === requestId && busy)
        fail(`编译超过 ${timeout / 1000} 秒，已请求终止 Worker。`);
    }, timeout);
    instance.postMessage({
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
    timer = setTimeout(() => {
      if (worker === instance && !ready) fail("编译器加载超过 30 秒。");
    }, 30000);
    instance.onerror = (e) => {
      if (worker === instance) fail(e.message || "Worker 启动失败");
    };
    instance.onmessage = ({ data }) => {
      if (worker !== instance || !data || typeof data !== "object") return;
      if (data.type === "fatal") {
        fail(data.error);
        return;
      }
      if (data.type === "ready") {
        if (ready) return;
        if (data.rootsSha256 !== manifest.dependencyContext.sha256) {
          fail("项目依赖根与编译器不匹配，请刷新。");
          return;
        }
        const artifacts = data.macroArtifacts ?? [];
        if (!Array.isArray(artifacts) || artifacts.length > 64 || artifacts.some(hash => typeof hash !== "string" || !/^[a-f0-9]{64}$/.test(hash))) {
          fail("内置宏目录无效，请刷新。");
          return;
        }
        if (macroHost) return;
        const host = macroHost = createProcMacroHost(manifest.commit, artifacts, manifest.websiteCommit);
        const loaded = () => {
          if (worker !== instance || macroHost !== host) return;
          clearTimeout(timer);
          ready = true;
          send();
        };
        if (artifacts.length === 0) loaded();
        else host.prepare().then(loaded, error => {
          if (worker === instance && macroHost === host)
            fail(error instanceof Error ? error.message : String(error));
        });
        return;
      }
      if (data.type === "macro-cancel") {
        if (busy && data.requestId === revision) {
          macroHost?.close("procedural macro execution canceled");
          macroHost = null;
          macroFailed = true;
        }
        return;
      }
      if (data.type === "macro") {
        if (!busy || data.requestId !== revision || !macroHost) return;
        if (!Number.isSafeInteger(data.id) || data.id < 1) {
          fail("宏请求标识无效。");
          return;
        }
        const host = macroHost, requestId = revision, id = data.id;
        const current = () => worker === instance && revision === requestId && busy && macroHost === host;
        const reply = (message, transfer = []) => {
          if (!current()) return;
          try { instance.postMessage({ type: "macro-result", requestId, id, ...message }, transfer); }
          catch (error) { fail(error instanceof Error ? error.message : String(error)); }
        };
        host.run(data.hash, data.request, data.budgetMs).then(
          result => reply(result, [result.response, result.stderr]),
          error => {
            if (current()) macroFailed = true;
            reply({ error: error instanceof Error ? error.message : String(error) });
          },
        );
        return;
      }
      if (data.type !== "result" || data.id !== revision || !busy) return;
      clearTimeout(timer);
      busy = false;
      if (macroFailed) kill();
      const messages = typeof data.diagnostics === "string" ? data.diagnostics : "";
      if (data.error) {
        report({
          status: "转译失败 · 未保留旧结果",
          error: data.error,
          messages,
          files: [],
        });
        return;
      }
      report({
        status: "转译完成 · 未执行",
        messages,
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
        severity: /^(warning|note):/.exec(match[4])?.[1] || "error",
      });
  }
  return markers;
}
