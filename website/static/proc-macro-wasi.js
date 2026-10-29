'use strict';

// The browser counterpart of internal/procmacro/{wasm,wasi_profile,wasi}.go.
// Run only inside a dedicated Worker: its caller owns the real 5 s / 60 s
// result deadlines and requests Worker termination for guest or host JS loops.
// Actual CPU shutdown timing belongs to the browser engine.
// Request/response JSON and invocation-local references are admitted by the Go
// SDK on the calling side; this module transports bounded, uninterpreted bytes.
(() => {
  const PROFILE_ID = 'goxide-wasi-command-1';
  const MAX_WASM_BYTES = 64 << 20;
  const MAX_MEMORY_PAGES = 4096;
  const MAX_TABLE_ELEMENTS = 65536;
  const MAX_MESSAGE_BYTES = 8 << 20;
  const MAX_DIAGNOSTIC_BYTES = 64 << 10;
  const MAX_U64 = (1n << 64n) - 1n;
  const BADF = 8, FAULT = 21, INVAL = 28, NOTSUP = 58, NONBLOCK = 4;
  const encoder = new TextEncoder();
  const decoder = new TextDecoder('utf-8', {fatal: true, ignoreBOM: true});
  const stderrDecoder = new TextDecoder('utf-8', {ignoreBOM: true});
  const argv = encoder.encode('goxide-proc-macro\0');
  const header = new Uint8Array([0, 97, 115, 109, 1, 0, 0, 0]);

  // Signature codes: 1 = ()->(), 2 = (i32)->(), 3 = clock_time_get,
  // 4+n = n i32 parameters followed by one i32 result.
  const signatures = Object.freeze({
    args_sizes_get: 6, args_get: 6, environ_sizes_get: 6, environ_get: 6,
    fd_read: 8, fd_write: 8, fd_close: 5, fd_fdstat_get: 6,
    fd_fdstat_set_flags: 6, fd_prestat_get: 6, fd_prestat_dir_name: 7,
    clock_time_get: 3, random_get: 6, poll_oneoff: 8, sched_yield: 4, proc_exit: 2,
  });

  function bytes(value, label, limit) {
    let result;
    if (value instanceof Uint8Array) result = value;
    else if (value instanceof ArrayBuffer) result = new Uint8Array(value);
    else throw new Error(`procedural macro ${label} must be bytes`);
    if (!(result.buffer instanceof ArrayBuffer)) {
      throw new Error(`procedural macro ${label} cannot use shared memory`);
    }
    if (result.byteLength > limit) {
      throw new Error(`procedural macro ${label} exceeds ${limit} bytes`);
    }
    return result;
  }

  class Reader {
    constructor(data) { this.data = data; this.pos = 0; }
    remaining() { return this.data.length - this.pos; }
    take(size) {
      if (size > this.remaining()) throw new Error('truncated procedural macro WASM');
      const result = this.data.subarray(this.pos, this.pos + size);
      this.pos += size;
      return result;
    }
    byte() { return this.take(1)[0]; }
    u32() {
      let result = 0;
      for (let shift = 0; shift < 35; shift += 7) {
        const value = this.byte();
        if (shift === 28 && value > 15) break;
        result += (value & 127) * (2 ** shift);
        if ((value & 128) === 0) return result;
      }
      throw new Error('invalid uint32 in procedural macro WASM');
    }
    name() {
      const value = this.take(this.u32());
      try { return decoder.decode(value); }
      catch { throw new Error('invalid UTF-8 in procedural macro WASM name'); }
    }
    end() {
      if (this.remaining() !== 0) throw new Error('trailing bytes in procedural macro WASM section');
    }
  }

  // Avoid per-section arrays: a bounded artifact can contain many tiny custom
  // sections, and their JS object overhead must not multiply without a bound.
  class Writer {
    constructor(limit, initial = 1024) {
      this.limit = limit;
      this.data = new Uint8Array(Math.min(limit, initial));
      this.length = 0;
    }
    reserve(size) {
      const required = this.length + size;
      if (required > this.limit) throw new Error(`procedural macro bytes exceed ${this.limit}`);
      if (required > this.data.length) {
        const next = new Uint8Array(Math.min(this.limit, Math.max(required, this.data.length * 2)));
        next.set(this.data.subarray(0, this.length));
        this.data = next;
      }
    }
    write(data) {
      this.reserve(data.length);
      this.data.set(data, this.length);
      this.length += data.length;
    }
    byte(value) { this.reserve(1); this.data[this.length++] = value; }
    u32(value) {
      do {
        const low = value % 128;
        value = Math.floor(value / 128);
        this.byte(low | (value === 0 ? 0 : 128));
      } while (value !== 0);
    }
    name(value) { const text = encoder.encode(value); this.u32(text.length); this.write(text); }
    section(id, data) { this.byte(id); this.u32(data.length); this.write(data); }
    view() { return this.data.subarray(0, this.length); }
    copy() { return this.view().slice(); }
  }

  function validateImports(data) {
    const reader = new Reader(data);
    const count = reader.u32();
    for (let index = 0; index < count; index++) {
      const module = reader.name(), name = reader.name(), kind = reader.byte();
      if (kind !== 0 || module !== 'wasi_snapshot_preview1' || !Object.hasOwn(signatures, name)) {
        throw new Error(`procedural macro WASM import ${module}.${name} (kind ${kind}) is outside ${PROFILE_ID}`);
      }
      reader.u32();
    }
    reader.end();
  }

  function boundedLimits(data, table) {
    const reader = new Reader(data);
    const count = reader.u32();
    const name = table ? 'table' : 'memory';
    const cap = table ? MAX_TABLE_ELEMENTS : MAX_MEMORY_PAGES;
    if (count !== 1) throw new Error(`procedural macro WASM requires exactly one ${name}`);
    const output = new Writer(32, 32);
    output.u32(1);
    if (table) {
      if (reader.byte() !== 0x70) throw new Error('procedural macro WASM table must contain funcref');
      output.byte(0x70);
    }
    const flags = reader.u32();
    if (flags > 1) throw new Error(`procedural macro WASM ${name} requires nonshared 32-bit limits`);
    const initial = reader.u32();
    if (initial > cap) throw new Error(`procedural macro WASM ${name} initial ${initial} exceeds ${cap}`);
    let maximum = cap;
    if (flags === 1) {
      maximum = reader.u32();
      if (maximum < initial) throw new Error(`procedural macro WASM ${name} maximum is below initial`);
      maximum = Math.min(maximum, cap);
    }
    if (table) maximum = initial;
    reader.end();
    output.u32(1);
    output.u32(initial);
    output.u32(maximum);
    return output.view();
  }

  function prepareWASM(input) {
    const data = bytes(input, 'WASM', MAX_WASM_BYTES);
    if (data.length < header.length || !header.every((value, index) => data[index] === value)) {
      throw new Error('procedural macro artifact is not a WASM 1 module');
    }
    const output = new Writer(MAX_WASM_BYTES, data.length + 16);
    output.write(header);
    const reader = new Reader(data.subarray(header.length));
    const seen = new Set();
    let lastOrder = 0;
    while (reader.remaining() !== 0) {
      const id = reader.byte();
      let section = reader.take(reader.u32());
      if (id > 12) throw new Error(`unsupported procedural macro WASM section ${id}`);
      if (id !== 0) {
        const order = id === 12 ? 10 : id >= 10 ? id + 1 : id;
        if (seen.has(id) || order <= lastOrder) {
          throw new Error(`duplicate or out-of-order WASM section ${id}`);
        }
        seen.add(id);
        lastOrder = order;
      }
      if (id === 2) validateImports(section);
      if (id === 4 || id === 5) section = boundedLimits(section, id === 4);
      output.section(id, section);
    }
    if (!seen.has(4) || !seen.has(5)) {
      throw new Error('procedural macro WASM requires one internal table and memory');
    }
    return output.view();
  }

  function signature(params, results) {
    if (results.length === 0) {
      if (params.length === 0) return 1;
      if (params.length === 1 && params[0] === 0x7f) return 2;
    } else if (results.length === 1 && results[0] === 0x7f) {
      if (params.length === 3 && params[0] === 0x7f && params[1] === 0x7e && params[2] === 0x7f) return 3;
      if (params.length <= 4 && params.every(value => value === 0x7f)) return 4 + params.length;
    }
    return 0;
  }

  function functionTypes(data) {
    const reader = new Reader(data);
    const count = reader.u32();
    if (count > Math.floor(reader.remaining() / 3)) throw new Error('truncated procedural macro WASM types');
    const types = new Uint8Array(count);
    const values = new Set([0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x70, 0x6f]);
    for (let index = 0; index < count; index++) {
      if (reader.byte() !== 0x60) throw new Error('procedural macro WASM requires function types');
      const params = reader.take(reader.u32()), results = reader.take(reader.u32());
      if (!params.every(value => values.has(value)) || !results.every(value => values.has(value))) {
        throw new Error('unsupported procedural macro WASM value type');
      }
      types[index] = signature(params, results);
    }
    reader.end();
    return types;
  }

  function importedTypes(data, types) {
    const reader = new Reader(data);
    const count = reader.u32();
    if (count > Math.floor(reader.remaining() / 4)) throw new Error('truncated procedural macro WASM imports');
    const imports = new Uint32Array(count);
    for (let index = 0; index < count; index++) {
      const module = reader.name(), name = reader.name(), kind = reader.byte();
      const type = reader.u32();
      if (kind !== 0 || module !== 'wasi_snapshot_preview1' || !Object.hasOwn(signatures, name) || types[type] !== signatures[name]) {
        throw new Error(`procedural macro WASM import ${module}.${name} has an invalid WASI signature`);
      }
      imports[index] = type;
    }
    reader.end();
    return imports;
  }

  function exportedNames(data) {
    const reader = new Reader(data);
    const count = reader.u32(), offset = reader.pos;
    const names = new Set();
    let start = null;
    for (let index = 0; index < count; index++) {
      const name = reader.name(), kind = reader.byte(), item = reader.u32();
      if (names.has(name)) throw new Error(`duplicate procedural macro WASM export ${name}`);
      names.add(name);
      if (name === '_start') {
        if (kind !== 0) throw new Error('procedural macro WASM _start must be a function');
        start = item;
      }
    }
    reader.end();
    return {count, offset, names, start, data};
  }

  function executionModule(prepared) {
    let types = new Uint8Array(), imports = new Uint32Array();
    let functions = new Uint8Array([0]), exports = null, start = null;
    const reader = new Reader(prepared.subarray(header.length));
    while (reader.remaining() !== 0) {
      const id = reader.byte(), data = reader.take(reader.u32());
      if (id === 1) types = functionTypes(data);
      else if (id === 2) imports = importedTypes(data, types);
      else if (id === 3) functions = data;
      else if (id === 7) exports = exportedNames(data);
      else if (id === 8) { const section = new Reader(data); start = section.u32(); section.end(); }
    }
    const functionSignature = index => {
      if (index < imports.length) return types[imports[index]];
      index -= imports.length;
      const section = new Reader(functions);
      if (index >= section.u32()) return undefined;
      let type;
      for (let current = 0; current <= index; current++) type = section.u32();
      return types[type];
    };
    if (exports === null || exports.start === null || functionSignature(exports.start) !== 1) {
      throw new Error('procedural macro WASM requires _start with no parameters or results');
    }
    if (start !== null && functionSignature(start) !== 1) {
      throw new Error('procedural macro WASM start requires no parameters or results');
    }
    const privateName = base => {
      let name = base, suffix = 0;
      while (exports.names.has(name)) name = `${base}_${++suffix}`;
      exports.names.add(name);
      return name;
    };
    const memoryName = privateName('__goxide_proc_memory');
    const startName = start === null ? null : privateName('__goxide_proc_start');
    const replacements = new Writer(MAX_WASM_BYTES + 256, exports.data.length + 128);
    replacements.u32(exports.count + (start === null ? 1 : 2));
    replacements.write(exports.data.subarray(exports.offset));
    replacements.name(memoryName); replacements.byte(2); replacements.u32(0);
    if (start !== null) { replacements.name(startName); replacements.byte(0); replacements.u32(start); }
    // JS cannot access an internal memory until instantiation has returned.
    // Invoke the original start explicitly after binding the private memory
    // export; data/element initialization still precedes it. Both original
    // start signatures are checked above before removing the start section.
    const output = new Writer(MAX_WASM_BYTES + 256, prepared.length + 128);
    output.write(header);
    const sections = new Reader(prepared.subarray(header.length));
    while (sections.remaining() !== 0) {
      const id = sections.byte(), data = sections.take(sections.u32());
      if (id !== 8) output.section(id, id === 7 ? replacements.view() : data);
    }
    return {bytes: output.view(), memoryName, startName};
  }

  class WASIExit extends Error {
    constructor(code) { super(`procedural macro execution: exit_code(${code})`); this.exitCode = code; }
  }

  function addTime(current, delta) { return delta > MAX_U64 - current ? MAX_U64 : current + delta; }

  function createWASI(requestBytes) {
    const input = bytes(requestBytes, 'request', MAX_MESSAGE_BYTES).slice();
    const stdout = new Writer(MAX_MESSAGE_BYTES), stderr = new Writer(MAX_DIAGNOSTIC_BYTES);
    let memory = null, read = 0, clock = 0n, random = 0x6d2b79f5;
    let fatal = null, exit = null;
    const closed = [false, false, false], flags = [0, 0, 0];
    const range = (offset, count, size = 1) => {
      offset >>>= 0; count >>>= 0;
      const length = count * size;
      if (length > 0xffffffff || memory === null || offset + length > memory.buffer.byteLength) return null;
      return new Uint8Array(memory.buffer, offset, length);
    };
    const view = data => new DataView(data.buffer, data.byteOffset, data.byteLength);
    const write32 = (offset, value) => {
      const data = range(offset, 4);
      if (data === null) return false;
      view(data).setUint32(0, value, true);
      return true;
    };
    const fail = message => { fatal ??= new Error(message); throw fatal; };
    const functions = Object.create(null);
    const add = (name, function_) => {
      functions[name] = (...args) => {
        if (fatal !== null) throw fatal;
        if (exit !== null) throw exit;
        return function_(...args);
      };
    };
    add('args_sizes_get', (argc, size) => write32(argc, 1) && write32(size, argv.length) ? 0 : FAULT);
    add('args_get', (pointers, buffer) => {
      if (!write32(pointers, buffer >>> 0)) return FAULT;
      const data = range(buffer, argv.length);
      if (data === null) return FAULT;
      data.set(argv);
      return 0;
    });
    add('environ_sizes_get', (count, size) => write32(count, 0) && write32(size, 0) ? 0 : FAULT);
    add('environ_get', () => 0);
    add('fd_read', (fd, pointers, count, written) => {
      if ((fd >>> 0) !== 0 || closed[0]) return BADF;
      count >>>= 0;
      const descriptors = range(pointers, count, 8), result = range(written, 4);
      if (descriptors === null || result === null) return FAULT;
      const list = view(descriptors);
      let total = 0;
      for (let index = 0; index < count; index++) {
        const destination = range(list.getUint32(index * 8, true), list.getUint32(index * 8 + 4, true));
        if (destination === null) return FAULT;
        const length = Math.min(destination.length, input.length - read);
        destination.set(input.subarray(read, read + length));
        read += length; total += length;
        if (read === input.length) break;
      }
      view(result).setUint32(0, total, true);
      return 0;
    });
    add('fd_write', (fd, pointers, count, written) => {
      fd >>>= 0; count >>>= 0;
      if (fd < 1 || fd > 2 || closed[fd]) return BADF;
      const destination = fd === 1 ? stdout : stderr;
      const descriptors = range(pointers, count, 8), result = range(written, 4);
      if (descriptors === null || result === null) return FAULT;
      const list = view(descriptors);
      let total = 0;
      for (let index = 0; index < count; index++) {
        const length = list.getUint32(index * 8 + 4, true);
        if (range(list.getUint32(index * 8, true), length) === null) return FAULT;
        total += length;
        if (total > destination.limit - destination.length) {
          return fail(`procedural macro ${fd === 1 ? 'stdout' : 'stderr'} exceeds ${destination.limit} bytes`);
        }
      }
      for (let index = 0; index < count; index++) {
        destination.write(range(list.getUint32(index * 8, true), list.getUint32(index * 8 + 4, true)));
      }
      view(result).setUint32(0, total, true);
      return 0;
    });
    add('fd_close', fd => {
      fd >>>= 0;
      if (fd >= 3 || closed[fd]) return BADF;
      closed[fd] = true;
      return 0;
    });
    add('fd_fdstat_get', (fd, pointer) => {
      fd >>>= 0;
      if (fd >= 3 || closed[fd]) return BADF;
      const data = range(pointer, 24);
      if (data === null) return FAULT;
      data.fill(0); data[0] = 2;
      const stat = view(data);
      stat.setUint16(2, flags[fd], true);
      stat.setBigUint64(8, (1n << 3n) | (1n << 27n) | (fd === 0 ? 1n << 1n : 1n << 6n), true);
      return 0;
    });
    add('fd_fdstat_set_flags', (fd, value) => {
      fd >>>= 0; value >>>= 0;
      if (fd >= 3 || closed[fd]) return BADF;
      if ((value & ~NONBLOCK) !== 0) return NOTSUP;
      flags[fd] = value;
      return 0;
    });
    add('fd_prestat_get', () => BADF);
    add('fd_prestat_dir_name', () => BADF);
    add('clock_time_get', (id, _precision, pointer) => {
      if ((id >>> 0) > 1) return INVAL;
      const data = range(pointer, 8);
      if (data === null) return FAULT;
      view(data).setBigUint64(0, clock, true);
      clock = addTime(clock, 1_000_000n);
      return 0;
    });
    add('random_get', (pointer, length) => {
      const data = range(pointer, length);
      if (data === null) return FAULT;
      for (let index = 0; index < data.length; index++) {
        random ^= random << 13; random ^= random >>> 17; random ^= random << 5;
        random >>>= 0;
        data[index] = random & 255;
      }
      return 0;
    });
    const subscription = (data, offset, base) => {
      const type = data.getUint8(offset + 8);
      if (type === 0) {
        const id = data.getUint32(offset + 16, true), options = data.getUint16(offset + 40, true);
        if (id > 1 || (options & ~1) !== 0) return {deadline: 0n, errno: INVAL, immediate: true};
        if (options === 1) return {deadline: 0n, errno: NOTSUP, immediate: true};
        const timeout = data.getBigUint64(offset + 24, true);
        return {deadline: addTime(base, timeout), errno: 0, immediate: timeout === 0n};
      }
      const fd = data.getUint32(offset + 16, true);
      const invalid = fd >= 3 || closed[fd] || type === 1 && fd !== 0 || type === 2 && fd === 0;
      return {deadline: 0n, errno: invalid ? BADF : 0, immediate: true};
    };
    add('poll_oneoff', (inputPointer, outputPointer, count, resultPointer) => {
      inputPointer >>>= 0; outputPointer >>>= 0; count >>>= 0;
      if (count === 0) return INVAL;
      let inputData = range(inputPointer, count, 48);
      const output = range(outputPointer, count, 32), result = range(resultPointer, 4);
      if (inputData === null || output === null || result === null) return FAULT;
      if (inputPointer < outputPointer + output.length && outputPointer < inputPointer + inputData.length) {
        inputData = inputData.slice();
      }
      const subscriptions = view(inputData), base = clock;
      let earliest = MAX_U64, ready = false;
      for (let index = 0; index < count; index++) {
        if (subscriptions.getUint8(index * 48 + 8) > 2) return INVAL;
        const event = subscription(subscriptions, index * 48, base);
        ready ||= event.immediate;
        if (!event.immediate && event.deadline < earliest) earliest = event.deadline;
      }
      if (!ready) clock = earliest;
      let events = 0;
      for (let index = 0; index < count; index++) {
        const source = index * 48;
        const event = subscription(subscriptions, source, base);
        if (!event.immediate && event.deadline > clock) continue;
        const record = output.subarray(events * 32, (events + 1) * 32);
        record.fill(0);
        record.set(inputData.subarray(source, source + 8));
        const fields = view(record), type = subscriptions.getUint8(source + 8);
        fields.setUint16(8, event.errno, true); record[10] = type;
        if (event.errno === 0 && type === 1) fields.setBigUint64(16, BigInt(input.length - read), true);
        events++;
      }
      view(result).setUint32(0, events, true);
      return 0;
    });
    add('sched_yield', () => 0);
    add('proc_exit', code => { exit = new WASIExit(code >>> 0); throw exit; });
    return {
      imports: {wasi_snapshot_preview1: functions},
      setMemory(value) {
        if (!(value instanceof WebAssembly.Memory) || !(value.buffer instanceof ArrayBuffer) || value.buffer.byteLength > MAX_MEMORY_PAGES * 65536) {
          throw new Error('procedural macro instance has invalid memory');
        }
        memory = value;
      },
      finish() {
        if (fatal !== null) throw fatal;
        if (exit !== null && exit.exitCode !== 0) throw exit;
        return {response: stdout.copy(), stderr: stderr.copy()};
      },
      error(error) {
        const cause = fatal ?? error;
        const message = cause instanceof Error ? cause.message : String(cause);
        return new Error(stderr.length === 0 ? message : `${message}\nstderr: ${stderrDecoder.decode(stderr.view())}`);
      },
    };
  }

  class Host {
    #cache = new Map();
    #cacheBytes = 0;
    #active = false;
    #closed = false;

    close() { this.#closed = true; this.#cache.clear(); this.#cacheBytes = 0; }

    async run(wasm, request) {
      if (this.#closed) throw new Error('procedural macro host is closed');
      if (this.#active) throw new Error('procedural macro host already has an active invocation');
      this.#active = true;
      let state;
      try {
        // Snapshot request bytes before awaiting hashing or engine compilation.
        state = createWASI(request);
        const prepared = prepareWASM(wasm);
        const hash = new Uint8Array(await crypto.subtle.digest('SHA-256', prepared));
        const key = Array.from(hash, value => value.toString(16).padStart(2, '0')).join('');
        if (this.#closed) throw new Error('procedural macro host is closed');
        let cached = this.#cache.get(key);
        if (cached === undefined) {
          const adapted = executionModule(prepared);
          while (this.#cacheBytes + prepared.length > MAX_WASM_BYTES) {
            const oldest = this.#cache.keys().next().value;
            this.#cacheBytes -= this.#cache.get(oldest).bytes;
            this.#cache.delete(oldest);
          }
          const module = await WebAssembly.compile(adapted.bytes);
          if (this.#closed) throw new Error('procedural macro host is closed');
          cached = {module, memoryName: adapted.memoryName, startName: adapted.startName, bytes: prepared.length};
          this.#cache.set(key, cached);
          this.#cacheBytes += cached.bytes;
        }
        const instance = new WebAssembly.Instance(cached.module, state.imports);
        state.setMemory(instance.exports[cached.memoryName]);
        try {
          if (cached.startName !== null) instance.exports[cached.startName]();
          instance.exports._start();
        } catch (error) {
          if (!(error instanceof WASIExit) || error.exitCode !== 0) throw error;
        }
        return state.finish();
      } catch (error) {
        throw state === undefined ? error : state.error(error);
      } finally {
        this.#active = false;
      }
    }
  }

  globalThis.GoxideProcMacroWASI = Object.freeze({
    PROFILE_ID, MAX_WASM_BYTES, MAX_MEMORY_PAGES, MAX_TABLE_ELEMENTS,
    MAX_MESSAGE_BYTES, MAX_DIAGNOSTIC_BYTES, prepareWASM, createWASI, Host,
  });
})();
