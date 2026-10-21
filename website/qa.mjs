import {write as writeSource,read as readEditor,view,done} from './monaco-qa.mjs';
import {chromium} from 'playwright';
import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import {execFileSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {recordWorkerObservations} from './validation/worker-observations.mjs';
const base = process.env.QA_URL || 'http://127.0.0.1:8080';
const browser = await chromium.launch({executablePath:process.env.CHROMIUM || '/usr/bin/chromium',headless:true,chromiumSandbox:true,args:process.env.QA_PROXY_SPKI ? ['--ignore-certificate-errors-spki-list='+process.env.QA_PROXY_SPKI] : []});
const context = await browser.newContext({viewport:{width:1440,height:1000},permissions:['clipboard-read','clipboard-write']});
const page = await context.newPage();
const errors=[];const requests=[];
page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>requests.push(r.url()));
await fs.mkdir(new URL('qa-artifacts/',import.meta.url),{recursive:true});
const finished=()=>done(page);
await page.goto(base.replace(/\/$/,'')+'/playground.html');await finished();
const manifest=await page.evaluate(async()=>await(await fetch('examples.json')).json());
const a04Matrix=JSON.parse(await fs.readFile(new URL('a04-matrix.json',import.meta.url),'utf8'));
const a04Results=[];
const a01Matrix=JSON.parse(await fs.readFile(new URL('a01-matrix.json',import.meta.url),'utf8'));
const a01Results=[];
// CLI project snapshots stay read-only, separate from browser example choices.
assert.equal(await page.locator('#examples option').count(), manifest.examples.length + (manifest.packageExamples || []).length);
// Readonly browsing and route snapshots are verified by catalog-qa.mjs.
for(let i=0;i<manifest.examples.length;i++){
 await page.selectOption('#examples',manifest.examples[i].id);await finished();
 assert.equal(await readEditor(page,'生成结果'),manifest.examples[i].go,`${manifest.examples[i].id}: WASM differs from CLI`);
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
const allocationChecks = [
 "macro_allocation_native",
 "macro_allocation_new_types",
 "macro_allocation_shadow",
 "macro_allocation_function",
 "macro_allocation_question",
 "reject_macro_allocation_new_expression",
 "reject_macro_allocation_capture_expr",
 "reject_macro_allocation_capture_ident",
 "reject_macro_allocation_capture_tt",
 "reject_macro_allocation_nested_capture",
 "reject_macro_allocation_make_capture",
 "reject_macro_allocation_named_type",
 "reject_macro_allocation_generic_type",
 "reject_macro_allocation_struct_type",
 "reject_macro_allocation_interface_type",
 "reject_macro_allocation_func_type",
 "reject_macro_allocation_local_primitive",
 "reject_macro_allocation_primitive_function",
 "reject_macro_allocation_primitive_import",
 "reject_macro_allocation_definition_var",
 "reject_macro_allocation_definition_const",
 "reject_macro_allocation_definition_type",
 "reject_macro_allocation_definition_import",
 "reject_macro_allocation_make_value",
 "reject_macro_allocation_new_value",
 "reject_macro_allocation_make_local_type",
 "reject_macro_allocation_new_local_type",
 "reject_macro_allocation_make_type",
 "reject_macro_allocation_make_arity",
 "reject_macro_allocation_make_negative",
 "reject_macro_allocation_make_capacity",
 "reject_macro_allocation_map_key",
 "reject_macro_allocation_array_length",
 "reject_macro_allocation_new_arity",
 "reject_macro_allocation_ellipsis"
];
// These are valid Go 1.27 programs, intentionally outside this macro type-form batch.
const nativeValidControls = {
 reject_macro_allocation_new_expression: 'macro_allocation_new_expression_native.go.txt',
};
const allocationTypeError = /macro allocation requires fixed predeclared type syntax/;
// Ordinary Go type errors stay in the existing native compilation phase.
// The browser only transpiles; it must produce identical invalid Go here.
const nativeTypeReject = {
 reject_macro_allocation_make_type: /make.*(?:slice|map|chan)|invalid argument.*make/s,
 reject_macro_allocation_make_arity: /missing len argument to make|not enough arguments.*make|make.*expects.*arguments/s,
 reject_macro_allocation_make_negative: /negative|must not be negative|overflows/s,
 reject_macro_allocation_make_capacity: /length and capacity swapped|len.*larger than cap|length.*capacity/s,
 reject_macro_allocation_map_key: /invalid map key type/,
 reject_macro_allocation_array_length: /invalid array length|must be non-negative/,
 reject_macro_allocation_new_arity: /too many arguments.*new/s,
 reject_macro_allocation_ellipsis: /invalid use of.*with.*make/s,
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
 reject_macro_allocation_new_expression: allocationTypeError,
 reject_macro_allocation_capture_expr: /macro allocation type does not support captured types/,
 reject_macro_allocation_capture_ident: /macro allocation type does not support captured types/,
 reject_macro_allocation_capture_tt: /macro allocation type does not support captured types/,
 reject_macro_allocation_nested_capture: /macro allocation type does not support captured types/,
 reject_macro_allocation_make_capture: /macro allocation type does not support captured types/,
 reject_macro_allocation_named_type: allocationTypeError,
 reject_macro_allocation_generic_type: allocationTypeError,
 reject_macro_allocation_struct_type: /template local type\/struct\/interface\/label declarations are unsupported/,
 reject_macro_allocation_interface_type: /template local type\/struct\/interface\/label declarations are unsupported/,
 reject_macro_allocation_func_type: allocationTypeError,
 reject_macro_allocation_local_primitive: /macro allocation type cannot use a template-local binding/,
 reject_macro_allocation_primitive_function: /template primitive int.*definition-site/s,
 reject_macro_allocation_primitive_import: /template primitive int.*definition-site/s,
 reject_macro_allocation_definition_var: /definition-site declaration/,
 reject_macro_allocation_definition_const: /definition-site declaration/,
 reject_macro_allocation_definition_type: /definition-site declaration/,
 reject_macro_allocation_definition_import: /definition-site import/,
 reject_macro_allocation_make_value: /requires a direct call/,
 reject_macro_allocation_new_value: /requires a direct call/,
 reject_macro_allocation_make_local_type: /local named type/,
 reject_macro_allocation_new_local_type: /local named type/,

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
// A03 uses independent source anchors, not CLI/WASM agreement alone, as its oracle.
// All fixtures reuse playground.gox; different contents and positions must not leak stacks.
const originDiagnosticChecks = [
 {
  "name": "reject_macro_origin_synthetic_new",
  "message": "macro allocation requires fixed predeclared type syntax",
  "point": null,
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!(value())"
   }
  ]
 },
 {
  "name": "reject_macro_origin_synthetic_context",
  "message": "macro allocation requires fixed predeclared type syntax",
  "point": null,
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!(value())"
   },
   {
    "macro": "inner",
    "definition": "macro_rules! inner",
    "call": "inner!($x)"
   }
  ]
 },
 {
  "name": "reject_macro_origin_synthetic_make",
  "message": "macro allocation requires fixed predeclared type syntax",
  "point": null,
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!(value())"
   }
  ]
 },
 {
  "name": "reject_macro_origin_synthetic_syntax",
  "message": "unsupported template syntax:",
  "point": null,
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!(value())"
   },
   {
    "macro": "inner",
    "definition": "macro_rules! inner",
    "call": "inner!($x)"
   }
  ]
 },
 {
  "name": "reject_macro_origin_real_new",
  "message": "macro allocation requires fixed predeclared type syntax",
  "point": {
   "anchor": "new(7)",
   "offset": 4,
   "token": "7"
  },
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!()"
   },
   {
    "macro": "inner",
    "definition": "macro_rules! inner",
    "call": "inner!()"
   }
  ]
 },
 {
  "name": "reject_macro_origin_real_make",
  "message": "macro allocation requires fixed predeclared type syntax",
  "point": {
   "anchor": "make(7, 1)",
   "offset": 5,
   "token": "7"
  },
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!()"
   }
  ]
 },
 {
  "name": "reject_macro_origin_real_syntax",
  "message": "unsupported template syntax:",
  "point": {
   "anchor": "1 +",
   "offset": 0,
   "token": "1"
  },
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!()"
   }
  ]
 },
 {
  "name": "reject_macro_origin_capture_question",
  "message": "macro allocation type does not support captured types",
  "point": {
   "anchor": "new($x)",
   "offset": 4,
   "token": "$"
  },
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!(value()?)"
   }
  ]
 }
];
const astPositiveChecks = ["macro_ast_question_behavior"];
const astDiagnosticChecks = [
 {
  "name": "reject_macro_ast_question",
  "message": "for built-in len",
  "point": null,
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!(value())"
   }
  ]
 },
 {
  "name": "reject_macro_ast_nested",
  "message": "for built-in len",
  "point": null,
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!(value())"
   },
   {
    "macro": "inner",
    "definition": "macro_rules! inner",
    "call": "inner!($x)"
   }
  ]
 },
 {
  "name": "reject_macro_ast_sibling",
  "message": "for built-in len",
  "point": null,
  "frames": [
   {
    "macro": "outer",
    "definition": "macro_rules! outer",
    "call": "outer!(value())",
    "callOccurrence": 1
   }
  ]
 },
 {
  "name": "reject_macro_ast_nonmacro",
  "message": "for built-in len",
  "point": null,
  "frames": []
 },
 {
  "name": "reject_macro_ast_real_point",
  "message": "cannot use 1",
  "point": {
   "anchor": "var wrong string = 1",
   "offset": 19,
   "token": "1"
  },
  "frames": []
 }
];
const allOriginChecks=[...originDiagnosticChecks,...astDiagnosticChecks];
const originResults=[];
function originPosition(source, anchor, offset=0, occurrence) {
 let index=source.indexOf(anchor);
 if(occurrence===undefined)assert.ok(index>=0 && source.indexOf(anchor,index+1)<0,`ambiguous origin anchor: ${anchor}`);
 else {
  assert.ok(Number.isInteger(occurrence)&&occurrence>=0,`invalid origin occurrence: ${anchor}`);
  for(let i=0;i<occurrence&&index>=0;i++)index=source.indexOf(anchor,index+1);
  assert.ok(index>=0,`missing origin occurrence: ${anchor}`);
 }
 const prefix=source.slice(0,index+offset);
 return {start:Buffer.byteLength(prefix),line:prefix.split('\n').length,column:Buffer.byteLength(prefix.slice(prefix.lastIndexOf('\n')+1))+1};
}
function assertOriginDiagnostic(diagnostic,check,source) {
 const [primary,...frames]=diagnostic.split('\n');
 assert.ok(primary.includes(check.message),`${check.name}: wrong diagnostic`);
 if(check.point) {
  const p=originPosition(source,check.point.anchor,check.point.offset);
  assert.ok(primary.startsWith(`playground.gox:${p.line}:${p.column}:`),`${check.name}: wrong original position`);
  assert.ok(primary.endsWith(`(source span [${p.start},${p.start+Buffer.byteLength(check.point.token)}))`),`${check.name}: wrong original token span`);
  assert.doesNotMatch(primary,/generated\/unmapped/);
 } else {
  assert.ok(primary.endsWith('[generated/unmapped]'),`${check.name}: synthetic point claimed a source`);
  assert.doesNotMatch(primary,/source span/);
 }
 assert.deepEqual(frames,check.frames.map(frame=>{
  const d=originPosition(source,frame.definition),c=originPosition(source,frame.call,0,frame.callOccurrence);
  return `  macro ${frame.macro} defined at playground.gox:${d.line}:${d.column}; invoked at playground.gox:${c.line}:${c.column}`;
 }),`${check.name}: inaccurate or reordered macro stack`);
}
const temp=await fs.mkdtemp(path.join(tmpdir(),'goxide-browser-parity-'));
try {
 for (const name of ['error_only','typed_nil','reject_wrong_type','pointer_error','named_error','slice_error','reject_value_error','reject_arity','reject_legacy_cfg','reject_macro_selector','reject_macro_prepared','reject_derive_origin','reject_enum_derive_origin','macro_imported_helper',...appendChecks,...copyChecks,...allocationChecks,...astPositiveChecks]) {
  const source=await fs.readFile(new URL(`checks/${name}.gox`,import.meta.url),'utf8');
  const input=path.join(temp,'playground.gox');await fs.writeFile(input,source);
  let nativeGo, nativeError;
  try { execFileSync(process.env.GOXIDE_BIN || fileURLToPath(new URL('../bin/goxide',import.meta.url)),['transpile','-o',input+'.go',input],{encoding:'utf8',cwd:fileURLToPath(new URL('..',import.meta.url)),stdio:['ignore','pipe','pipe']}); nativeGo=await fs.readFile(input+'.go','utf8'); }
  catch(e) {nativeError=String(e.stderr).trim().replaceAll(input,'playground.gox');}
  await writeSource(page,source);
  if(name.startsWith('reject_') && !nativeTypeReject[name]) {
   assert.ok(nativeError,`${name}: CLI unexpectedly accepted input`);
   if(frontendReject[name])assert.match(nativeError,frontendReject[name],`${name}: wrong rejection`);
   await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
   assert.equal((await page.textContent('#diagnostic')).trim(),nativeError,`${name}: diagnostics differ`);
   assert.equal(await readEditor(page,'生成结果'),'');
   assert.equal(await view(page,'生成结果').getByRole('button',{name:'复制文件',exact:true}).isDisabled(),true);
   if(nativeValidControls[name]) {
    const control=path.join(temp,'valid-go-control.go');
    await fs.writeFile(control,await fs.readFile(new URL(`checks/${nativeValidControls[name]}`,import.meta.url),'utf8'));
    execFileSync(process.env.GO_BIN || 'go',['run',control],{encoding:'utf8',cwd:fileURLToPath(new URL('..',import.meta.url)),stdio:['ignore','pipe','pipe']});
   }
   // Each builtin/readonly rejection must recover through a real subsequent WASM compile.
   if(frontendReject[name]) {
    await writeSource(page,manifest.examples[0].source);await finished();
    assert.equal(await readEditor(page,'生成结果'),manifest.examples[0].go,`${name}: error recovery differs`);
   }
  } else {
   assert.equal(nativeError,undefined,`${name}: CLI rejected input`);
   await finished();assert.equal(await readEditor(page,'生成结果'),nativeGo,`${name}: generated Go differs`);
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
 // UI textarea values normalize CRLF. Raw worker calls separately verify exact CRLF bytes.
 const originWorkerInputs=[];
 for(const check of allOriginChecks) {
  const fixture=await fs.readFile(new URL(`checks/${check.name}.gox`,import.meta.url),'utf8');
  for(const ending of ['LF','CRLF']) {
   const source=ending==='LF'?fixture:fixture.replaceAll('\n','\r\n');
   const input=path.join(temp,'playground.gox');await fs.writeFile(input,source);
   let expected;
   for(let repeat=0;repeat<2;repeat++) {
    let diagnostic;
    try {execFileSync(process.env.GOXIDE_BIN || fileURLToPath(new URL('../bin/goxide',import.meta.url)),['transpile','-o',input+'.go',input],{encoding:'utf8',cwd:fileURLToPath(new URL('..',import.meta.url)),stdio:['ignore','pipe','pipe']});}
    catch(e) {diagnostic=String(e.stderr).trim().replaceAll(input,'playground.gox');}
    assert.ok(diagnostic,`${check.name}: CLI unexpectedly accepted input`);
    assertOriginDiagnostic(diagnostic,check,source);
    if(repeat)assert.equal(diagnostic,expected,`${check.name}: nondeterministic CLI diagnostic`);
    expected=diagnostic;
    if(ending==='LF') {
     await writeSource(page,source);await page.click('#compile');
     await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
     assert.equal((await page.textContent('#diagnostic')).trim(),expected,`${check.name}: UI diagnostic differs`);
     assert.equal(await readEditor(page,'生成结果'),'');assert.equal(await view(page,'生成结果').getByRole('button',{name:'复制文件',exact:true}).isDisabled(),true);
     await writeSource(page,manifest.examples[0].source);await finished();
     assert.equal(await readEditor(page,'生成结果'),manifest.examples[0].go,`${check.name}: UI recovery differs`);
    }
    originWorkerInputs.push({source,expected,name:check.name,ending,repeat});
    originWorkerInputs.push({source:manifest.examples[0].source,go:manifest.examples[0].go,name:check.name,ending,repeat,recovery:true});
   }
   originResults.push({name:check.name,ending,diagnostic:expected});
  }
 }
 // One real WASM worker processes every input and recovery, retaining the same filename.
 const rawResults=await page.evaluate(async({inputs,commit,hash})=>{
  const worker=new Worker(`worker.js?compiler=${commit}&wasm=${hash}`);
  let timer;
  const response=()=>new Promise((resolve,reject)=>{
   timer=setTimeout(()=>reject(new Error('origin QA worker timeout')),30000);
   worker.onerror=e=>{clearTimeout(timer);reject(new Error(e.message));};
   worker.onmessage=({data})=>{clearTimeout(timer);data.type==='fatal'?reject(new Error(data.error)):resolve(data);};
  });
  try {
   if((await response()).type!=='ready')throw new Error('origin QA worker not ready');
   const results=[];
   for(let id=0;id<inputs.length;id++) {
    const pending=response();worker.postMessage({type:'compile',id,source:inputs[id].source});
    const result=await pending;
    if(result.type!=='result'||result.id!==id)throw new Error('origin QA worker result mismatch');
    results.push(result);
   }
   return results;
  } finally {clearTimeout(timer);worker.terminate();}
 },{inputs:originWorkerInputs,commit:manifest.commit,hash:manifest.wasmSha256});
 for(let i=0;i<rawResults.length;i++) {
  const expected=originWorkerInputs[i],actual=rawResults[i];
  if(expected.recovery) {
   assert.equal(actual.error,undefined,`${expected.name}: raw worker recovery rejected`);
   assert.equal(actual.go,expected.go,`${expected.name}: raw worker recovery differs`);
  } else {
   assert.equal(actual.error?.trim(),expected.expected,`${expected.name}/${expected.ending}: raw worker diagnostic differs`);
   assert.equal(actual.go || '', '',`${expected.name}: raw worker leaked output`);
  }
 }
 await fs.writeFile(new URL('qa-artifacts/origin-diagnostics.json',import.meta.url),JSON.stringify(originResults,null,2)+'\n');
 // Current-only matrices share the real CLI/UI/Worker path, with separate evidence.
 for(const [label,matrix,results] of [['a04',a04Matrix,a04Results],['a01',a01Matrix,a01Results]]) {
 const matrixWorkerInputs=[];
 const saveMatrix=()=>fs.writeFile(new URL(`qa-artifacts/${label}-observations.json`,import.meta.url),JSON.stringify(results,null,2)+'\n');
 await saveMatrix();
 for(const check of matrix.checks) {
  const fixture=await fs.readFile(new URL(`checks/${check.name}.gox`,import.meta.url),'utf8');
  for(const ending of ['LF','CRLF']) {
   const source=ending==='LF'?fixture:fixture.replaceAll('\n','\r\n');
   const input=path.join(temp,'playground.gox');await fs.writeFile(input,source);
   let previous;
   for(let repeat=0;repeat<2;repeat++) {
    let go,diagnostic,exit=0;
    try {execFileSync(process.env.GOXIDE_BIN || fileURLToPath(new URL('../bin/goxide',import.meta.url)),['transpile','-o',input+'.go',input],{encoding:'utf8',cwd:fileURLToPath(new URL('..',import.meta.url)),stdio:['ignore','pipe','pipe']});go=await fs.readFile(input+'.go','utf8');}
    catch(e) {exit=e.status;go=String(e.stdout || '');diagnostic=String(e.stderr).trim().replaceAll(input,'playground.gox');}
    const observation={name:check.name,ending,repeat,expect:check.expect,exit,go,diagnostic};
    results.push(observation);await saveMatrix();
    if(check.expect==='success') {
     assert.equal(diagnostic,undefined,`${check.name}: current CLI rejected current matrix input`);
     assert.ok(go,`${check.name}: missing generated Go`);
    } else {
     assert.equal(check.expect,'frontend-reject');
     assert.ok(diagnostic,`${check.name}: current CLI accepted invalid current matrix input`);
     assert.match(diagnostic,new RegExp(check.message,'i'),`${check.name}: wrong current matrix rejection`);
     assert.equal(go,'',`${check.name}: rejected CLI input leaked Go`);
     if(check.point)assertOriginDiagnostic(diagnostic,check,source);
    }
    const expected=check.expect==='success'?go:diagnostic;
    if(repeat)assert.equal(expected,previous,`${check.name}/${ending}: nondeterministic output`);
    previous=expected;
    if(ending==='LF') {
     await writeSource(page,source);await page.click('#compile');
     if(check.expect==='success') {
      await finished();
     } else {
      await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
     }
     observation.ui={go:await readEditor(page,'生成结果'),diagnostic:(await page.textContent('#diagnostic')).trim(),copyDisabled:await view(page,'生成结果').getByRole('button',{name:'复制文件',exact:true}).isDisabled()};
     await saveMatrix();
     if(check.expect==='success')assert.equal(observation.ui.go,go,`${check.name}: current matrix UI Go differs`);
     else {
      assert.equal(observation.ui.diagnostic,diagnostic,`${check.name}: current matrix UI diagnostic differs`);
      assert.equal(observation.ui.go,'');assert.equal(observation.ui.copyDisabled,true);
     }
     await writeSource(page,manifest.examples[0].source);await finished();
     observation.uiRecoveryGo=await readEditor(page,'生成结果');await saveMatrix();
     assert.equal(observation.uiRecoveryGo,manifest.examples[0].go,`${check.name}: current matrix UI recovery differs`);
    }
    matrixWorkerInputs.push({source,go,error:diagnostic,name:check.name,ending,repeat});
    matrixWorkerInputs.push({source:manifest.examples[0].source,go:manifest.examples[0].go,name:check.name,ending,repeat,recovery:true});
   }
  }
 }
 const matrixRawResults=await recordWorkerObservations(
  (collect,args)=>page.evaluate(collect,args),
  {inputs:matrixWorkerInputs,commit:manifest.commit,hash:manifest.wasmSha256},
  batch=>fs.writeFile(new URL(`qa-artifacts/${label}-worker-observations.json`,import.meta.url),JSON.stringify(batch,null,2)+'\n')
 );
 assert.equal(matrixRawResults.length,matrixWorkerInputs.length);
 for(let i=0;i<matrixRawResults.length;i++) {
  const expected=matrixWorkerInputs[i],actual=matrixRawResults[i];
  if(expected.error) {
   assert.equal(actual.error?.trim(),expected.error,`${expected.name}/${expected.ending}: current matrix worker diagnostic differs`);
   assert.equal(actual.go || '','',`${expected.name}: current matrix worker leaked output`);
  } else {
   assert.equal(actual.error,undefined,`${expected.name}: current matrix worker success/recovery rejected`);
   assert.equal(actual.go,expected.go,`${expected.name}/${expected.ending}: current matrix worker Go differs`);
  }
 }
 await saveMatrix();
 }
} finally {await fs.rm(temp,{recursive:true,force:true});}
await page.selectOption('#examples',manifest.examples[0].id);await finished();
await writeSource(page,manifest.examples[0].source.replace('21','37'));await finished();
assert.match(await readEditor(page,'生成结果'),/37/);
await view(page,'生成结果').getByRole('button',{name:'复制文件',exact:true}).click();assert.equal(await page.evaluate(()=>navigator.clipboard.readText()),await readEditor(page,'生成结果'));
await writeSource(page,'package main\nfunc main( {');
await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
assert.equal(await readEditor(page,'生成结果'),'');assert.equal(await view(page,'生成结果').getByRole('button',{name:'复制文件',exact:true}).isDisabled(),true);
await page.click('#reset');await finished();
for(let n=0;n<6;n++)await writeSource(page,manifest.examples[0].source.replace('21',String(80+n)));
await finished();assert.match(await readEditor(page,'生成结果'),/85/);
await writeSource(page,'package main\nfunc main(){}\n'+' '.repeat(32769));
await page.waitForFunction(()=>document.querySelector('#diagnostic').textContent.includes('32 KiB'));
await page.click('#reset');await finished();
await writeSource(page,'package main\nimport "net/http"\nfunc main(){ _ = http.MethodGet }');
await page.waitForFunction(()=>document.querySelector('#status').dataset.state==='error');
assert.match(await page.textContent('#diagnostic'),/outside the bundled package set/);
await writeSource(page,'package main\n// <img src=x onerror=alert(1)>\nfunc main() {}');await finished();assert.equal(await page.locator('img').count(),0);
await view(page,'源码').getByRole('textbox').focus();await page.keyboard.press('Control+Enter');await finished();await page.keyboard.press('Tab');assert.ok(await page.evaluate(()=>document.activeElement!==document.body));
await page.click('#reset');await finished();
await page.locator('#playground h1').click();
await page.screenshot({path:new URL('qa-artifacts/desktop.png',import.meta.url).pathname,fullPage:true});
await page.setViewportSize({width:390,height:844});
assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),true);
await page.screenshot({path:new URL('qa-artifacts/mobile.png',import.meta.url).pathname,fullPage:true});
// Intercept only a QA worker to deterministically exercise hard timeout and cancel.
await page.route('**/worker.js*',r=>r.fulfill({contentType:'application/javascript',body:`postMessage({type:'ready',rootsSha256:'${manifest.dependencyContext.sha256}'}); onmessage=()=>{};`}));
await page.reload();await page.waitForFunction(()=>document.querySelector('#status')?.textContent==='正在转译…');
await page.waitForFunction(()=>document.querySelector('#diagnostic').textContent.includes('超过 10 秒'),{},{timeout:15000});
assert.equal(await readEditor(page,'生成结果'),'');
await page.click('#compile');await page.waitForFunction(()=>document.querySelector('#status').textContent==='正在转译…');await page.click('#stop');
assert.match(await page.textContent('#status'),/已中断/);
await page.unroute('**/worker.js*');await page.click('#reset');await finished();
assert.equal(await readEditor(page,'生成结果'),manifest.examples[0].go);
assert.deepEqual(errors,[]);
assert.equal(requests.every(u=>u.startsWith(base)),true,'external network request');
const originGroupResults=checks=>({fixtures:checks.length,cliObservations:checks.length*4,uiLfObservations:checks.length*2,rawWorkerObservations:checks.length*4,recoveryAfterEach:true,sourceFile:'playground.gox reused across distinct sources',scope:'UI LF plus non-UI real WASM worker LF/CRLF; no cross-file/package or effect/frame coverage claim'});
const builtinResults=names=>({positive:checkResults.filter(name=>names.includes(name)&&!name.startsWith('reject_')),frontendReject:Object.keys(frontendReject).filter(name=>names.includes(name)),nativeTypeReject:Object.keys(nativeTypeReject).filter(name=>names.includes(name)),nativeTypeRejectWasm:'transpilation parity only; final Go compilation is native'});
const currentGroupResults=(matrix,results)=>({fixtures:matrix.checks.length,positive:matrix.checks.filter(c=>c.expect==='success').map(c=>c.name),frontendReject:matrix.checks.filter(c=>c.expect==='frontend-reject').map(c=>c.name),cliObservations:results.length,uiLfObservations:results.filter(c=>c.ending==='LF').length,rawWorkerObservations:results.length,recoveryAfterEach:true,currentOnlyExamples:matrix.examples,scope:'Current compiler CLI/WASM parity and recovery only; no baseline support or browser Go execution claim'});
console.log(JSON.stringify({examples:manifest.examples.length,parity:'byte-for-byte',goResultChecks:checkResults.slice(0,9),diagnosticChecks:checkResults.slice(9).filter(name=>!appendChecks.includes(name)&&!copyChecks.includes(name)&&!allocationChecks.includes(name)&&!astPositiveChecks.includes(name)),appendChecks:builtinResults(appendChecks),copyChecks:builtinResults(copyChecks),allocationChecks:{...builtinResults(allocationChecks),nativeValidButUnsupported:Object.keys(nativeValidControls)},originChecks:originGroupResults(originDiagnosticChecks),astChecks:{...originGroupResults(astDiagnosticChecks),positive:checkResults.filter(name=>astPositiveChecks.includes(name))},a04Checks:currentGroupResults(a04Matrix,a04Results),a01Checks:{...currentGroupResults(a01Matrix,a01Results),diagnosticAnchorChecks:a01Matrix.checks.filter(c=>c.point).length},checks:['edit','error recovery','rapid edits','copy','32 KiB','unsupported import','XSS','keyboard','390px','hard timeout','interrupt','reset','no external requests'],errors},null,2));
await browser.close();
