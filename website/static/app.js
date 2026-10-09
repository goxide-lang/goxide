'use strict';
const $ = id => document.getElementById(id);
let manifest, examples, worker, ready = false, busy = false, revision = 0, debounce, timer, selected = 0;
let files = [], activeFile = 0, packageMode = false, generatedFiles = [], activeGenerated = 0;
const MAX_BYTES = 32768, MAX_FILES = 16, COMPILE_MS = 10000, LOAD_MS = 30000;
function status(message, state = '') { $('status').textContent = message; $('status').dataset.state = state; }
function clearOutput() {
  generatedFiles = []; activeGenerated = 0; $('generated-files').replaceChildren();
  $('output').value = ''; $('output-path').textContent = '生成结果 / Go';
  $('copy').disabled = true; $('diagnostic').hidden = true;
}
function tabs(container, entries, active, select) {
  $(container).replaceChildren(...entries.map((file, index) => {
    const button = document.createElement('button'); button.type = 'button';
    button.textContent = file.path; button.setAttribute('aria-pressed', String(index === active));
    button.onclick = () => select(index); return button;
  }));
}
function showSource(index = activeFile) {
  activeFile = index; $('source').value = files[index].source;
  $('source-path').textContent = '源码 / ' + files[index].path;
  $('delete-file').disabled = files.length <= 1;
  tabs('source-files', files, index, showSource);
  $('input-mode').textContent = packageMode ? `同包 · ${files.length} 个文件` : '单文件兼容入口';
}
function showGenerated(index = activeGenerated) {
  activeGenerated = index; $('output').value = generatedFiles[index].source;
  $('output-path').textContent = '生成结果 / ' + generatedFiles[index].path;
  tabs('generated-files', generatedFiles, index, showGenerated);
}
function kill() { clearTimeout(timer); worker?.terminate(); worker = null; ready = busy = false; $('stop').disabled = true; }
function fail(message) { kill(); clearOutput(); status('转译未完成。修正源码或点击「转译」重试。', 'error'); $('diagnostic').textContent = message; $('diagnostic').hidden = false; }
function send() {
  if (!ready || !worker) return;
  if (files.reduce((size, file) => size + new TextEncoder().encode(file.source).length, 0) > MAX_BYTES) { fail('源码集合超过 32 KiB UTF-8 限制。'); return; }
  busy = true; $('stop').disabled = false;
  status('正在转译…');
  timer = setTimeout(() => fail('编译超过 10 秒，Worker 已终止。可修改、重置或重新转译。'), COMPILE_MS);
  worker.postMessage({type:'compile', id:revision, ...(packageMode ? {files:files.map(file => ({...file}))} : {source:files[0].source})});
}
function compile() {
  if (!manifest || !files.length) return;
  clearTimeout(debounce); revision++; clearOutput();
  if (busy) kill();
  if (ready) { send(); return; }
  if (worker) return; // Loading: the eventual ready handler compiles newest files.
  status('正在载入浏览器编译器…'); $('stop').disabled = false;
  const instance = new Worker(`worker.js?site=${manifest.websiteCommit}&compiler=${manifest.commit}&wasm=${manifest.wasmSha256}`); worker = instance;
  timer = setTimeout(() => fail('编译器加载超过 30 秒。请重试。'), LOAD_MS);
  instance.onerror = e => { if (worker === instance) fail(e.message || 'Worker 启动失败'); };
  instance.onmessage = ({data}) => {
    if (worker !== instance) return;
    if (data.type === 'fatal') { fail(data.error); return; }
    if (data.type === 'ready') { clearTimeout(timer); ready = true; send(); return; }
    if (data.type !== 'result' || data.id !== revision) return;
    clearTimeout(timer); busy = false; $('stop').disabled = true;
    if (data.error) { clearOutput(); status('转译失败 · 未保留旧结果', 'error'); $('diagnostic').textContent = data.error; $('diagnostic').hidden = false; return; }
    generatedFiles = data.files || [{path:'playground.go', source:data.go}];
    showGenerated(0); $('copy').disabled = false;
    const bytes = generatedFiles.reduce((size, file) => size + new TextEncoder().encode(file.source).length, 0);
    status(`转译完成 · ${generatedFiles.length} 个 Go 文件 · ${bytes.toLocaleString()} bytes Go · 未执行`);
  };
}
function changed() {
  revision++; clearTimeout(debounce); clearOutput();
  if (busy) kill();
  status('源码已更新，等待转译…'); debounce = setTimeout(compile, 450);
}
function choose(index) {
  selected = index; $('examples').value = String(index); const example = examples[index];
  packageMode = Array.isArray(example.files);
  files = packageMode ? example.files.map(file => ({...file})) : [{path:'playground.gox', source:example.source}];
  $('example-description').textContent = example.description; $('file-error').hidden = true; $('new-file-name').value = '';
  showSource(0); compile();
}
$('source').addEventListener('input', () => { files[activeFile].source = $('source').value; changed(); });
$('source').addEventListener('keydown', e => { if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); compile(); } });
$('add-file').onclick = () => {
  const path = $('new-file-name').value;
  let error = '';
  if (!/^[A-Za-z][A-Za-z0-9_-]*\.gox$/.test(path) || path.endsWith('_test.gox') || path.startsWith('goxide_')) error = '请输入普通 .gox 文件名（字母开头，无目录；不接受测试或保留文件名）。';
  else if (files.some(file => file.path.toLowerCase() === path.toLowerCase())) error = '文件名已存在。';
  else if (files.length >= MAX_FILES) error = '最多 16 个同包文件。';
  $('file-error').textContent = error; $('file-error').hidden = !error;
  if (error) return;
  const packageName = files.map(file => file.source.replace(/\/\*[\s\S]*?\*\/|\/\/[^\n]*/g, ' ').match(/^\s*package\s+([\p{L}_][\p{L}\p{Nd}_]*)/mu)?.[1]).find(Boolean) || 'main';
  files.push({path, source:`package ${packageName}\n`}); packageMode = true;
  $('new-file-name').value = ''; showSource(files.length - 1); changed();
};
$('delete-file').onclick = () => {
  if (files.length <= 1) return;
  files.splice(activeFile, 1); $('file-error').hidden = true;
  showSource(Math.min(activeFile, files.length - 1)); changed();
};
$('compile').onclick = compile;
$('reset').onclick = () => choose(selected);
$('stop').onclick = () => { revision++; clearTimeout(debounce); kill(); clearOutput(); status('已中断。点击「转译」或「重置示例」可恢复。'); };
$('examples').onchange = () => choose(Number($('examples').value));
$('copy').onclick = async () => {
  const currentRevision = revision, path = generatedFiles[activeGenerated]?.path;
  try {
    await navigator.clipboard.writeText($('output').value);
    if (revision === currentRevision && generatedFiles[activeGenerated]?.path === path) status('已复制完整 Go 文件：' + path);
  } catch {
    if (revision !== currentRevision || generatedFiles[activeGenerated]?.path !== path) return;
    $('output').focus(); $('output').select(); status('浏览器未允许剪贴板。已选中结果，请按 Ctrl / ⌘ + C。');
  }
};
function renderDependencies() {
  const dependencies = manifest.dependencies || [];
  const option = (value, label) => { const node = document.createElement('option'); node.value = value; node.textContent = label; return node; };
  $('dependencies').replaceChildren(...dependencies.map((dependency, index) => option(index, dependency.package)));
  function showFile() {
    const dependency = dependencies[Number($('dependencies').value)];
    const file = dependency?.files[Number($('dependency-files').value)];
    $('dependency-source').value = file?.source || '';
    $('dependency-provenance').textContent = dependency && file ? `${dependency.package} @ ${dependency.version} · ${dependency.source} · ${file.path} · SHA-256 ${file.sha256}` : '本构建没有随附可核验依赖源码。';
  }
  function showDependency() {
    const dependency = dependencies[Number($('dependencies').value)];
    $('dependency-files').replaceChildren(...(dependency?.files || []).map((file, index) => option(index, file.path)));
    showFile();
  }
  $('dependencies').onchange = showDependency; $('dependency-files').onchange = showFile; showDependency();
}
document.querySelectorAll('[data-example]').forEach(link => link.addEventListener('click', () => { if (manifest) choose(examples.findIndex(e => e.id === link.dataset.example)); }));
function renderCatalog() {
  const catalog = manifest.catalog;
  if (!catalog || catalog.compilerCommit !== manifest.commit) throw new Error('特性目录与编译器版本不一致');
  const el = (tag, text, cls) => { const node = document.createElement(tag); if (text !== undefined) node.textContent = text; if (cls) node.className = cls; return node; };
  const code = text => { const node = el('pre', text); node.tabIndex = 0; return node; };
  const details = (title, text) => { const d=el('details'); d.append(el('summary',title)); if(text!==undefined)d.append(code(text)); return d; };
  const supportLabels = {browser:'当前版本 · 浏览器可转译',cli:'当前版本 · 仅 CLI 工程',unavailable:'当前线上不可用 · 设计 / 开发中'};
  $('catalog-version').textContent = `${manifest.commit} · ${manifest.toolchain} · metadata ${manifest.metadataSchema} · 复核 ${catalog.reviewedAt}`;
  $('catalog-rules').replaceChildren(...catalog.rules.map(rule=>el('li',rule)));
  for (const [value,label] of Object.entries(catalog.statusLabels)) { const option=el('option',label);option.value=value;$('catalog-status').append(option); }
  let topic='all';
  for (const [value,label] of [['all','全部主题'],...Object.entries(catalog.topics)]) {
    const button=el('button',label);button.type='button';button.dataset.topic=value;button.setAttribute('aria-pressed',String(value==='all'));
    button.onclick=()=>{topic=value;filter();};$('catalog-topics').append(button);
  }
  for(const item of catalog.items) {
    const article=el('article',undefined,'feature-card');article.id='feature-'+item.id;article.dataset.feature=item.id;
    const meta=el('div',undefined,'feature-meta');meta.append(el('span',item.rfcs.map(r=>r.startsWith('research/')?'研究 RFC '+r.slice(9):'RFC '+r).join(' · ')));
    const badge=el('span',catalog.statusLabels[item.status],'status-badge');badge.dataset.status=item.status;meta.append(badge);
    const heading=el('h3');const anchor=el('a',item.title);anchor.href='#'+article.id;heading.append(anchor);
    article.append(meta,heading,el('p',item.summary),el('p',supportLabels[item.support],'feature-scope'),el('p','边界：'+item.limitation,'feature-limit'));
    if(item.development)article.append(el('p','开发快照：'+item.development,'feature-dev'));
    const usage=details('典型用法与例子');usage.append(el('p',item.usage));
    if(item.snippet){usage.append(el('p',item.snippet.language,'sample-title'),code(item.snippet.text),el('p',item.snippet.note));}
    for(const id of item.examples) {
      const example=manifest.examples.find(e=>e.id===id);if(!example)throw new Error('目录示例缺失');
      usage.append(el('p',example.title,'sample-title'));
      const link=el('a','打开 Playground →','example-open');link.href='#playground';link.dataset.example=id;
      link.onclick=()=>{choose(manifest.examples.findIndex(e=>e.id===id));};usage.append(link);
      usage.append(details('查看完整 goxide',example.source),details('查看真实 CLI 生成 Go',example.go));
      const output=details('查看 CLI 运行输出（浏览器不执行）',example.stdout);usage.append(output);
    }
    if(item.id==='reexports'){const a=el('a','查看 use / pub use 多文件快照 →','snapshot-link');a.href='#projects';usage.append(a);}
    if(['projects','import-go','vendor','cli-options','workspace','workspace-import'].includes(item.id)){const a=el('a','查看 CLI 工程快照与验证输出 →','snapshot-link');a.href='#project-snapshots';usage.append(a);}
    for(const id of item.checks){const check=catalog.checks.find(c=>c.id===id);const d=details('拒绝例：'+id,check.source);d.append(el('p','当前版本真实 CLI 诊断'),code(check.diagnostic));usage.append(d);}
    article.append(usage);$('catalog-items').append(article);
  }
  function filter(){
    const query=$('catalog-search').value.trim().toLowerCase();let count=0;
    for(const item of catalog.items){const visible=(topic==='all'||item.topic===topic)&&($('catalog-status').value==='all'||item.status===$('catalog-status').value)&&($('catalog-support').value==='all'||item.support===$('catalog-support').value)&&[item.title,item.summary,item.usage,...item.rfcs].join(' ').toLowerCase().includes(query);$('feature-'+item.id).hidden=!visible;if(visible)count++;}
    for(const b of $('catalog-topics').children)b.setAttribute('aria-pressed',String(b.dataset.topic===topic));
    $('catalog-count').textContent=`显示 ${count} / ${catalog.items.length} 个主题 · 状态对应线上版本 ${manifest.commit.slice(0,7)}`;$('catalog-empty').hidden=count!==0;
  }
  function reset(){topic='all';$('catalog-search').value='';$('catalog-status').value='all';$('catalog-support').value='all';filter();}
  $('catalog-search').oninput=filter;$('catalog-status').onchange=filter;$('catalog-support').onchange=filter;$('catalog-clear').onclick=reset;
  function followHash(){const target=document.getElementById(location.hash.slice(1));if(target?.classList.contains('feature-card')){reset();target.scrollIntoView({block:'start'});}}
  addEventListener('hashchange',followHash);filter();followHash();
  for(const project of manifest.projectExamples||[]){const d=details(project.title);if(project.note)d.append(el('p',project.note));for(const file of project.files)d.append(details(file.path,file.source));for(const run of project.runs)d.append(el('p',run.command,'mono'),code(run.stdout));$('project-examples').append(d);}
}

