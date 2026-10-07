// This function is serialized into the real page by Playwright. Keep it standalone.
export async function collectWorkerObservations({inputs,commit,hash,timeoutMs=30000}) {
 const observations=[];
 let worker,timer,index=-1,raw;
 const response=()=>new Promise((resolve,reject)=>{
  timer=setTimeout(()=>reject(new Error('current matrix QA worker timeout')),timeoutMs);
  worker.onerror=event=>{clearTimeout(timer);reject(new Error(event.message));};
  worker.onmessage=({data})=>{clearTimeout(timer);raw=data;data.type==='fatal'?reject(new Error(data.error)):resolve(data);};
 });
 try {
  worker=new Worker(`worker.js?compiler=${commit}&wasm=${hash}`);
  if((await response()).type!=='ready')throw new Error('current matrix QA worker not ready');
  for(index=0;index<inputs.length;index++) {
   raw=undefined;
   const pending=response();worker.postMessage({type:'compile',id:index,source:inputs[index].source});
   const result=await pending;
   if(result.type!=='result'||result.id!==index)throw new Error('current matrix QA worker result mismatch');
   observations.push({input:inputs[index],result});
  }
  return {complete:true,observations,failure:null};
 } catch(error) {
  return {complete:false,observations,failure:{stage:index<0?'ready':'compile',index,
   input:index<0?null:inputs[index],raw:raw??null,error:String(error.message??error)}};
 } finally {clearTimeout(timer);worker?.terminate();}
}

// Persist evidence before allowing any worker/protocol failure to fail the gate.
export async function recordWorkerObservations(evaluate,args,save) {
 let batch;
 try {batch=await evaluate(collectWorkerObservations,args);}
 catch(error) {batch={complete:false,observations:[],failure:{stage:'page-evaluate',index:null,input:null,raw:null,error:String(error.message??error)}};}
 await save(batch);
 if(!batch.complete||batch.failure)throw new Error('current matrix QA worker failed: '+JSON.stringify(batch.failure));
 return batch.observations.map(entry=>entry.result);
}
