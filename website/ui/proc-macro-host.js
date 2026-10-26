// The page owns this Worker and its deadlines. Terminating a compiler Worker
// does not terminate Workers it created, so macro execution must live here.
export function createProcMacroHost(compilerCommit, allowedHashes, websiteCommit) {
  const allowed = new Set(allowedHashes);
  const query = new URLSearchParams({ compiler: compilerCommit, site: websiteCommit });
  const artifacts = new Map(), abort = new AbortController();
  let worker = null, invocation = null, startup = null, loading = null, nextID = 0, closed = false;

  function stop(error) {
    const previous = worker;
    worker = null;
    previous?.terminate();
    startup?.reject(error);
    startup = null;
    invocation?.finish(error);
  }

  return {
    close(reason = "procedural macro host is closed") {
      closed = true;
      abort.abort();
      artifacts.clear();
      stop(new Error(reason));
    },

    prepare() {
      if (closed) return Promise.reject(new Error("procedural macro host is closed"));
      if (loading) return loading;
      // Downloads and Worker script loading belong to the compiler's loading
      // watchdog, before Go starts the per-invocation execution clock.
      loading = (async () => {
        await Promise.all([...allowed].map(async hash => {
          const result = await fetch(`/proc-macros/${hash}.wasm?${query}`, { signal: abort.signal });
          if (!result.ok) throw new Error(`procedural macro WASM HTTP ${result.status}`);
          if (!result.body) throw new Error("procedural macro WASM response has no body");
          const chunks = [], reader = result.body.getReader();
          let length = 0;
          try {
            for (;;) {
              const { value, done } = await reader.read();
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
          if (closed) throw new Error("procedural macro host is closed");
          if (actual !== hash) throw new Error("procedural macro artifact integrity mismatch");
          artifacts.set(hash, wasm);
        }));
        if (closed) throw new Error("procedural macro host is closed");
        if (allowed.size === 0) return;
        await new Promise((resolve, reject) => {
          startup = { resolve, reject };
          worker = new Worker(`/proc-macro-worker.js?${query}`);
          const instance = worker;
          instance.onerror = event => {
            if (worker === instance) stop(new Error(event.message || "procedural macro Worker failed"));
          };
          instance.onmessageerror = () => {
            if (worker === instance) stop(new Error("procedural macro Worker returned an unreadable message"));
          };
          instance.onmessage = ({ data }) => {
            if (worker !== instance || !data || typeof data !== "object") return;
            if (data.type === "ready") {
              startup?.resolve();
              startup = null;
              return;
            }
            const current = invocation;
            if (!current || data.type !== "result" || data.id !== current.id) return;
            if (data.error) { stop(new Error(data.error)); return; }
            if (!(data.response instanceof ArrayBuffer) || data.response.byteLength > 8 * 1024 * 1024 ||
                !(data.stderr instanceof ArrayBuffer) || data.stderr.byteLength > 64 * 1024) {
              stop(new Error("invalid procedural macro response buffers"));
              return;
            }
            current.finish(null, { response: data.response, stderr: data.stderr });
          };
        });
      })().catch(error => { abort.abort(); stop(error); throw error; });
      return loading;
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
      if (!worker || startup || !artifacts.has(hash)) throw new Error("procedural macro host is not prepared");
      const input = request.slice(0), wasm = artifacts.get(hash).slice(), id = ++nextID;
      return new Promise((resolve, reject) => {
        const current = {
          id,
          finish(error, value) {
            if (invocation !== current) return;
            clearTimeout(timer);
            invocation = null;
            if (error) reject(error); else resolve(value);
          },
        };
        invocation = current;
        // The page deadline still covers engine compilation, fresh guest init
        // and execution, even when the macro Worker's event loop is blocked.
        const timer = setTimeout(() => {
          if (invocation === current) stop(new Error("procedural macro invocation time budget exceeded"));
        }, budgetMs);
        try { worker.postMessage({ type: "run", id, wasm: wasm.buffer, request: input }, [wasm.buffer, input]); }
        catch (error) { stop(error); }
      });
    },
  };
}