(async () => {
  const response = await fetch('examples.json', {cache:'no-store'});
  if (!response.ok) throw new Error(`示例清单 HTTP ${response.status}`);
  manifest = await response.json();
  examples = [...manifest.examples, ...(manifest.packageExamples || [])];
  if (!/^[a-f0-9]{64}$/.test(manifest.wasmSha256 || '')) throw new Error('编译器清单缺少完整性摘要，请刷新重试');
  renderCatalog(); renderDependencies();
  $('examples').replaceChildren(...examples.map((e,i) => { const option = document.createElement('option'); option.value = i; option.textContent = e.title; return option; }));
  $('version').textContent = `源码提交 ${manifest.commit} · 编译器源摘要 SHA-256 ${manifest.compilerDigest} · ${manifest.toolchain}`;
  $('packages').textContent = manifest.packages.join(' · ');
  $('library-versions').textContent = manifest.publicLibraries.map(p=>`${p.module} @ ${p.version}`).join(' · ');
  for (const example of manifest.cliExamples || []) {
    for (const file of example.files) {
      const detail = document.createElement('details');
      const summary = document.createElement('summary'); summary.textContent = file.path;
      detail.append(summary);
      for (const [label, value] of [['goxide 源码', file.source], ['CLI 生成 Go', file.go]]) {
        const title = document.createElement('p'); title.textContent = label;
        const pre = document.createElement('pre'); pre.tabIndex = 0; pre.textContent = value;
        detail.append(title, pre);
      }
      $('cli-example').append(detail);
    }
    const pre = document.createElement('pre'); pre.textContent = 'CLI 运行输出\n' + example.stdout;
    $('cli-example').append(pre);
  }
  for (const id of ['examples','source','compile','reset','new-file-name','add-file']) $(id).disabled = false;
  choose(0);
  if (document.modelContext?.registerTool) {
    const lifecycle = new AbortController();
    addEventListener('pagehide', () => lifecycle.abort(), {once:true});
    Promise.resolve(document.modelContext.registerTool({name:'read_playground',description:'Read the current goxide source, generated Go and compiler status without executing code.', inputSchema:{type:'object',properties:{},additionalProperties:false}, annotations:{readOnlyHint:true,untrustedContentHint:true}, execute(input) { if (!input || typeof input !== 'object' || Object.keys(input).length) throw new Error('expected empty object'); return {mode:packageMode?'package':'single',files:files.map(file=>({...file})),generatedFiles:generatedFiles.map(file=>({...file})),source:$('source').value,go:$('output').value,status:$('status').textContent,diagnostic:$('diagnostic').hidden ? '' : $('diagnostic').textContent}; }}, {signal:lifecycle.signal})).catch(() => {});
  }
})().catch(e => { $('catalog-count').textContent = '特性目录暂时不可用，请刷新重试。'; fail(String(e)); });
