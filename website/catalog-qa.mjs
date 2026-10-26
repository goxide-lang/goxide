import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {write,read,view,done} from './monaco-qa.mjs';
import {recordWorkerObservations} from './validation/worker-observations.mjs';
const documentEnd=process.platform==='darwin'?'Meta+ArrowDown':'Control+End';
const base=process.env.QA_URL||'http://127.0.0.1:8084';
const layoutNames={source:'仅源码',result:'仅结果',horizontal:'左右并排',vertical:'上下排列'};
async function selectLayout(page,layout){
 const trigger=page.getByRole('button',{name:'编辑器布局',exact:true});
 const menu=page.getByRole('group',{name:'选择编辑器布局',exact:true});
 await trigger.click();await menu.waitFor({state:'visible'});
 assert.equal(await trigger.getAttribute('aria-expanded'),'true');
 await menu.getByRole('button',{name:layoutNames[layout],exact:true}).click();
 await menu.waitFor({state:'hidden'});assert.equal(await trigger.getAttribute('aria-expanded'),'false');
 assert.equal(await page.locator('.dual-editor').getAttribute('data-layout'),layout);
}
async function workspaceAction(page,name){
 const trigger=page.getByRole('button',{name:'更多操作',exact:true});
 const menu=page.getByRole('group',{name:'工作区操作',exact:true});
 await trigger.click();await menu.waitFor({state:'visible'});
 await menu.getByRole('button',{name,exact:true}).click();
 await menu.waitFor({state:'hidden'});assert.equal(await trigger.getAttribute('aria-expanded'),'false');
}
const browser=await chromium.launch({executablePath:process.env.CHROMIUM||'/usr/bin/chromium',headless:true,chromiumSandbox:true});
try{
 const context=await browser.newContext({viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write'],reducedMotion:'reduce'});
 const page=await context.newPage(),errors=[],requests=[];
 async function nav(name){await page.getByRole('link',{name,exact:true}).first().click();const path={'RFC 特性':'/rfc.html','Playground':'/playground.html','本地使用':'/local.html'}[name];await page.waitForFunction(path=>location.pathname===path,path);}
page.on('pageerror',error=>errors.push(error.message));context.on('request',request=>requests.push(request));
 await page.goto(base+'/');await page.getByRole('heading',{name:/用 Goxide 表达业务/}).waitFor();
 assert.ok(!requests.some(request=>/MonacoPane|editor.worker|compiler.wasm/.test(request.url())),'home does not load editors or compiler');
 await page.evaluate(()=>window.qaDocumentMarker='same document');
 await nav('Playground');await done(page);
 const manifest=await page.evaluate(async()=>await(await fetch('/examples.json')).json());
 assert.equal(manifest.catalog.compilerCommit,manifest.catalogCompilerCommit);
 assert.equal(manifest.compilerSource.digest,manifest.compilerDigest);
 assert.equal(manifest.compilerSource.kind==='worktree'?manifest.compilerSource.baseCommit:manifest.compilerSource.commit,manifest.commit);
 await page.selectOption('#examples','package_files');await done(page);
 const pkg=manifest.packageExamples[0];
 for(const file of pkg.generatedFiles){await view(page,'生成结果').locator(`.file-tabs [data-path="${file.path}"]`).click();assert.equal(await read(page,'生成结果'),file.source,file.path+' Worker/CLI parity');}
 const file=pkg.files[0], source=view(page,'源码');await source.locator(`.file-tabs [data-path="${file.path}"]`).click();
 const edited=file.source+'\n// unsaved route draft\n';await write(page,edited);await done(page);assert.equal(await read(page,'源码'),edited);
 await source.getByRole('textbox').focus();await page.keyboard.press(documentEnd);await page.keyboard.press('x');
 await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));await source.getByRole('textbox').focus();await page.keyboard.press('ControlOrMeta+Z');await done(page);assert.equal(await read(page,'源码'),edited,'Monaco undo');
 await source.locator(`.file-tabs [data-path="${pkg.files[1].path}"]`).click();await source.locator(`.file-tabs [data-path="${file.path}"]`).click();assert.equal(await read(page,'源码'),edited);
 await source.getByRole('textbox').focus();await page.keyboard.press(documentEnd);await page.keyboard.press('Shift+ArrowLeft');
 await source.locator(`.file-tabs [data-path="${pkg.files[1].path}"]`).click();await source.locator(`.file-tabs [data-path="${file.path}"]`).click();await source.getByRole('textbox').focus();await page.keyboard.press('x');await done(page);assert.equal(await read(page,'源码'),edited.slice(0,-1)+'x','selection restored across models');await source.getByRole('textbox').focus();await page.keyboard.press('ControlOrMeta+Z');await done(page);assert.equal(await read(page,'源码'),edited);
 await nav('RFC 特性');await page.locator('#rfc-list a[href="/rfc.html?feature=traits"]').click();
 await page.goBack();await page.goBack();assert.equal(await read(page,'源码'),edited);assert.equal(await page.evaluate(()=>window.qaDocumentMarker),'same document');
 await page.goForward();await page.goForward();assert.match(page.url(),/feature=traits$/);await nav('Playground');assert.equal(await read(page,'源码'),edited);
 // Creation is local to the source explorer; output selection remains independent.
 async function createFile(name) {
  if (!await page.locator('#new-file-name').isVisible()) await source.getByRole('button',{name:'新建文件',exact:true}).click();
  await page.fill('#new-file-name',name);await source.getByRole('button',{name:'创建',exact:true}).click();
 }
 await createFile('../escape.gox');assert.match(await page.textContent('#file-error'),/普通/);
 await createFile('extra.gox');await done(page);await write(page,'package main\nconst Extra = 9\n');await done(page);
 await createFile('extra.gox');assert.match(await page.textContent('#file-error'),/已存在/);
 await createFile('second.gox');await done(page);assert.equal(await source.locator('.file-tabs [data-path]').count(),pkg.files.length+2);
 await source.getByRole('button',{name:'删除文件',exact:true}).click();await done(page);assert.equal(await read(page,'源码'),'package main\nconst Extra = 9\n');
 for(const layout of ['source','result','horizontal','vertical']){await selectLayout(page,layout);assert.equal(await page.locator('.source-panel').isVisible(),layout!=='result');assert.equal(await page.locator('.result-panel').isVisible(),layout!=='source');}
 for(const [name,group] of [['编辑器布局','选择编辑器布局'],['更多操作','工作区操作']]){
  const trigger=page.getByRole('button',{name,exact:true}),menu=page.getByRole('group',{name:group,exact:true});
  await trigger.click();await menu.waitFor({state:'visible'});await page.keyboard.press('Escape');
  await menu.waitFor({state:'hidden'});assert.equal(await trigger.getAttribute('aria-expanded'),'false');
  assert.equal(await page.locator('.dual-editor').getAttribute('data-layout'),'vertical','Escape preserves layout');
 }
 await source.getByRole('button',{name:'收起文件树',exact:true}).click();assert.equal(await source.locator('.file-tree').isVisible(),false);await source.getByRole('button',{name:'展开文件树',exact:true}).click();
 const output=view(page,'生成结果'),readonlyOutput=await read(page,'生成结果');
 assert.equal(await output.locator('.new-file').count(),0);await output.getByRole('textbox').focus();await page.keyboard.press('ControlOrMeta+A');await page.keyboard.insertText('should not write');assert.equal(await read(page,'生成结果'),readonlyOutput,'readonly output');
 await source.locator('.file-tree summary').click();
 await source.locator(`.file-tree [data-path="${manifest.dependencyContext.path}"]`).click();
 assert.equal(await read(page,'源码'),manifest.dependencyContext.source);
 await source.getByRole('textbox').focus();await page.keyboard.press('ControlOrMeta+A');await page.keyboard.insertText('should not write');assert.equal(await read(page,'源码'),manifest.dependencyContext.source,'readonly dependency');
 assert.match(await source.locator('.file-provenance').textContent(),/SHA-256/);
 await source.locator(`.file-tabs [data-path="${file.path}"]`).click();
 await selectLayout(page,'source');await write(page,'package main\nfunc broken( {');await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
 assert.ok(await source.locator('.squiggly-error').count()>0,'compiler location markers');await page.locator('.diagnostic-links button').first().click();assert.equal(await source.getByRole('textbox').evaluate(element=>document.activeElement===element),true,'diagnostic focuses source');await selectLayout(page,'result');assert.equal(await read(page,'生成结果'),'');
 const oldEditor=await source.locator('.monaco-editor').elementHandle();
 await selectLayout(page,'horizontal');await workspaceAction(page,'重置');await done(page);
 await source.getByRole('textbox').waitFor({state:'attached'});
 assert.equal(await oldEditor.evaluate(element=>element.isConnected),false,'reset removes old Monaco editor');
 assert.equal(await source.locator('.monaco-editor').count(),1,'one recreated source editor');
 assert.equal(await source.locator('.squiggly-error').count(),0);
 await write(page,pkg.files[0].source+'\n// stop test\n');await workspaceAction(page,'中断');assert.equal(await read(page,'生成结果'),'');await page.click('#compile');await done(page);
 // Recompilation keeps an existing generated path selected, including helper files.
 const resultPath=pkg.generatedFiles.at(-1).path;await view(page,'生成结果').locator(`.file-tabs [data-path="${resultPath}"]`).click();await write(page,pkg.files[0].source+'\n// path retained\n');await done(page);assert.equal(await view(page,'生成结果').locator('.file-tabs [aria-pressed=true]').getAttribute('data-path'),resultPath);
 await nav('RFC 特性');assert.equal(await page.locator('#rfc-list a').count(),manifest.catalog.items.length);
 for(const item of manifest.catalog.items){await page.locator(`#rfc-list a[href="/rfc.html?feature=${item.id}"]`).click();await page.waitForFunction(title=>document.querySelector('#rfc-content h2')?.textContent===title,item.title);const text=await page.textContent('#rfc-content');assert.ok(text.includes(item.limitation));assert.ok(text.includes(manifest.catalog.statusLabels[item.status]));assert.equal(await page.locator('#rfc-content details').count(),0);}
 for(const example of manifest.examples){const item=manifest.catalog.items.find(item=>item.examples.includes(example.id));await page.locator(`#rfc-list a[href="/rfc.html?feature=${item.id}"]`).click();await page.waitForFunction(title=>document.querySelector('#rfc-content h2')?.textContent===title,item.title);await page.locator(`#rfc-content a[href="/playground.html?example=${example.id}"]`).click();await page.waitForFunction(id=>document.querySelector('#examples')?.value===id,example.id);await done(page);assert.equal(await read(page,'生成结果'),example.go,example.id+' parity');await nav('RFC 特性');}
 // New consumer types must create request-local dependency instances too.
 await nav('Playground');
 for(const check of manifest.editingChecks){
  await page.selectOption('#examples',check.example);await done(page);
  await write(page,check.source);await done(page);assert.equal(await read(page,'生成结果'),check.go,check.id+' edited Worker/CLI parity');
  await workspaceAction(page,'重置');await done(page);assert.equal(await read(page,'生成结果'),manifest.examples.find(example=>example.id===check.example).go,check.id+' edit recovery');
 }
 // Every published rejection comes from the same native consumer graph.
 await nav('Playground');await page.selectOption('#examples',manifest.examples[0].id);await done(page);
 for(const check of manifest.catalog.checks){
  await write(page,check.source);await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
  assert.equal((await page.textContent('#diagnostic')).trim(),check.diagnostic,check.id+' diagnostic parity');
  assert.equal(await read(page,'生成结果'),'');assert.equal(await view(page,'生成结果').getByRole('button',{name:'复制文件',exact:true}).isDisabled(),true);
  await workspaceAction(page,'重置');await done(page);assert.equal(await read(page,'生成结果'),manifest.examples[0].go,check.id+' recovery');
 }
 // Reuse one real compiler Worker and filename across unrelated diagnostics.
 const nestedSource='package main\nmacro_rules! inner { () => { missing }; }\nmacro_rules! outer { () => { inner!() }; }\nfunc main() { _ = outer!() }\n';
 const ordinarySource='package main\n// 日本語\nfunc main() {\n    _ = unrelated\n}\n';
 const nestedError='playground.gox:2:30: unresolved name\n  macro outer defined at playground.gox:3:1; called at playground.gox:4:19\n  macro inner defined at playground.gox:2:1; called at playground.gox:3:30';
 const rawInputs=[];
 for(const ending of ['LF','CRLF'])for(const entry of [
  {source:nestedSource,error:nestedError},
  {source:ordinarySource,error:'playground.gox:4:9: unresolved name'},
  {source:manifest.examples[0].source,go:manifest.examples[0].go},
 ])rawInputs.push({...entry,ending,source:ending==='CRLF'?entry.source.replaceAll('\n','\r\n'):entry.source});
 await fs.mkdir(new URL('qa-artifacts/',import.meta.url),{recursive:true});
 const rawMacroHostURL=base+'/__qa/proc-macro-host.js';
 const rawMacroHostSource=await fs.readFile(new URL('ui/proc-macro-host.js',import.meta.url),'utf8');
 await page.route(rawMacroHostURL,route=>route.fulfill({contentType:'text/javascript',body:rawMacroHostSource}));
 const rawResults=await recordWorkerObservations((collect,args)=>page.evaluate(collect,args),
  {inputs:rawInputs,commit:manifest.commit,hash:manifest.wasmSha256,rootsHash:manifest.dependencyContext.sha256,macroHostURL:rawMacroHostURL,timeoutMs:70000},
  batch=>fs.writeFile(new URL('qa-artifacts/diagnostic-worker-observations.json',import.meta.url),JSON.stringify(batch,null,2)+'\n'));
 await page.unroute(rawMacroHostURL);
 for(let index=0;index<rawInputs.length;index++){
  const expected=rawInputs[index],actual=rawResults[index];
  if(expected.error){assert.equal(actual.error?.trim(),expected.error,expected.ending+' diagnostic provenance');assert.equal(actual.go||'','');}
  else{assert.equal(actual.error,undefined);assert.equal(actual.go,expected.go,expected.ending+' recovery');}
 }
 // Exercise the real fixed guest, then recover from its structured diagnostic.
 const proc=manifest.examples.find(example=>example.id==='proc_function');
 const guest=manifest.macroBundle.artifacts.find(artifact=>artifact.module==='example.org/website-macros');
 assert.deepEqual(guest.entries.map(entry=>entry.name).sort(),['TypeName','constant','twice']);
 assert.ok(requests.some(request=>new URL(request.url()).pathname==='/'+guest.path),'real bundled macro WASM requested');
 assert.ok(requests.some(request=>new URL(request.url()).pathname==='/proc-macro-worker.js'),'real dedicated macro Worker requested');
 await nav('Playground');await page.selectOption('#examples','proc_function');await done(page);
 assert.deepEqual(proc.macroSources.map(file=>file.path).sort(),['macros/expand.go','macros/goxide.toml','macros/lib.gox']);
 const projectDownload=page.getByRole('link',{name:'下载完整项目',exact:true});
 assert.equal(await projectDownload.getAttribute('href'),'/'+proc.macroDownload.path);
 assert.equal((await context.request.get(base+'/'+proc.macroDownload.path)).status(),200);
 await page.getByRole('button',{name:'查看宏实现',exact:true}).click();
 assert.equal(await read(page,'源码'),proc.macroSources.find(file=>file.path===proc.macroEntryPath).source);
 await source.getByRole('textbox').focus();await page.keyboard.press('ControlOrMeta+A');await page.keyboard.insertText('should not write');assert.equal(await read(page,'源码'),proc.macroSources.find(file=>file.path===proc.macroEntryPath).source,'readonly macro registration');
 await source.locator('.file-tree [data-path="macros/expand.go"]').click();assert.equal(await read(page,'源码'),proc.macroSources.find(file=>file.path==='macros/expand.go').source,'complete helper implementation');
 assert.equal(await page.getByRole('link',{name:'学习编写过程宏',exact:true}).getAttribute('href'),proc.macroDocumentation);
 await source.locator('.file-tabs [data-path="playground.gox"]').click();
 await write(page,proc.source.replace('return 21','return 25'));await done(page);
 assert.match(await read(page,'生成结果'),/25/,'edited input reaches real procedural expansion');
 assert.match(await read(page,'生成结果'),/\*\s*2/,'function macro emitted multiplication');
 await write(page,proc.source.replace('macros::twice!(next())','macros::twice!()'));
 await page.waitForFunction(()=>document.querySelector('#diagnostic').textContent.includes('twice requires an expression'));
 assert.equal(await read(page,'生成结果'),'','macro diagnostic clears previous result');
 await workspaceAction(page,'重置');await done(page);assert.equal(await read(page,'生成结果'),proc.go,'recovery after macro diagnostic');
 await nav('Playground');await page.selectOption('#examples',manifest.examples[0].id);await done(page);
 for(const value of [80,81,82,83,84,85])await write(page,manifest.examples[0].source.replace('21',String(value)));await done(page);assert.match(await read(page,'生成结果'),/85/,'latest rapid edit');
 await write(page,'package main\nfunc main() {}\n'+' '.repeat(32769));await page.waitForFunction(()=>document.querySelector('#diagnostic').textContent.includes('32 KiB'));assert.equal(await read(page,'生成结果'),'');await workspaceAction(page,'重置');await done(page);
 await write(page,'package main\n// <img src=x onerror=alert(1)>\nfunc main() {}\n');await done(page);assert.equal(await page.locator('img').count(),0,'source is rendered as text');
 await source.getByRole('textbox').focus();await page.keyboard.press('ControlOrMeta+Enter');await done(page);await page.keyboard.press('Tab');assert.ok(await page.evaluate(()=>document.activeElement!==document.body));
 await write(page,'/* package decoy */\npackage/**/示例\nfunc F() {}\n');await done(page);
 await createFile('unicode.gox');await done(page);assert.equal(await read(page,'源码'),'package 示例\n','new file follows Unicode package clause');
 await workspaceAction(page,'重置');await done(page);
 await nav('本地使用');await page.locator('#project-examples .file-view').first().waitFor();assert.ok(await page.locator('#project-examples .file-view').count()>0);assert.equal(await page.locator('#project-examples .new-file').count(),0);
 for(const project of manifest.projectExamples.filter(project=>project.download)){const link=page.locator(`#project-examples a[href="/${project.download.path}"]`);assert.equal(await link.getAttribute('download'),project.download.path);const response=await context.request.get(base+'/'+project.download.path);assert.equal(response.status(),200);const bytes=await response.body();assert.equal(bytes.subarray(0,2).toString(),'PK');assert.ok(project.download.files.includes('go.mod'));assert.ok(project.download.files.includes('go.sum'));assert.ok(project.download.files.includes('goxide.roots.toml'));assert.ok(project.download.files.includes('impl/impl.go'));assert.ok(project.download.files.some(path=>path.endsWith('.gox')));}
 assert.equal(await page.locator('#project-examples a[download]').count(),1);
 for(const route of ['/learn/?stage=gox','/docs/?page=manifest','/design/?page=grammar','/rfc.html?feature=traits','/playground.html?example=macro','/local.html','/']){assert.equal((await page.goto(base+route)).status(),200);await page.getByRole('navigation',{name:'主导航'}).waitFor();await page.reload();await page.getByRole('navigation',{name:'主导航'}).waitFor();}
 await page.goto(base+'/rfc.html?feature=missing');await page.getByRole('alert').filter({hasText:'找不到此 RFC'}).waitFor();
 await page.goto(base+'/playground.html?example=missing');await page.getByRole('alert').filter({hasText:'找不到此示例'}).waitFor();
 await page.goto(base+'/playground.html?example=package_files');await done(page);
 await fs.mkdir(new URL('qa-artifacts/',import.meta.url),{recursive:true});await page.screenshot({path:new URL('qa-artifacts/preact-monaco-desktop.png',import.meta.url).pathname});
 await page.setViewportSize({width:390,height:844});
 await source.locator('.file-tree').waitFor({state:'hidden'});await view(page,'生成结果').locator('.file-tree').waitFor({state:'hidden'});
 const narrowFileCount=await source.locator('.file-tabs [data-path]').count();
 await source.getByRole('button',{name:'新建文件',exact:true}).click();await page.locator('#new-file-name').waitFor({state:'visible'});
 assert.equal(await source.locator('.file-tree').isVisible(),true,'narrow create opens source explorer');
 await page.fill('#new-file-name','mobile.gox');await source.getByRole('button',{name:'取消',exact:true}).click();
 await page.locator('#new-file-name').waitFor({state:'hidden'});assert.equal(await source.locator('.file-tabs [data-path]').count(),narrowFileCount,'cancel does not create a source file');
 await source.getByRole('button',{name:'收起文件树',exact:true}).click();await source.locator('.file-tree').waitFor({state:'hidden'});
 for(const layout of ['source','result','horizontal','vertical']){await selectLayout(page,layout);assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),layout+' reading width');}
 await selectLayout(page,'horizontal');await page.screenshot({path:new URL('qa-artifacts/preact-monaco-narrow.png',import.meta.url).pathname});
 await page.setViewportSize({width:1440,height:1000});assert.equal(await page.locator('.dual-editor').evaluate(element=>getComputedStyle(element).gridTemplateColumns.split(' ').length),2);
 await source.getByRole('textbox').focus();await page.keyboard.press('ControlOrMeta+f');await source.locator('.find-widget.visible').waitFor();await page.keyboard.press('Escape');await source.getByRole('textbox').focus();await page.keyboard.press('Control+Space');await page.waitForFunction(()=>document.querySelector('.suggest-widget.visible'),null,{timeout:10000});await page.keyboard.press('Escape');
 assert.deepEqual(errors,[]);assert.ok(requests.every(request=>request.url().startsWith(base)&&request.method()==='GET'),'same-origin GET only');assert.ok(requests.some(request=>/editor.worker-.*\.js/.test(request.url())),'bundled Monaco worker');assert.ok(!requests.some(request=>request.url().includes('/goxide/assets/')),'organization root base');
 const unavailable=await context.newPage();await unavailable.route('**/examples.json',route=>route.fulfill({status:503,body:'unavailable'}));await unavailable.goto(base+'/');await unavailable.getByRole('alert').filter({hasText:'503'}).waitFor();await unavailable.close();
 const stale=await context.newPage();await stale.route('**/compiler.wasm*',route=>route.fulfill({contentType:'application/wasm',body:Buffer.from([0,97,115,109,1,0,0,0])}));await stale.goto(base+'/playground.html');await stale.waitForFunction(()=>document.querySelector('#diagnostic')?.textContent.includes('完整性不匹配'));assert.equal(await read(stale,'生成结果'),'');await stale.close();
 const tampered=await context.newPage();
 await tampered.route('**/proc-macros/*.wasm*',route=>route.fulfill({contentType:'application/wasm',body:Buffer.from([0,97,115,109,1,0,0,0])}));
 await tampered.goto(base+'/playground.html?example=proc_function');
 await tampered.waitForFunction(()=>document.querySelector('#diagnostic')?.textContent.includes('procedural macro artifact integrity mismatch'));
 assert.equal(await read(tampered,'生成结果'),'','untrusted guest bytes never produce Go');
 await tampered.unroute('**/proc-macros/*.wasm*');await tampered.click('#compile');await done(tampered);
 assert.equal(await read(tampered,'生成结果'),proc.go,'recovery after rejected macro bytes');await tampered.close();
 // Keep JavaScript rejection reasons when the Go host awaits the Promise.
 const workerSource=await(await context.request.get(base+'/worker.js')).text();
 for(const rejection of [
  {expression:'new Error("QA Error rejection")',message:'Error: QA Error rejection'},
  {expression:'"QA string rejection"',message:'QA string rejection'},
  {expression:'null',message:'null'},
 ]){
  const rejected=await context.newPage();
  await rejected.route('**/worker.js*',route=>route.fulfill({contentType:'application/javascript',body:workerSource+'\nself.goxideRunProcMacro=()=>Promise.reject('+rejection.expression+');\n'}));
  await rejected.goto(base+'/playground.html?example=proc_function');
  await rejected.waitForFunction(()=>document.querySelector('#status')?.dataset.state==='error');
  const diagnostic=await rejected.textContent('#diagnostic');
  assert.ok(diagnostic.includes('procedural macro execution: '+rejection.message),'preserved JavaScript rejection '+rejection.message);
  assert.match(diagnostic,/^[^\n]+\.gox:\d+:\d+:/,'host rejection source location');
  assert.match(diagnostic,/\n  macro defined at [^\n]+; called at [^\n]+/,'host rejection expansion trace');
  assert.equal(await read(rejected,'生成结果'),'','rejected host Promise clears output');
  await rejected.unroute('**/worker.js*');await workspaceAction(rejected,'中断');await rejected.click('#compile');await done(rejected);
  assert.equal(await read(rejected,'生成结果'),proc.go,'recovery after rejected host Promise');await rejected.close();
 }
 const interrupted=await context.newPage();
 let releaseFetch,seenFetch;const blocked=new Promise(resolve=>{releaseFetch=resolve}),requested=new Promise(resolve=>{seenFetch=resolve});
 await interrupted.route('**/proc-macros/*.wasm*',async route=>{seenFetch();await blocked;await route.continue().catch(()=>{});});
 await interrupted.goto(base+'/playground.html?example=proc_function');await requested;
 await workspaceAction(interrupted,'中断');assert.equal(await read(interrupted,'生成结果'),'','stopping a macro clears output');
 releaseFetch();await interrupted.unroute('**/proc-macros/*.wasm*');await interrupted.click('#compile');await done(interrupted);
 assert.equal(await read(interrupted,'生成结果'),proc.go,'fresh Worker recovers after macro cancellation');await interrupted.close();
 const editorFailure=await context.newPage();const loadErrors=[];editorFailure.on('pageerror',error=>loadErrors.push(error.message));
 await editorFailure.route('**/assets/MonacoPane-*.js',route=>route.fulfill({status:503,contentType:'text/javascript',body:'unavailable'}));
 await editorFailure.goto(base+'/playground.html');
 await view(editorFailure,'源码').getByRole('alert').filter({hasText:'编辑器加载失败'}).waitFor();
 assert.equal(await view(editorFailure,'源码').getByRole('textbox').count(),0);assert.deepEqual(loadErrors,[]);await editorFailure.close();
 for(const consistentHash of [false,true]){
  const mismatch=await context.newPage();
  await mismatch.route('**/examples.json',async route=>{
   const changed=structuredClone(manifest);changed.dependencyContext.source+='\n# altered roots\n';
   if(consistentHash){const {createHash}=await import('node:crypto');changed.dependencyContext.sha256=createHash('sha256').update(changed.dependencyContext.source).digest('hex');}
   await route.fulfill({contentType:'application/json',body:JSON.stringify(changed)});
  });
  await mismatch.goto(base+'/playground.html');
  if(consistentHash){await mismatch.waitForFunction(()=>document.querySelector('#diagnostic')?.textContent.includes('项目依赖根与编译器不匹配'));assert.equal(await read(mismatch,'生成结果'),'');}
  else await mismatch.getByRole('alert').filter({hasText:'清单版本或项目依赖根不匹配'}).waitFor();
  await mismatch.close();
 }
 const timeout=await context.newPage();
 await timeout.route('**/examples.json',route=>route.fulfill({contentType:'application/json',body:JSON.stringify({...manifest,compileTimeoutMs:1000})}));
 await timeout.route('**/worker.js*',route=>route.fulfill({contentType:'application/javascript',body:`postMessage({type:'ready',rootsSha256:'${manifest.dependencyContext.sha256}',macroArtifacts:[]});onmessage=()=>{};`}));
 await timeout.goto(base+'/playground.html');await timeout.waitForFunction(()=>document.querySelector('#diagnostic')?.textContent.includes('编译超过 1 秒'));
 assert.equal(await read(timeout,'生成结果'),'');await timeout.click('#compile');await timeout.waitForFunction(()=>document.querySelector('#status')?.textContent==='正在转译…');await workspaceAction(timeout,'中断');assert.match(await timeout.textContent('#status'),/已中断/);
 await timeout.unroute('**/worker.js*');await timeout.unroute('**/examples.json');await timeout.reload();await done(timeout);assert.equal(await read(timeout,'生成结果'),manifest.examples[0].go);await timeout.close();
 const delayed=await context.newPage();
 await delayed.addInitScript(()=>{
  const NativeWorker=window.Worker,state=window.qaHeldResults={hold:false,results:[],history:[]};
  window.Worker=class {
   constructor(...args){
    if(new URL(args[0],location.href).pathname!=='/worker.js')return new NativeWorker(...args);
    this.actual=new NativeWorker(...args);
    this.actual.onmessage=event=>{const deliver=()=>this.onmessage?.(event);if(event.data.type==='result')state.history.push({data:event.data,deliver});if(event.data.type==='result'&&state.hold)state.results.push({data:event.data,deliver});else deliver();};
    this.actual.onerror=event=>this.onerror?.(event);
   }
   postMessage(...args){this.actual.postMessage(...args);}
   terminate(){this.actual.terminate();}
  };
 });
 await delayed.goto(base+'/playground.html');await done(delayed);
 await delayed.evaluate(()=>window.qaHeldResults.hold=true);await write(delayed,manifest.examples[0].source.replace('21','111'));
 await delayed.waitForFunction(()=>window.qaHeldResults.results.length===1,null,{timeout:70000});
 await delayed.evaluate(()=>window.qaHeldResults.history[0].deliver());assert.equal(await read(delayed,'生成结果'),'','old request ID cannot populate a busy compilation');assert.equal(await delayed.textContent('#status'),'正在转译…');
 await delayed.evaluate(()=>{window.qaHeldResults.hold=false;window.qaHeldResults.results.shift().deliver();});await done(delayed);assert.match(await read(delayed,'生成结果'),/111/);
 await delayed.evaluate(()=>window.qaHeldResults.hold=true);await write(delayed,manifest.examples[0].source.replace('21','222'));
 await delayed.waitForFunction(()=>window.qaHeldResults.results.length===1,null,{timeout:70000});
 await delayed.selectOption('#examples','package_files');await delayed.waitForFunction(()=>window.qaHeldResults.results.length===2,null,{timeout:70000});
 await delayed.evaluate(()=>window.qaHeldResults.results.shift().deliver());assert.equal(await read(delayed,'生成结果'),'','obsolete Worker cannot populate a busy package compilation');assert.equal(await delayed.textContent('#status'),'正在转译…');
 await delayed.evaluate(()=>{window.qaHeldResults.hold=false;window.qaHeldResults.results.shift().deliver();});await done(delayed);
 await view(delayed,'生成结果').locator(`.file-tabs [data-path="${pkg.generatedFiles[0].path}"]`).click();assert.equal(await read(delayed,'生成结果'),pkg.generatedFiles[0].source,'package result remains native CLI output');await delayed.close();
 console.log(JSON.stringify({url:base,compilerCommit:manifest.commit,websiteCommit:manifest.websiteCommit,wasmSha256:manifest.wasmSha256,examples:manifest.examples.length,packageGeneratedFiles:pkg.generatedFiles.length,rfcItems:manifest.catalog.items.length,diagnosticParity:manifest.catalog.checks.length,editedTypeParity:manifest.editingChecks.length,diagnosticWorkerLFCRLF:rawResults.length,proceduralMacros:{function:true,attribute:true,derive:true,realGuest:true,diagnosticRecovery:true,integrityRecovery:true,cancelRecovery:true,promiseRejectionValues:3},hardTimeoutRecovery:true,delayedRealWorker:true,dependencyRootMismatch:true,unicodePackage:true,sourceEscaping:true,preact:true,monaco:true,modelsUndo:true,editorRecreated:true,lazyFailure:true,readonly:true,layouts:4,clientHistoryDraft:true,deepLinksReload:true,lazyEditor:true,rootSiteAssets:true,narrowReading:true,errors},null,2));
}finally{await browser.close();}
