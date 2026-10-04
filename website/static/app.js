'use strict';
const $ = id => document.getElementById(id);
let manifest, worker, ready = false, busy = false, revision = 0, debounce, timer, selected = 0;
const MAX_BYTES = 32768, COMPILE_MS = 10000, LOAD_MS = 30000;
function status(message, state = '') { $('status').textContent = message; $('status').dataset.state = state; }
function clearOutput() { $('output').value = ''; $('copy').disabled = true; $('diagnostic').hidden = true; }
function kill() { clearTimeout(timer); worker?.terminate(); worker = null; ready = busy = false; $('stop').disabled = true; }
function fail(message) { kill(); clearOutput(); status('转译未完成。修正源码或点击「转译」重试。', 'error'); $('diagnostic').textContent = message; $('diagnostic').hidden = false; }
function send() {
  if (!ready || !worker) return;
  const source = $('source').value;
  if (new TextEncoder().encode(source).length > MAX_BYTES) { fail('源码超过 32 KiB UTF-8 限制。'); return; }
  busy = true; $('stop').disabled = false;
  status('正在转译…');
  timer = setTimeout(() => fail('编译超过 10 秒，Worker 已终止。可修改、重置或重新转译。'), COMPILE_MS);
  worker.postMessage({type:'compile', id:revision, source});
}
function compile() {
  clearTimeout(debounce); revision++; clearOutput();
  if (busy) kill();
  if (ready) { send(); return; }
  if (worker) return; // Loading: the eventual ready handler compiles newest source.
  status('正在载入浏览器编译器…'); $('stop').disabled = false;
  const instance = new Worker('worker.js'); worker = instance;
  timer = setTimeout(() => fail('编译器加载超过 30 秒。请重试。'), LOAD_MS);
  instance.onerror = e => { if (worker === instance) fail(e.message || 'Worker 启动失败'); };
  instance.onmessage = ({data}) => {
    if (worker !== instance) return;
    if (data.type === 'fatal') { fail(data.error); return; }
    if (data.type === 'ready') { clearTimeout(timer); ready = true; send(); return; }
    if (data.type !== 'result' || data.id !== revision) return;
    clearTimeout(timer); busy = false; $('stop').disabled = true;
    if (data.error) { clearOutput(); status('转译失败 · 未保留旧结果', 'error'); $('diagnostic').textContent = data.error; $('diagnostic').hidden = false; return; }
    $('output').value = data.go; $('copy').disabled = false;
    status(`转译完成 · ${new TextEncoder().encode(data.go).length.toLocaleString()} bytes Go · 未执行`);
  };
}
function changed() {
  revision++; clearTimeout(debounce); clearOutput();
  if (busy) kill();
  status('源码已更新，等待转译…'); debounce = setTimeout(compile, 450);
}
function choose(index) { selected = index; $('examples').value = String(index); $('source').value = manifest.examples[index].source; $('example-description').textContent = manifest.examples[index].description; compile(); }
$('source').addEventListener('input', changed);
$('source').addEventListener('keydown', e => { if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); compile(); } });
$('compile').onclick = compile;
$('reset').onclick = () => choose(selected);
$('stop').onclick = () => { revision++; clearTimeout(debounce); kill(); clearOutput(); status('已中断。点击「转译」或「重置示例」可恢复。'); };
$('examples').onchange = () => choose(Number($('examples').value));
$('copy').onclick = async () => {
  try { await navigator.clipboard.writeText($('output').value); status('已复制生成的 Go 源码。'); }
  catch { $('output').focus(); $('output').select(); status('浏览器未允许剪贴板。已选中结果，请按 Ctrl / ⌘ + C。'); }
};
document.querySelectorAll('[data-example]').forEach(link => link.addEventListener('click', () => { if (manifest) choose(manifest.examples.findIndex(e => e.id === link.dataset.example)); }));
(async () => {
  const response = await fetch('examples.json');
  if (!response.ok) throw new Error(`示例清单 HTTP ${response.status}`);
  manifest = await response.json();
  $('examples').replaceChildren(...manifest.examples.map((e,i) => { const option = document.createElement('option'); option.value = i; option.textContent = e.title; return option; }));
  $('version').textContent = `源码提交 ${manifest.commit} · 编译器源摘要 SHA-256 ${manifest.compilerDigest} · ${manifest.toolchain}`;
  $('packages').textContent = manifest.packages.join(' · ');
  for (const example of manifest.cliExamples || []) {
    for (const file of example.files) {
      const detail = document.createElement('details');
      const summary = document.createElement('summary'); summary.textContent = file.path;
      detail.append(summary);
      for (const [label, value] of [['hgo 源码', file.source], ['CLI 生成 Go', file.go]]) {
        const title = document.createElement('p'); title.textContent = label;
        const pre = document.createElement('pre'); pre.tabIndex = 0; pre.textContent = value;
        detail.append(title, pre);
      }
      $('cli-example').append(detail);
    }
    const pre = document.createElement('pre'); pre.textContent = 'CLI 运行输出\n' + example.stdout;
    $('cli-example').append(pre);
  }
  for (const id of ['examples','source','compile','reset']) $(id).disabled = false;
  choose(0);
  if (document.modelContext?.registerTool) {
    const lifecycle = new AbortController();
    addEventListener('pagehide', () => lifecycle.abort(), {once:true});
    Promise.resolve(document.modelContext.registerTool({name:'read_playground',description:'Read the current hgo source, generated Go and compiler status without executing code.', inputSchema:{type:'object',properties:{},additionalProperties:false}, annotations:{readOnlyHint:true,untrustedContentHint:true}, execute(input) { if (!input || typeof input !== 'object' || Object.keys(input).length) throw new Error('expected empty object'); return {source:$('source').value,go:$('output').value,status:$('status').textContent,diagnostic:$('diagnostic').hidden ? '' : $('diagnostic').textContent}; }}, {signal:lifecycle.signal})).catch(() => {});
  }
})().catch(e => fail(String(e)));
