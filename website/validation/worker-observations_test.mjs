import assert from 'node:assert/strict';
import {collectWorkerObservations,recordWorkerObservations} from './worker-observations.mjs';
const inputs=[{name:'first',source:'first'},{name:'second',source:'second'},{name:'recovery',source:'good'}];
const args={inputs,commit:'compiler',websiteCommit:'site',hash:'wasm',rootsHash:'roots',timeoutMs:10};
const original=globalThis.Worker;
const originalHost=globalThis.qaCreateProcHost;
let checks=0;
try {
 for(const failure of ['success','fatal','wrong-id','timeout','error','not-ready','wrong-roots']) {
  let terminated=false;
  globalThis.Worker=class {
   constructor(){queueMicrotask(()=>this.onmessage({data:{type:failure==='not-ready'?'result':'ready',rootsSha256:failure==='wrong-roots'?'other':'roots'}}));}
   postMessage({id}) {
    if(id===1&&failure==='timeout')return;
    queueMicrotask(()=>{
     if(id===1&&failure==='error')return this.onerror({message:'synthetic worker crash'});
     this.onmessage({data:id===1&&failure==='fatal'?{type:'fatal',error:'synthetic fatal'}:
      {type:'result',id:id===1&&failure==='wrong-id'?99:id,go:'actual '+id}});
    });
   }
   terminate(){terminated=true;}
  };
  let saved;
  const execute=()=>recordWorkerObservations((fn,value)=>fn(value),args,async batch=>{saved=structuredClone(batch);});
  if(failure==='success') {
   const results=await execute();assert.equal(results.length,3);assert.equal(saved.complete,true);assert.equal(saved.failure,null);
  } else {
   await assert.rejects(execute,/current matrix QA worker failed/);
   assert.equal(saved.complete,false);assert.ok(saved.failure.error);
   const ready=failure==='not-ready'||failure==='wrong-roots';
   assert.equal(saved.observations.length,ready?0:1);
   assert.equal(saved.failure.index,ready?-1:1);
   assert.deepEqual(saved.failure.input,ready?null:inputs[1]);
   if(!ready)assert.deepEqual(saved.observations[0],{input:inputs[0],result:{type:'result',id:0,go:'actual 0'}});
   if(failure==='fatal')assert.equal(saved.failure.raw.type,'fatal');
   if(failure==='wrong-id')assert.equal(saved.failure.raw.id,99);
   if(failure==='timeout')assert.equal(saved.failure.raw,null);
  }
  assert.equal(terminated,true);checks++;
 }
 let saved;
 await assert.rejects(()=>recordWorkerObservations(async()=>{throw new Error('page closed');},args,async batch=>{saved=batch;}),/page closed/);
 assert.equal(saved.failure.stage,'page-evaluate');assert.equal(saved.complete,false);checks++;
 for(const fails of [false,true]) {
  let hosts=0,closed=0,terminated=false;
  globalThis.qaCreateProcHost=(commit,allowed,site)=>{
   assert.equal(commit,'compiler');assert.equal(site,'site');assert.deepEqual(allowed,['guest']);hosts++;
   let prepared=false;
   return {async prepare(){prepared=true;},close(){closed++;},async run(hash,request,budget){
    assert.equal(prepared,true);
    assert.equal(hash,'guest');assert.ok(request instanceof ArrayBuffer);assert.equal(budget,5000);
    if(fails)throw new Error('guest rejected');
    return {response:new ArrayBuffer(2),stderr:new ArrayBuffer(0)};
   }};
  };
  globalThis.Worker=class {
   constructor(){queueMicrotask(()=>this.onmessage({data:{type:'ready',rootsSha256:'roots',macroArtifacts:['guest']}}));}
   postMessage(message,transfer) {
    if(message.type==='compile')return queueMicrotask(()=>this.onmessage({data:{type:'macro',requestId:message.id,id:9,hash:'guest',request:new ArrayBuffer(1),budgetMs:5000}}));
    assert.equal(message.type,'macro-result');assert.equal(message.id,9);
    if(fails)assert.equal(message.error,'guest rejected');
    else assert.deepEqual(transfer,[message.response,message.stderr]);
    queueMicrotask(()=>this.onmessage({data:{type:'result',id:message.requestId,...(fails?{error:message.error}:{go:'expanded'})}}));
   }
   terminate(){terminated=true;}
  };
  const batch=await collectWorkerObservations({...args,macroHostURL:'data:text/javascript,export const createProcMacroHost=(...args)=>globalThis.qaCreateProcHost(...args);'});
  assert.equal(batch.complete,true);assert.equal(batch.observations.length,inputs.length);
  for(const {result} of batch.observations)assert.equal(fails?result.error:result.go,fails?'guest rejected':'expanded');
  assert.equal(hosts,inputs.length);assert.equal(closed,inputs.length);assert.equal(terminated,true);checks++;
 }
} finally {globalThis.Worker=original;if(originalHost===undefined)delete globalThis.qaCreateProcHost;else globalThis.qaCreateProcHost=originalHost;}
console.log(`worker observation controls passed: ${checks}`);
