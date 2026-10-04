'use strict';
importScripts('wasm_exec.js');
const go = new Go();
onmessage = ({data}) => {
  if (data.type !== 'compile') return;
  try { postMessage({type: 'result', id: data.id, ...self.hgoCompile(data.source)}); }
  catch (e) { postMessage({type: 'result', id: data.id, error: String(e)}); }
};
(async () => {
  const response = await fetch('compiler.wasm');
  if (!response.ok) throw new Error(`WASM HTTP ${response.status}`);
  const result = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
  await go.run(result.instance);
})().catch(e => postMessage({type: 'fatal', error: String(e)}));
