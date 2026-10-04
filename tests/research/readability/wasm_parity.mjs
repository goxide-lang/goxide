// Local-only native/WASM differential check; run website/build.py first.
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import crypto from 'node:crypto';
import assert from 'node:assert/strict';
import {fileURLToPath, pathToFileURL} from 'node:url';
import {execFileSync} from 'node:child_process';
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../..');
const dist = path.join(root, 'website/dist');
globalThis.crypto ??= crypto.webcrypto;
let ready;
const started = new Promise(resolve => {ready = resolve;});
globalThis.postMessage = message => {if (message.type === 'ready') ready();};
await import(pathToFileURL(path.join(dist, 'wasm_exec.js')).href);
const go = new globalThis.Go();
const binary = await WebAssembly.instantiate(fs.readFileSync(path.join(dist, 'compiler.wasm')), go.importObject);
void go.run(binary.instance);
await started;
const manifest = JSON.parse(fs.readFileSync(path.join(dist, 'examples.json'), 'utf8'));
for (const example of manifest.examples) {
  const actual = globalThis.hgoCompile(example.source);
  assert.equal(actual.error, undefined, example.id);
  assert.equal(actual.go, example.go, `${example.id}: native/WASM output differs`);
  console.log(`PASS website ${example.id}`);
}
const scratch = fs.mkdtempSync(path.join(os.tmpdir(), 'readability-wasm-'));
try {
  for (const name of fs.readdirSync(path.join(root, 'website/checks')).filter(n => n.endsWith('.hgo')).sort()) {
    const source = fs.readFileSync(path.join(root, 'website/checks', name), 'utf8');
    const input = path.join(scratch, 'playground.hgo');
    fs.writeFileSync(input, source);
    let output, error;
    try {output = execFileSync(path.join(root, 'bin/hgo'), ['transpile', input], {encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe']});}
    catch (e) {error = String(e.stderr).trim().replaceAll(input, 'playground.hgo');}
    const actual = globalThis.hgoCompile(source);
    if (error !== undefined) assert.equal(actual.error, error, `${name}: diagnostic mismatch`);
    else {assert.equal(actual.error, undefined, name); assert.equal(actual.go, output, `${name}: output mismatch`);}
    console.log(`PASS adapter ${name}`);
  }
} finally {fs.rmSync(scratch, {recursive: true, force: true});}
process.exit(0);
