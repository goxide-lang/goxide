'use strict';
const release = new URL(self.location.href).searchParams;
const compiler = release.get('compiler');
const expectedHash = release.get('wasm');
importScripts(`wasm_exec.js?compiler=${compiler}`);
const go = new Go();
const send = self.postMessage.bind(self);
let active = null, invocation = null, nextInvocation = 0;

function finishMacro(error, value) {
  const pending = invocation;
  invocation = null;
  if (pending) {
    if (error) pending.reject(error); else pending.resolve(value);
  }
}

// The page owns the macro Worker, fetch, integrity checks and hard deadline.
// This compiler Worker only relays requests while its Go goroutine is awaiting.
self.goxideRunProcMacro = async (hash, request, budgetMs) => {
  if (!active || active.canceled) throw Error('procedural macro request canceled');
  if (invocation) throw Error('procedural macro host already has an active invocation');
  if (!(request instanceof Uint8Array) || request.byteLength > 8 * 1024 * 1024) throw Error('invalid procedural macro request');
  if (!Number.isFinite(budgetMs) || budgetMs <= 0 || budgetMs > 5000) throw Error('invalid procedural macro execution budget');
  const id = ++nextInvocation, requestId = active.id, input = request.slice();
  return new Promise((resolve, reject) => {
    invocation = { id, requestId, resolve, reject };
    try {
      send({ type: 'macro', requestId, id, hash, request: input.buffer, budgetMs }, [input.buffer]);
    } catch (error) { finishMacro(error); }
  });
};

onmessage = async ({ data }) => {
  if (!data || typeof data !== 'object') return;
  if (data.type === 'macro-result') {
    if (!invocation || invocation.id !== data.id || invocation.requestId !== data.requestId || active?.id !== data.requestId || active.canceled) return;
    finishMacro(data.error ? Error(data.error) : null, { response: data.response, stderr: data.stderr });
    return;
  }
  if (data.type === 'cancel') {
    if (active?.id === data.id) {
      active.canceled = true;
      send({ type: 'macro-cancel', requestId: active.id });
      finishMacro(Error('procedural macro execution canceled'));
    }
    return;
  }
  if (data.type !== 'compile') return;
  if (active) { send({ type: 'result', id: data.id, error: 'compiler already has an active request' }); return; }
  active = { id: data.id, canceled: false };
  try {
    const result = await (Object.hasOwn(data, 'files') ? self.goxideCompilePackage(data.files) : self.goxideCompile(data.source));
    send({ type: 'result', id: data.id, ...(active.canceled ? { error: 'compilation canceled' } : result) });
  } catch (error) { send({ type: 'result', id: data.id, error: String(error) }); }
  finally { finishMacro(Error('compilation finished')); active = null; }
};

(async () => {
  const response = await fetch(`compiler.wasm?compiler=${compiler}`);
  if (!response.ok) throw new Error(`WASM HTTP ${response.status}`);
  const bytes = await response.arrayBuffer();
  const actualHash = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)), b => b.toString(16).padStart(2, '0')).join('');
  if (!/^[a-f0-9]{64}$/.test(expectedHash || '') || actualHash !== expectedHash) throw new Error('WASM 版本或完整性不匹配，请刷新页面重试');
  const result = await WebAssembly.instantiate(bytes, go.importObject);
  await go.run(result.instance);
})().catch(e => send({ type: 'fatal', error: String(e) }));
