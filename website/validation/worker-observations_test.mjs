import assert from 'node:assert/strict';
import {collectWorkerObservations,recordWorkerObservations} from './worker-observations.mjs';
const inputs=[{name:'first',source:'first'},{name:'second',source:'second'},{name:'recovery',source:'good'}];
const args={inputs,commit:'compiler',hash:'wasm',timeoutMs:10};
const original=globalThis.Worker;
let checks=0;
try {
 for(const failure of ['success','fatal','wrong-id','timeout','error','not-ready']) {
  let terminated=false;
  globalThis.Worker=class {
   constructor(){queueMicrotask(()=>this.onmessage({data:{type:failure==='not-ready'?'result':'ready'}}));}
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
   const ready=failure==='not-ready';
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
} finally {globalThis.Worker=original;}
console.log(`worker observation controls passed: ${checks}`);
