import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
const base=process.env.QA_URL||'http://127.0.0.1:8084';
const args=[];if(process.env.QA_PROXY_SPKI)args.push('--ignore-certificate-errors-spki-list='+process.env.QA_PROXY_SPKI);
const browser=await chromium.launch({executablePath:process.env.CHROMIUM||'/usr/bin/chromium',headless:true,chromiumSandbox:true,args});
try {
 const context=await browser.newContext({viewport:{width:1440,height:1000},reducedMotion:'reduce',permissions:['clipboard-read','clipboard-write']});
 const page=await context.newPage();const errors=[],requests=[],consoleErrors=[];
 page.on('pageerror',e=>errors.push(e.message));page.on('console',m=>{if(m.type()==='error')consoleErrors.push(m.text());});
 context.on('request',r=>requests.push({url:r.url(),method:r.method(),body:r.postData()}));
 await page.goto(base);
 const done=()=>page.waitForFunction(()=>document.querySelector('#status').textContent.startsWith('转译完成'),{},{timeout:60000});await done();
 const manifest=await page.evaluate(async()=>await(await fetch('examples.json')).json());
 assert.equal(manifest.catalog.compilerCommit,manifest.commit);
 assert.deepEqual(manifest.abiVersions,{enum:8,reference:3,use:2,sum:2});assert.equal(manifest.metadataSchema,8);assert.equal(manifest.publicLibraries.length,2);
 assert.ok(requests.some(r=>new URL(r.url).pathname.endsWith('/compiler.wasm')&&new URL(r.url).searchParams.get('compiler')===manifest.commit));
 const wasmHash=await page.evaluate(async sha=>{const bytes=await(await fetch('compiler.wasm?compiler='+sha)).arrayBuffer();return Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',bytes)),b=>b.toString(16).padStart(2,'0')).join('');},manifest.commit);
 assert.equal(wasmHash,manifest.wasmSha256);
 assert.equal(await page.locator('.feature-card').count(),manifest.catalog.items.length);
 for(const item of manifest.catalog.items){
  const card=page.locator('#feature-'+item.id);
  assert.ok((await card.textContent()).includes(item.limitation));
  assert.equal(await card.locator('.example-open').count(),item.examples.length);
  assert.ok((await card.locator('.status-badge').textContent())===manifest.catalog.statusLabels[item.status]);
 }
 await page.fill('#catalog-search','0024');assert.equal(await page.locator('.feature-card:visible').count(),manifest.catalog.items.filter(c=>c.rfcs.includes('0024')).length);
 await page.selectOption('#catalog-status','partial');assert.equal(await page.locator('.feature-card:visible').count(),manifest.catalog.items.filter(c=>c.rfcs.includes('0024')&&c.status==='partial').length);
 await page.fill('#catalog-search','no-such-feature');assert.equal(await page.locator('.feature-card:visible').count(),0);assert.ok(await page.locator('#catalog-empty').isVisible());
 await page.click('#catalog-clear');assert.equal(await page.locator('.feature-card:visible').count(),manifest.catalog.items.length);
 for(const topic of Object.keys(manifest.catalog.topics)){await page.click(`[data-topic="${topic}"]`);assert.equal(await page.locator('.feature-card:visible').count(),manifest.catalog.items.filter(c=>c.topic===topic).length);}
 await page.click('#catalog-clear');await page.selectOption('#catalog-support','browser');assert.equal(await page.locator('.feature-card:visible').count(),manifest.catalog.items.filter(c=>c.support==='browser').length);
 // A permalink must reveal its card even when filters would hide it.
 await page.evaluate(()=>location.hash='feature-use-paths');await page.waitForFunction(()=>!document.querySelector('#feature-use-paths').hidden);
 assert.equal(await page.locator('.feature-card:visible').count(),manifest.catalog.items.length);
 // Every advertised Playground example is clicked from its card and really compiled.
 for(const example of manifest.examples){
  const item=manifest.catalog.items.find(c=>c.examples.includes(example.id));
  const card=page.locator('#feature-'+item.id);await card.locator(':scope > details').evaluate(e=>e.open=true);
  await card.locator(`[data-example="${example.id}"]`).click();await done();
  assert.equal(await page.inputValue('#output'),example.go,example.id+' browser parity');
 }
 for(const check of manifest.catalog.checks){
  await page.fill('#source',check.source);
  await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
  assert.equal(await page.inputValue('#output'),'');assert.ok(await page.isDisabled('#copy'));
  // Native filename is normalized by build.py; browser always uses playground.gox.
  assert.equal((await page.textContent('#diagnostic')).trim(),check.diagnostic,check.id+' rejection parity');
 }
 await page.click('#reset');await done();
 assert.ok(await page.inputValue('#source'));
 await page.fill('#source',manifest.examples[0].source.replace('21','37'));await done();assert.match(await page.inputValue('#output'),/37/);
 await page.click('#copy');assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),await page.inputValue('#output'));
 await page.click('#reset');await done();
 // Check all internal fragment links and project snapshots without private documentation URLs.
 const missing=await page.locator('a[href^="#"]').evaluateAll(links=>links.map(a=>a.getAttribute('href')).filter(h=>h!=='#'&&!document.getElementById(h.slice(1))));assert.deepEqual(missing,[]);
 for(const project of manifest.projectExamples){const text=await page.locator('#project-examples').textContent();for(const file of project.files)assert.ok(text.includes(file.source));for(const run of project.runs)assert.ok(text.includes(run.stdout));}
 await page.locator('.feature-card > details').evaluateAll(nodes=>nodes.forEach(n=>n.open=false));
 await page.click('#catalog-clear');await page.locator('#catalog').scrollIntoViewIfNeeded();
 await fs.mkdir(new URL('qa-artifacts/',import.meta.url),{recursive:true});
 await page.screenshot({path:new URL('qa-artifacts/catalog-desktop.png',import.meta.url).pathname});
 await page.setViewportSize({width:390,height:844});await page.locator('#catalog').evaluate(e=>e.scrollIntoView());
 assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await page.screenshot({path:new URL('qa-artifacts/catalog-mobile.png',import.meta.url).pathname});
 await page.locator('.catalog-controls').evaluate(e=>e.scrollIntoView());await page.screenshot({path:new URL('qa-artifacts/catalog-mobile-controls.png',import.meta.url).pathname});
 await page.locator('#feature-serde > details').evaluate(e=>e.open=true);
 await page.locator('#feature-serde > details details').first().evaluate(e=>e.open=true);
 await page.locator('#feature-serde').scrollIntoViewIfNeeded();assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth));
 await page.screenshot({path:new URL('qa-artifacts/catalog-mobile-example.png',import.meta.url).pathname});
 assert.deepEqual(errors,[]);assert.deepEqual(consoleErrors,[]);
 assert.ok(requests.every(r=>r.url.startsWith(base)&&r.method==='GET'&&r.body===null));
 const result={url:base,compilerCommit:manifest.commit,websiteCommit:manifest.websiteCommit,wasmSha256:wasmHash,metadataSchema:manifest.metadataSchema,cards:manifest.catalog.items.length,examples:manifest.examples.length,negativeExamples:manifest.catalog.checks.length,filters:true,permalinks:true,snapshots:true,mobile390:true,clipboard:true,parity:'byte-for-byte',errors,consoleErrors,network:'same-origin GET only'};
 console.log(JSON.stringify(result,null,2));
 // Intentional unavailable manifest: readable error state instead of stale or fake data.
 const failure=await context.newPage();await failure.route('**/examples.json',r=>r.fulfill({status:503,body:'unavailable'}));await failure.goto(base);
 await failure.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
 assert.match(await failure.textContent('#catalog-count'),/暂时不可用/);assert.ok(await failure.isDisabled('#compile'));assert.equal(await failure.locator('.feature-card').count(),0);
 console.log('manifest failure state: passed');
 const stale=await context.newPage();await stale.route('**/compiler.wasm*',r=>r.fulfill({contentType:'application/wasm',body:Buffer.from([0,97,115,109,1,0,0,0])}));await stale.goto(base);
 await stale.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
 assert.match(await stale.textContent('#diagnostic'),/完整性不匹配/);assert.equal(await stale.inputValue('#output'),'');assert.ok(await stale.isDisabled('#copy'));
 console.log('stale/corrupt WASM rejected before execution: passed');
} finally {await browser.close();}
