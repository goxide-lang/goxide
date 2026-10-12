import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {write,read,view,done} from './monaco-qa.mjs';
const base=process.env.QA_URL||'http://127.0.0.1:8084';
const browser=await chromium.launch({executablePath:process.env.CHROMIUM||'/usr/bin/chromium',headless:true,chromiumSandbox:true});
try{
 const context=await browser.newContext({viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write'],reducedMotion:'reduce'});
 const page=await context.newPage(),errors=[],requests=[];
 async function nav(name){await page.getByRole('link',{name,exact:true}).click();const path={'RFC 特性':'/rfc.html','Playground':'/playground.html','本地使用':'/local.html'}[name];await page.waitForFunction(path=>document.querySelector('header nav a[aria-current="page"]')?.getAttribute('href')===path,path);}
page.on('pageerror',error=>errors.push(error.message));context.on('request',request=>requests.push(request));
 await page.goto(base+'/');await page.getByRole('heading',{name:/写得更明确/}).waitFor();
 assert.ok(!requests.some(request=>/MonacoPane|editor.worker|compiler.wasm/.test(request.url())),'home does not load editors or compiler');
 await page.evaluate(()=>window.qaDocumentMarker='same document');
 await nav('Playground');await done(page);
 const manifest=await page.evaluate(async()=>await(await fetch('/examples.json')).json());
 assert.equal(manifest.catalog.compilerCommit,manifest.commit);
 await page.selectOption('#examples','package_files');await done(page);
 const pkg=manifest.packageExamples[0];
 for(const file of pkg.generatedFiles){await view(page,'生成结果').locator(`.file-tabs [data-path="${file.path}"]`).click();assert.equal(await read(page,'生成结果'),file.source,file.path+' Worker/CLI parity');}
 const file=pkg.files[0], source=view(page,'源码');await source.locator(`.file-tabs [data-path="${file.path}"]`).click();
 const edited=file.source+'\n// unsaved route draft\n';await write(page,edited);await done(page);assert.equal(await read(page,'源码'),edited);
 await source.getByRole('textbox').focus();await page.keyboard.press('Control+End');await page.keyboard.press('x');
 await page.evaluate(()=>new Promise(resolve=>requestAnimationFrame(()=>requestAnimationFrame(resolve))));await source.getByRole('textbox').focus();await page.keyboard.press('Control+Z');await done(page);assert.equal(await read(page,'源码'),edited,'Monaco undo');
 await source.locator(`.file-tabs [data-path="${pkg.files[1].path}"]`).click();await source.locator(`.file-tabs [data-path="${file.path}"]`).click();assert.equal(await read(page,'源码'),edited);
 await source.getByRole('textbox').focus();await page.keyboard.press('Control+End');await page.keyboard.press('Shift+ArrowLeft');
 await source.locator(`.file-tabs [data-path="${pkg.files[1].path}"]`).click();await source.locator(`.file-tabs [data-path="${file.path}"]`).click();await source.getByRole('textbox').focus();await page.keyboard.press('x');await done(page);assert.equal(await read(page,'源码'),edited.slice(0,-1)+'x','selection restored across models');await source.getByRole('textbox').focus();await page.keyboard.press('Control+Z');await done(page);assert.equal(await read(page,'源码'),edited);
 await nav('RFC 特性');await page.locator('#rfc-list a[href="/rfc.html?feature=traits"]').click();
 await page.goBack();await page.goBack();assert.equal(await read(page,'源码'),edited);assert.equal(await page.evaluate(()=>window.qaDocumentMarker),'same document');
 await page.goForward();await page.goForward();assert.match(page.url(),/feature=traits$/);await nav('Playground');assert.equal(await read(page,'源码'),edited);
 // Both create controls share name/duplicate/path validation.
 await page.fill('#new-file-name','../escape.gox');await source.locator('.tabs-row > .new-file').click();assert.match(await page.textContent('#file-error'),/普通/);
 await page.fill('#new-file-name','extra.gox');await source.locator('.file-tree .new-file').click();await done(page);await write(page,'package main\nconst Extra = 9\n');await done(page);
 await page.fill('#new-file-name','extra.gox');await source.locator('.tabs-row > .new-file').click();assert.match(await page.textContent('#file-error'),/已存在/);
 await page.fill('#new-file-name','second.gox');await source.locator('.tabs-row > .new-file').click();await done(page);assert.equal(await source.locator('.file-tabs [data-path]').count(),pkg.files.length+2);
 await source.getByRole('button',{name:'删除文件',exact:true}).click();await done(page);assert.equal(await read(page,'源码'),'package main\nconst Extra = 9\n');
 for(const layout of ['source','result','horizontal','vertical']){await page.click(`[data-layout="${layout}"]`);assert.equal(await page.locator('.source-panel').isVisible(),layout!=='result');assert.equal(await page.locator('.result-panel').isVisible(),layout!=='source');}
 await source.getByRole('button',{name:'收起文件树',exact:true}).click();assert.equal(await source.locator('.file-tree').isVisible(),false);await source.getByRole('button',{name:'展开文件树',exact:true}).click();
 for(const label of ['生成结果','只读依赖']){
  const pane=view(page,label),original=await read(page,label);assert.equal(await pane.locator('.new-file').count(),0);await pane.getByRole('textbox').focus();await page.keyboard.press('Control+A');await page.keyboard.insertText('should not write');assert.equal(await read(page,label),original,'readonly '+label);
 }
 await view(page,'只读依赖').locator('.file-tabs [data-path]').nth(1).click();assert.match(await page.locator('#playground .provenance').first().textContent(),/SHA-256/);
 await page.click('[data-layout="source"]');await write(page,'package main\nfunc broken( {');await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
 assert.ok(await source.locator('.squiggly-error').count()>0,'compiler location markers');await page.click('[data-layout="result"]');assert.equal(await read(page,'生成结果'),'');
 const oldEditor=await source.locator('.monaco-editor').elementHandle();
 await page.click('[data-layout="horizontal"]');await page.click('#reset');await done(page);
 await source.getByRole('textbox').waitFor({state:'attached'});
 assert.equal(await oldEditor.evaluate(element=>element.isConnected),false,'reset removes old Monaco editor');
 assert.equal(await source.locator('.monaco-editor').count(),1,'one recreated source editor');
 assert.equal(await source.locator('.squiggly-error').count(),0);
 await write(page,pkg.files[0].source+'\n// stop test\n');await page.click('#stop');assert.equal(await read(page,'生成结果'),'');await page.click('#compile');await done(page);
 // Recompilation keeps an existing generated path selected, including helper files.
 const resultPath=pkg.generatedFiles.at(-1).path;await view(page,'生成结果').locator(`.file-tabs [data-path="${resultPath}"]`).click();await write(page,pkg.files[0].source+'\n// path retained\n');await done(page);assert.equal(await view(page,'生成结果').locator('.file-tabs [aria-pressed=true]').getAttribute('data-path'),resultPath);
 await nav('RFC 特性');assert.equal(await page.locator('#rfc-list a').count(),manifest.catalog.items.length);
 for(const item of manifest.catalog.items){await page.locator(`#rfc-list a[href="/rfc.html?feature=${item.id}"]`).click();await page.waitForFunction(title=>document.querySelector('#rfc-content h2')?.textContent===title,item.title);const text=await page.textContent('#rfc-content');assert.ok(text.includes(item.limitation));assert.ok(text.includes(manifest.catalog.statusLabels[item.status]));assert.equal(await page.locator('#rfc-content details').count(),0);}
 for(const example of manifest.examples){const item=manifest.catalog.items.find(item=>item.examples.includes(example.id));await page.locator(`#rfc-list a[href="/rfc.html?feature=${item.id}"]`).click();await page.waitForFunction(title=>document.querySelector('#rfc-content h2')?.textContent===title,item.title);await page.locator(`#rfc-content a[href="/playground.html?example=${example.id}"]`).click();await page.waitForFunction(id=>document.querySelector('#examples')?.value===id,example.id);await done(page);assert.equal(await read(page,'生成结果'),example.go,example.id+' parity');await nav('RFC 特性');}
 await nav('Playground');await page.selectOption('#examples',manifest.examples[0].id);await done(page);
 for(const value of [80,81,82,83,84,85])await write(page,manifest.examples[0].source.replace('21',String(value)));await done(page);assert.match(await read(page,'生成结果'),/85/,'latest rapid edit');
 await write(page,'package main\nfunc main() {}\n'+' '.repeat(32769));await page.waitForFunction(()=>document.querySelector('#diagnostic').textContent.includes('32 KiB'));assert.equal(await read(page,'生成结果'),'');await page.click('#reset');await done(page);
 await nav('本地使用');await page.locator('#project-examples .file-view').first().waitFor();assert.ok(await page.locator('#project-examples .file-view').count()>0);assert.equal(await page.locator('#project-examples .new-file').count(),0);
 for(const project of manifest.projectExamples.filter(project=>project.download)){const link=page.locator(`#project-examples a[href="/${project.download.path}"]`);assert.equal(await link.getAttribute('download'),project.download.path);const response=await context.request.get(base+'/'+project.download.path);assert.equal(response.status(),200);const bytes=await response.body();assert.equal(bytes.subarray(0,2).toString(),'PK');assert.ok(project.download.files.includes('goxide.toml'));assert.ok(project.download.files.includes('goxide.lock'));assert.ok(project.download.files.some(path=>path.endsWith('.gox')));}
 assert.equal(await page.locator('#project-examples a[download]').count(),2);
 for(const route of ['/rfc.html?feature=traits','/playground.html?example=macro','/local.html','/']){assert.equal((await page.goto(base+route)).status(),200);await page.getByRole('navigation',{name:'主导航'}).waitFor();await page.reload();await page.getByRole('navigation',{name:'主导航'}).waitFor();}
 await page.goto(base+'/rfc.html?feature=missing');await page.getByRole('alert').filter({hasText:'找不到此 RFC'}).waitFor();
 await page.goto(base+'/playground.html?example=missing');await page.getByRole('alert').filter({hasText:'找不到此示例'}).waitFor();
 await page.goto(base+'/playground.html?example=package_files');await done(page);
 await fs.mkdir(new URL('qa-artifacts/',import.meta.url),{recursive:true});await page.screenshot({path:new URL('qa-artifacts/preact-monaco-desktop.png',import.meta.url).pathname});
 await page.setViewportSize({width:390,height:844});for(const layout of ['source','result','horizontal','vertical']){await page.click(`[data-layout="${layout}"]`);assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),layout+' reading width');}
 await page.click('[data-layout="horizontal"]');await page.screenshot({path:new URL('qa-artifacts/preact-monaco-narrow.png',import.meta.url).pathname});
 await page.setViewportSize({width:1440,height:1000});assert.equal(await page.locator('.dual-editor').evaluate(element=>getComputedStyle(element).gridTemplateColumns.split(' ').length),2);
 await source.getByRole('textbox').focus();await page.keyboard.press('Control+f');await source.locator('.find-widget.visible').waitFor();await page.keyboard.press('Escape');await source.getByRole('textbox').focus();await page.keyboard.press('Control+Space');await page.waitForFunction(()=>document.querySelector('.suggest-widget.visible'),null,{timeout:10000});await page.keyboard.press('Escape');
 assert.deepEqual(errors,[]);assert.ok(requests.every(request=>request.url().startsWith(base)&&request.method()==='GET'),'same-origin GET only');assert.ok(requests.some(request=>/editor.worker-.*\.js/.test(request.url())),'bundled Monaco worker');assert.ok(!requests.some(request=>request.url().includes('/goxide/assets/')),'organization root base');
 const unavailable=await context.newPage();await unavailable.route('**/examples.json',route=>route.fulfill({status:503,body:'unavailable'}));await unavailable.goto(base+'/');await unavailable.getByRole('alert').filter({hasText:'503'}).waitFor();await unavailable.close();
 const stale=await context.newPage();await stale.route('**/compiler.wasm*',route=>route.fulfill({contentType:'application/wasm',body:Buffer.from([0,97,115,109,1,0,0,0])}));await stale.goto(base+'/playground.html');await stale.waitForFunction(()=>document.querySelector('#diagnostic')?.textContent.includes('完整性不匹配'));assert.equal(await read(stale,'生成结果'),'');await stale.close();
 const editorFailure=await context.newPage();const loadErrors=[];editorFailure.on('pageerror',error=>loadErrors.push(error.message));
 await editorFailure.route('**/assets/MonacoPane-*.js',route=>route.fulfill({status:503,contentType:'text/javascript',body:'unavailable'}));
 await editorFailure.goto(base+'/playground.html');
 await view(editorFailure,'源码').getByRole('alert').filter({hasText:'编辑器加载失败'}).waitFor();
 assert.equal(await view(editorFailure,'源码').getByRole('textbox').count(),0);assert.deepEqual(loadErrors,[]);await editorFailure.close();
 console.log(JSON.stringify({url:base,compilerCommit:manifest.commit,websiteCommit:manifest.websiteCommit,wasmSha256:manifest.wasmSha256,examples:manifest.examples.length,packageGeneratedFiles:pkg.generatedFiles.length,rfcItems:manifest.catalog.items.length,preact:true,monaco:true,modelsUndo:true,editorRecreated:true,lazyFailure:true,readonly:true,layouts:4,clientHistoryDraft:true,deepLinksReload:true,lazyEditor:true,rootSiteAssets:true,narrowReading:true,errors},null,2));
}finally{await browser.close();}
