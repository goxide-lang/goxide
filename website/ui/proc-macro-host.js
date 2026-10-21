// The page owns this Worker and its deadlines. Terminating a compiler Worker
// does not terminate Workers it created, so macro execution must live here.
export function createProcMacroHost(compilerCommit, allowedHashes) {
  const allowed = new Set(allowedHashes);
  const query = new URLSearchParams({ compiler: compilerCommit });
  let worker = null, invocation = null, nextID = 0, closed = false;

  function stop(error) {
    const previous = worker;
    worker = null;
    previous?.terminate();
    invocation?.finish(error);
  }

  return {
    close(reason = "procedural macro host is closed") {
      closed = true;
      stop(new Error(reason));
    },

    async run(hash, request, budgetMs) {
      if (closed) throw new Error("procedural macro host is closed");
      if (!allowed.has(hash)) throw new Error("procedural macro artifact is not bundled with this compiler");
      if (invocation) throw new Error("procedural macro host already has an active invocation");
      if (!(request instanceof ArrayBuffer) || request.byteLength > 8 * 1024 * 1024) {
        throw new Error("invalid procedural macro request");
      }
      if (!Number.isFinite(budgetMs) || budgetMs <= 0 || budgetMs > 5000) {
        throw new Error("invalid procedural macro execution budget");
      }
      const input = request.slice(0), id = ++nextID;
      const abort = new AbortController();
      return new Promise((resolve, reject) => {
        const current = {
          finish(error, value) {
            if (invocation !== current) return;
            clearTimeout(timer);
            invocation = null;
            abort.abort();
            if (error) reject(error); else resolve(value);
          },
        };
        invocation = current;
        // The timer remains on the page even when guest init or execution
        // blocks the macro Worker's event loop.
        const timer = setTimeout(() => {
          if (invocation === current) stop(new Error("procedural macro invocation time budget exceeded"));
        }, budgetMs);
        (async () => {
          const result = await fetch(`/proc-macros/${hash}.wasm?${query}`, { signal: abort.signal });
          if (invocation !== current) return;
          if (!result.ok) throw new Error(`procedural macro WASM HTTP ${result.status}`);
          if (!result.body) throw new Error("procedural macro WASM response has no body");
          const chunks = [], reader = result.body.getReader();
          let length = 0;
          try {
            for (;;) {
              const { value, done } = await reader.read();
              if (invocation !== current) return;
              if (done) break;
              length += value.byteLength;
              if (length > 64 * 1024 * 1024) throw new Error("procedural macro WASM exceeds 64 MiB");
              chunks.push(value);
            }
          } finally {
            reader.releaseLock();
          }
          const wasm = new Uint8Array(length);
          let offset = 0;
          for (const chunk of chunks) { wasm.set(chunk, offset); offset += chunk.byteLength; }
          const actual = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", wasm)), byte => byte.toString(16).padStart(2, "0")).join("");
          if (invocation !== current) return;
          if (actual !== hash) throw new Error("procedural macro artifact integrity mismatch");
          if (!worker) worker = new Worker(`/proc-macro-worker.js?${query}`);
          const instance = worker;
          const matches = () => worker === instance && invocation === current;
          instance.onerror = event => {
            if (matches()) stop(new Error(event.message || "procedural macro Worker failed"));
          };
          instance.onmessageerror = () => {
            if (matches()) stop(new Error("procedural macro Worker returned an unreadable message"));
          };
          instance.onmessage = ({ data }) => {
            if (!matches() || !data || data.type !== "result" || data.id !== id) return;
            if (data.error) { stop(new Error(data.error)); return; }
            if (!(data.response instanceof ArrayBuffer) || data.response.byteLength > 8 * 1024 * 1024 ||
                !(data.stderr instanceof ArrayBuffer) || data.stderr.byteLength > 64 * 1024) {
              stop(new Error("invalid procedural macro response buffers"));
              return;
            }
            current.finish(null, { response: data.response, stderr: data.stderr });
          };
          instance.postMessage({ type: "run", id, wasm: wasm.buffer, request: input }, [wasm.buffer, input]);
        })().catch(error => {
          if (invocation === current) stop(error);
        });
      });
    },
  };
}
