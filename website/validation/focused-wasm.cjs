'use strict';
// Execute the same js/wasm frontend used by worker.js; no browser UI claim.
const fs = require('node:fs');
const assert = require('node:assert/strict');
globalThis.fs = fs;
globalThis.require = require;
globalThis.crypto ??= require('node:crypto').webcrypto;
globalThis.performance ??= require('node:perf_hooks').performance;
const [wasm, runtime, inputsFile, outputsFile, rootsHash] = process.argv.slice(2);
assert.equal(process.env.GOMAXPROCS, '1', 'only this Node process must use GOMAXPROCS=1');
require(runtime);
const go = new Go();
go.env = {...process.env};
let readyResolve;
const ready = new Promise(resolve => {readyResolve = resolve;});
globalThis.postMessage = message => {
  assert.equal(message.type, 'ready');
  assert.match(rootsHash, /^[a-f0-9]{64}$/);
  assert.equal(message.rootsSha256, rootsHash, 'WASM uses another project dependency context');
  readyResolve();
};
(async () => {
  const {instance} = await WebAssembly.instantiate(fs.readFileSync(wasm), go.importObject);
  go.run(instance).catch(error => {console.error(error); process.exit(1);});
  await Promise.race([ready, new Promise((_, reject) => setTimeout(() => reject(Error('WASM readiness timeout')), 30000))]);
  const outputs = [];
  for (const input of JSON.parse(fs.readFileSync(inputsFile, 'utf8'))) {
    const result = await globalThis.goxideCompile(input.source);
    assert.equal(typeof result, 'object');
    assert.ok(('go' in result) !== ('error' in result));
    outputs.push({id: input.id, ...result});
    fs.writeFileSync(outputsFile, JSON.stringify({complete: false, outputs}, null, 2)+'\n');
  }
  fs.writeFileSync(outputsFile, JSON.stringify({complete: true, outputs}, null, 2)+'\n');
  process.exit(0);
})().catch(error => {console.error(error); process.exit(1);});
