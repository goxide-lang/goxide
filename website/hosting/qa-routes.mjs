import { chromium } from 'playwright';
import assert from 'node:assert/strict';
const browser=await chromium.launch({executablePath:process.env.CHROMIUM||'/usr/bin/chromium',args:['--no-sandbox']});
try {
 for(const [base,headers] of [['http://127.0.0.1:8084/',true],['http://127.0.0.1:8085/goxide/',false]]) {
  const context=await browser.newContext();const page=await context.newPage();
  const response=await page.goto(base.replace(/\/$/,''));assert.equal(response.status(),200);assert.equal(page.url(),base);
  for(const asset of ['worker.js','wasm_exec.js','compiler.wasm','examples.json']) {
   const r=await context.request.get(base+asset);assert.equal(r.status(),200);
   if(asset.endsWith('.wasm'))assert.match(r.headers()['content-type'],/application\/wasm/);
   if(headers)assert.match(r.headers()['content-security-policy'],/wasm-unsafe-eval/);
  }
  for(const path of ['missing','deep/unknown','private/rfcs.md','.git/config','compile']) {
   const r=await page.goto(base+path);assert.equal(r.status(),404);assert.match(await page.textContent('h1'),/404/);
   assert.equal(await page.locator('#source').count(),0);
  }
  await context.close();console.log(JSON.stringify({base,rootRedirect:true,resources:true,real404:true,cloudflareHeadersSimulated:headers}));
 }
} finally {await browser.close();}
