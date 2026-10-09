'use strict';
const release = new URL(self.location.href).searchParams;
const compiler = release.get('compiler');
const expectedHash = release.get('wasm');
importScripts(`wasm_exec.js?compiler=${compiler}`);
const go = new Go();
onmessage = ({data}) => {
  if (data.type !== 'compile') return;
  try { postMessage({type: 'result', id: data.id, ...(Object.hasOwn(data, 'files') ? self.goxideCompilePackage(data.files) : self.goxideCompile(data.source))}); }
  catch (e) { postMessage({type: 'result', id: data.id, error: String(e)}); }
};
(async () => {
  const response = await fetch(`compiler.wasm?compiler=${compiler}`);
  if (!response.ok) throw new Error(`WASM HTTP ${response.status}`);
  const bytes = await response.arrayBuffer();
  const actualHash = Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256', bytes)), b=>b.toString(16).padStart(2,'0')).join('');
  if (!/^[a-f0-9]{64}$/.test(expectedHash || '') || actualHash !== expectedHash) throw new Error('WASM 版本或完整性不匹配，请刷新页面重试');
  const result = await WebAssembly.instantiate(bytes, go.importObject);
  await go.run(result.instance);
})().catch(e => postMessage({type: 'fatal', error: String(e)}));
