// This function is serialized into the real page by Playwright. Keep it standalone.
export async function collectWorkerObservations({inputs,commit,websiteCommit,hash,rootsHash,macroHostURL,timeoutMs=30000}) {
 const observations=[];
 let worker,timer,macroHost,index=-1,raw;
 const response=()=>new Promise((resolve,reject)=>{
  timer=setTimeout(()=>reject(new Error('current matrix QA worker timeout')),timeoutMs);
  worker.onerror=event=>{clearTimeout(timer);reject(new Error(event.message));};
  worker.onmessage=({data})=>{
   raw=data;
   if(data.type==='macro'&&macroHost&&data.requestId===index){
    const host=macroHost,requestId=index;
    const reply=(result,transfer=[])=>{if(macroHost===host&&index===requestId)worker.postMessage({type:'macro-result',requestId,id:data.id,...result},transfer);};
    macroHost.run(data.hash,data.request,data.budgetMs).then(
     result=>reply(result,[result.response,result.stderr]),
     error=>reply({error:String(error.message??error)}),
    );
    return;
   }
   if(data.type==='macro-cancel'&&data.requestId===index){macroHost?.close('compilation canceled');return;}
   clearTimeout(timer);data.type==='fatal'?reject(new Error(data.error)):resolve(data);
  };
 });
 try {
  const createHost=macroHostURL?(await import(macroHostURL)).createProcMacroHost:null;
  worker=new Worker(`worker.js?compiler=${commit}&wasm=${hash}`);
  const ready=await response();
  if(ready.type!=='ready')throw new Error('current matrix QA worker not ready');
  if(ready.rootsSha256!==rootsHash)throw new Error('current matrix QA dependency roots mismatch');
  for(index=0;index<inputs.length;index++) {
   raw=undefined;
   macroHost=createHost?.(commit,ready.macroArtifacts??[],websiteCommit);
   const pending=response();
   await Promise.race([macroHost?.prepare(),pending]);
   worker.postMessage({type:'compile',id:index,source:inputs[index].source});
   const result=await pending;
   macroHost?.close('compilation finished');macroHost=null;
   if(result.type!=='result'||result.id!==index)throw new Error('current matrix QA worker result mismatch');
   observations.push({input:inputs[index],result});
  }
  return {complete:true,observations,failure:null};
 } catch(error) {
  return {complete:false,observations,failure:{stage:index<0?'ready':'compile',index,
   input:index<0?null:inputs[index],raw:raw??null,error:String(error.message??error)}};
 } finally {clearTimeout(timer);const host=macroHost;macroHost=null;host?.close();worker?.terminate();}
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
