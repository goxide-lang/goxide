import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
const base = process.env.QA_URL || 'http://127.0.0.1:8080';
const browser = await chromium.launch({executablePath:process.env.CHROMIUM || '/usr/bin/chromium',headless:true,args:['--no-sandbox',...(process.env.QA_PROXY_SPKI ? ['--ignore-certificate-errors-spki-list='+process.env.QA_PROXY_SPKI] : [])]});
const context = await browser.newContext({viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write']});
const page = await context.newPage();
const errors=[];const requests=[];
page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>requests.push(r.url()));
await fs.mkdir(new URL('qa-artifacts/',import.meta.url),{recursive:true});
const finished=()=>page.waitForFunction(()=>document.querySelector('#status').textContent.startsWith('转译完成'),{},{timeout:60000});
await page.goto(base);await finished();
const manifest=await page.evaluate(async()=>await(await fetch('examples.json')).json());
// CLI project snapshots stay read-only, separate from browser example choices.
assert.equal(await page.locator('#examples option').count(), manifest.examples.length);
const snapshotText = await page.locator('#cli-example').textContent();
for (const example of manifest.cliExamples) {
 for (const file of example.files) {
  assert.ok(snapshotText.includes(file.source));
  assert.ok(snapshotText.includes(file.go));
 }
 assert.ok(snapshotText.includes(example.stdout));
}
for(let i=0;i<manifest.examples.length;i++){
 await page.selectOption('#examples',String(i));await finished();
 assert.equal(await page.inputValue('#output'),manifest.examples[i].go,`${manifest.examples[i].id}: WASM differs from CLI`);
}
// Native CLI runs only in this local QA process, never in the web server.
const checkResults=[];
const appendChecks = [
 'macro_append_native','macro_append_shadow','macro_append_string','macro_append_question','macro_append_function',
 'reject_macro_append_slice','reject_macro_append_element','reject_macro_append_arity','reject_macro_append_ellipsis',
 'reject_macro_append_definition_var','reject_macro_append_definition_const','reject_macro_append_definition_type',
 'reject_macro_append_definition_import','reject_macro_append_value','reject_macro_append_named_type',
];
const copyChecks = ['macro_copy_native',
 'macro_copy_string',
 'macro_copy_shadow',
 'macro_copy_function',
 'macro_copy_question',
 'macro_copy_readonly_source',
 'reject_macro_copy_slice',
 'reject_macro_copy_element',
 'reject_macro_copy_string_rune',
 'reject_macro_copy_arity',
 'reject_macro_copy_ellipsis',
 'reject_macro_copy_definition_var',
 'reject_macro_copy_definition_const',
 'reject_macro_copy_definition_type',
 'reject_macro_copy_definition_import',
 'reject_macro_copy_value',
 'reject_macro_copy_named_type',
 'reject_macro_copy_readonly_target',
 'reject_macro_copy_readonly_pointer'];
// Ordinary Go type errors stay in the existing native compilation phase.
// The browser only transpiles; it must produce identical invalid Go here.
const nativeTypeReject = {
 reject_macro_copy_slice: /copy.*(?:must be a slice|must be slice)|invalid copy/s,
 reject_macro_copy_element: /different element types/,
 reject_macro_copy_string_rune: /different element types/,
 reject_macro_copy_arity: /not enough arguments (?:to|for) copy/,
 reject_macro_copy_ellipsis: /invalid use of.*with.*copy|invalid operation.*copy.*\.\.\./s,
 reject_macro_append_slice: /(?:first argument to append must be a slice|invalid append: argument must be a slice)/,
 reject_macro_append_element: /cannot use.*as int value in argument to append/s,
 reject_macro_append_arity: /not enough arguments (?:to|for) append/,
 reject_macro_append_ellipsis: /too many arguments in call to append/,
};
const frontendReject = {
 reject_macro_copy_definition_var: /definition-site declaration/,
 reject_macro_copy_definition_const: /definition-site declaration/,
 reject_macro_copy_definition_type: /definition-site declaration/,
 reject_macro_copy_definition_import: /definition-site import/,
 reject_macro_copy_value: /requires a direct call/,
 reject_macro_copy_named_type: /local named type/,
 reject_macro_copy_readonly_target: /readonly|read-only/,
 reject_macro_copy_readonly_pointer: /readonly|read-only/,
 reject_macro_append_definition_var: /definition-site declaration/,
 reject_macro_append_definition_const: /definition-site declaration/,
 reject_macro_append_definition_type: /definition-site declaration/,
 reject_macro_append_definition_import: /definition-site import/,
 reject_macro_append_value: /requires a direct call/,
 reject_macro_append_named_type: /local named type/,
};
const temp=await fs.mkdtemp(path.join(tmpdir(),'goxide-browser-parity-'));
try {
 for (const name of ['error_only','typed_nil','reject_wrong_type','pointer_error','named_error','slice_error','reject_value_error','reject_arity','reject_legacy_cfg','reject_macro_selector','reject_macro_prepared','reject_derive_origin','reject_enum_derive_origin','macro_imported_helper',...appendChecks,...copyChecks]) {
  const source=await fs.readFile(new URL(`checks/${name}.gox`,import.meta.url),'utf8');
  const input=path.join(temp,'playground.gox');await fs.writeFile(input,source);
  let nativeGo, nativeError;
  try { nativeGo=execFileSync(process.env.GOXIDE_BIN || fileURLToPath(new URL('../bin/goxide',import.meta.url)),['transpile',input],{encoding:'utf8',cwd:fileURLToPath(new URL('..',import.meta.url)),stdio:['ignore','pipe','pipe']}); }
  catch(e) {nativeError=String(e.stderr).trim().replaceAll(input,'playground.gox');}
  await page.fill('#source',source);
  if(name.startsWith('reject_') && !nativeTypeReject[name]) {
   assert.ok(nativeError,`${name}: CLI unexpectedly accepted input`);
   if(frontendReject[name])assert.match(nativeError,frontendReject[name],`${name}: wrong rejection`);
   await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
   assert.equal((await page.textContent('#diagnostic')).trim(),nativeError,`${name}: diagnostics differ`);
   assert.equal(await page.inputValue('#output'),'');
   assert.equal(await page.isDisabled('#copy'),true);
   // Each builtin/readonly rejection must recover through a real subsequent WASM compile.
   if(frontendReject[name]) {
    await page.fill('#source',manifest.examples[0].source);await finished();
    assert.equal(await page.inputValue('#output'),manifest.examples[0].go,`${name}: error recovery differs`);
   }
  } else {
   assert.equal(nativeError,undefined,`${name}: CLI rejected input`);
   await finished();assert.equal(await page.inputValue('#output'),nativeGo,`${name}: generated Go differs`);
   if(nativeTypeReject[name]) {
    const generated=path.join(temp,'invalid.go');await fs.writeFile(generated,nativeGo);
    let goError;
    try {execFileSync(process.env.GO_BIN || 'go',['build','-o',path.join(temp,'invalid.a'),generated],{encoding:'utf8',cwd:fileURLToPath(new URL('..',import.meta.url)),stdio:['ignore','pipe','pipe']});}
    catch(e) {goError=String(e.stderr);}
    assert.ok(goError,`${name}: native Go unexpectedly accepted invalid builtin call`);
    assert.match(goError,nativeTypeReject[name],`${name}: wrong native Go rejection`);
   }
  }
  checkResults.push(name);
 }
} finally {await fs.rm(temp,{recursive:true,force:true});}
await page.selectOption('#examples','0');await finished();
await page.fill('#source',manifest.examples[0].source.replace('21','37'));await finished();
assert.match(await page.inputValue('#output'),/37/);
await page.click('#copy');assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),await page.inputValue('#output'));
await page.fill('#source','package main\nfunc main( {');
await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
assert.equal(await page.inputValue('#output'),'');assert.equal(await page.isDisabled('#copy'),true);
await page.click('#reset');await finished();
for(let n=0;n<6;n++)await page.fill('#source',manifest.examples[0].source.replace('21',String(80+n)));
await finished();assert.match(await page.inputValue('#output'),/85/);
await page.fill('#source','package main\nfunc main(){}\n'+' '.repeat(32769));
await page.waitForFunction(()=>document.querySelector('#diagnostic').textContent.includes('32 KiB'));
await page.click('#reset');await finished();
await page.fill('#source','package main\nimport "net/http"\nfunc main(){ _ = http.MethodGet }');
await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
assert.match(await page.textContent('#diagnostic'),/outside the bundled package set/);
await page.fill('#source','package main\n// <img src=x onerror=alert(1)>\nfunc main() {}');await finished();assert.equal(await page.locator('img').count(),0);
await page.locator('#source').focus();await page.keyboard.press('Control+Enter');await finished();await page.keyboard.press('Tab');assert.equal(await page.evaluate(()=>document.activeElement.id),'copy');
await page.click('#reset');await finished();
await page.locator('h1').click();
await page.screenshot({path:new URL('qa-artifacts/desktop.png',import.meta.url).pathname,fullPage:true});
await page.setViewportSize({width:390,height:844});
assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
await page.screenshot({path:new URL('qa-artifacts/mobile.png',import.meta.url).pathname,fullPage:true});
// Intercept only a QA worker to deterministically exercise hard timeout and cancel.
await page.route('**/worker.js*',r=>r.fulfill({contentType:'application/javascript',body:'postMessage({type:"ready"}); onmessage=()=>{};'}));
await page.reload();await page.waitForFunction(()=>document.querySelector('#status').textContent==='正在转译…');
await page.waitForFunction(()=>document.querySelector('#diagnostic').textContent.includes('超过 10 秒'),{},{timeout:15000});
assert.equal(await page.inputValue('#output'),'');
await page.click('#compile');await page.waitForFunction(()=>document.querySelector('#status').textContent==='正在转译…');await page.click('#stop');
assert.match(await page.textContent('#status'),/已中断/);
await page.unroute('**/worker.js*');await page.click('#reset');await finished();
assert.equal(await page.inputValue('#output'),manifest.examples[0].go);
assert.deepEqual(errors,[]);
assert.equal(requests.every(u=>u.startsWith(base)),true,'external network request');
const builtinResults=names=>({positive:checkResults.filter(name=>names.includes(name)&&!name.startsWith('reject_')),frontendReject:Object.keys(frontendReject).filter(name=>names.includes(name)),nativeTypeReject:Object.keys(nativeTypeReject).filter(name=>names.includes(name)),nativeTypeRejectWasm:'transpilation parity only; final Go compilation is native'});
console.log(JSON.stringify({examples:manifest.examples.length,parity:'byte-for-byte',goResultChecks:checkResults.slice(0,9),diagnosticChecks:checkResults.slice(9).filter(name=>!appendChecks.includes(name)&&!copyChecks.includes(name)),appendChecks:builtinResults(appendChecks),copyChecks:builtinResults(copyChecks),checks:['edit','error recovery','rapid edits','copy','32 KiB','unsupported import','XSS','keyboard','390px','hard timeout','interrupt','reset','no external requests'],errors},null,2));
await browser.close();
