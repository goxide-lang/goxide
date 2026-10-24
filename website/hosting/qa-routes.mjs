import {chromium} from 'playwright';
import assert from 'node:assert/strict';
const base=process.env.QA_URL||'http://127.0.0.1:8084';
const browser=await chromium.launch({executablePath:process.env.CHROMIUM||'/usr/bin/chromium',chromiumSandbox:true});
try{
 const context=await browser.newContext();const page=await context.newPage();
 const assetResponse=await context.request.get(base+'/frontend-assets.json');assert.equal(assetResponse.status(),200);const assets=(await assetResponse.json()).assets;
 const manifestResponse=await context.request.get(base+'/examples.json');assert.equal(manifestResponse.status(),200);const manifest=await manifestResponse.json();
 for(const asset of ['learn/?stage=gox','learn/?stage=procedural','docs/?page=manifest','docs/?page=procedural-macros','design/?page=grammar','tutorial-go.zip','tutorial-mixed.zip','tutorial-gox.zip','tutorial-features.zip','tutorial-procedural.zip','rfc.html','playground.html','local.html','worker.js','proc-macro-worker.js','proc-macro-wasi.js','wasm_exec.js','compiler.wasm','examples.json','THIRD-PARTY-LICENSES.txt','project-reexports.zip',...Object.keys(assets),...manifest.macroBundle.artifacts.map(artifact=>artifact.path)]){const response=await context.request.get(base+'/'+asset);assert.equal(response.status(),200,asset);if(asset.endsWith('.wasm'))assert.match(response.headers()['content-type'],/application\/wasm/);}
 for(const path of ['/missing','/deep/unknown','/private/rfcs.md','/.git/config','/compile']){assert.equal((await page.goto(base+path)).status(),404);assert.match(await page.textContent('h1'),/404/);assert.equal(await page.locator('#root').count(),0);}
 console.log(JSON.stringify({base,rootSite:true,assets:Object.keys(assets).length,real404:true}));
}finally{await browser.close();}
