'use strict';

// This Worker never fetches macro artifacts. The page supplies bytes from
// the fixed, integrity-checked artifact snapshot and owns the result deadline.
// On timeout/cancellation the caller must terminate this Worker, not send a
// message to a guest that may have blocked its event loop, then create a new one.
const profileURL = new URL('proc-macro-wasi.js', self.location.href);
profileURL.search = self.location.search;
importScripts(profileURL.href);
const macroHost = new self.GoxideProcMacroWASI.Host();

self.onmessage = async ({data}) => {
  if (data === null || typeof data !== 'object' || data.type !== 'run') return;
  const id = data.id;
  if (typeof id !== 'string' && !Number.isSafeInteger(id)) {
    self.postMessage({type: 'result', id: null, error: 'procedural macro request requires an id'});
    return;
  }
  try {
    const {response, stderr} = await macroHost.run(data.wasm, data.request);
    self.postMessage({type: 'result', id, response: response.buffer, stderr: stderr.buffer}, [response.buffer, stderr.buffer]);
  } catch (error) {
    self.postMessage({type: 'result', id, error: error instanceof Error ? error.message : String(error)});
  }
};
self.postMessage({type: 'ready'});
