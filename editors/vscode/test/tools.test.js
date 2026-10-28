"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const development = require("../development");
const { Gopls, goplsVersion, installOptions } = require("../tools");

const binaryName = process.platform === "win32" ? "gopls.exe" : "gopls";

class CancellationTokenSource {
  listeners = new Set();
  token = {
    isCancellationRequested: false,
    onCancellationRequested: listener => { this.listeners.add(listener); return { dispose: () => this.listeners.delete(listener) }; },
  };
  cancel() {
    this.token.isCancellationRequested = true;
    for (const listener of [...this.listeners]) listener();
  }
  dispose() { this.listeners.clear(); }
}

async function fixture(t) {
  const root = await fs.mkdtemp(path.join(os.tmpdir(), "goxide-tools-"));
  const storage = path.join(root, "extension storage");
  const pathKey = Object.keys(process.env).find(key => key.toUpperCase() === "PATH") || "PATH";
  const previousPath = process.env[pathKey];
  process.env[pathKey] = path.join(root, "bin");
  await fs.mkdir(process.env[pathKey]);
  t.after(async () => {
    if (previousPath === undefined) delete process.env[pathKey];
    else process.env[pathKey] = previousPath;
    await fs.rm(root, { recursive: true, force: true });
  });
  const warnings = [], logs = [], progress = [];
  const token = new CancellationTokenSource();
  const vscode = {
    CancellationTokenSource, ProgressLocation: { Notification: 15 },
    window: {
      showWarningMessage: async message => { warnings.push(message); },
      withProgress: (options, task) => { progress.push(options); return task({}, token.token); },
    },
  };
  const tools = new Gopls(vscode, { scheme: "file", fsPath: storage }, { appendLine: line => logs.push(line) });
  const directory = path.join(storage, "tools", `gopls-${goplsVersion}`, `${process.platform}-${process.arch}`);
  const installed = path.join(directory, binaryName);
  const writeBinary = async filename => {
    await fs.mkdir(path.dirname(filename), { recursive: true });
    await fs.writeFile(filename, "installed gopls", { mode: 0o755 });
  };
  const install = async options => {
    if (options.args[0] === "env") return {
      code: 0, cancelled: false, stderr: "",
      stdout: JSON.stringify({ GOHOSTOS: process.platform === "win32" ? "windows" : process.platform, GOHOSTARCH: process.arch === "x64" ? "amd64" : process.arch }),
    };
    await writeBinary(path.join(options.env.GOBIN, binaryName));
    return { code: 0, cancelled: false, stdout: "", stderr: "" };
  };
  t.after(() => tools.dispose());
  return { root, storage, bin: process.env[pathKey], tools, vscode, warnings, logs, progress, token, directory, installed, writeBinary, install };
}

test("installation selects the extension host on all distributed platforms", () => {
  for (const [platform, arch, goos, goarch] of [
    ["win32", "x64", "windows", "amd64"], ["win32", "arm64", "windows", "arm64"],
    ["darwin", "arm64", "darwin", "arm64"], ["linux", "x64", "linux", "amd64"], ["linux", "arm64", "linux", "arm64"],
  ]) {
    const directory = platform === "win32" ? "C:\\User Tools\\gopls" : "/extension tools/gopls";
    const inherited = { GOOS: "js", GOARCH: "wasm", GOWORK: "/project/go.work", GOFLAGS: "-mod=vendor -overlay=project.json", gobin: "/global/bin", GOPROXY: "https://proxy.example", GOTOOLCHAIN: "local" };
    const options = installOptions(directory, platform, arch, inherited);
    assert.deepEqual([options.command, ...options.args], ["go", "install", "golang.org/x/tools/gopls@v0.24.0"]);
    assert.equal(options.cwd, directory);
    assert.equal(options.env.GOBIN, directory);
    assert.equal(options.env.gobin, undefined);
    assert.equal(options.env.GOWORK, "off");
    assert.equal(options.env.GOFLAGS, "");
    assert.equal(options.env.GO111MODULE, "on");
    assert.equal(options.env.GOOS, goos);
    assert.equal(options.env.GOARCH, goarch);
    assert.equal(options.env.CGO_ENABLED, "0");
    assert.equal(options.env.GOPROXY, inherited.GOPROXY);
    assert.equal(options.env.GOTOOLCHAIN, "local");
    assert.equal(inherited.GOARCH, "wasm");
    assert.equal(inherited.gobin, "/global/bin");
  }
});

test("custom commands, explicit off, and Go extension conflict never install", async t => {
  const f = await fixture(t);
  const install = t.mock.method(development, "execute", () => assert.fail("unexpected installation"));
  for (const configured of ["off", "custom-gopls", "../tools/gopls", "C:\\tools\\gopls.exe"]) {
    assert.equal(await f.tools.prepare(configured, false, f.root), configured);
  }
  assert.equal(await f.tools.prepare("gopls", true, f.root), "off");
  assert.equal(install.mock.callCount(), 0);
  assert.equal(f.progress.length, 0);
});

test("PATH takes precedence over the private cached tool", async t => {
  const f = await fixture(t);
  const onPath = path.join(f.bin, binaryName);
  await f.writeBinary(onPath);
  await f.writeBinary(f.installed);
  assert.equal(await f.tools.prepare("gopls", false, f.root), onPath);
  assert.equal(f.progress.length, 0);
});

test("relative PATH entries are resolved for each project's server working directory", async t => {
  const f = await fixture(t);
  const other = path.join(f.root, "other");
  const first = path.join(f.root, "bin", binaryName), second = path.join(other, "bin", binaryName);
  await f.writeBinary(first);
  await f.writeBinary(second);
  process.env[Object.keys(process.env).find(key => key.toUpperCase() === "PATH")] = "bin";
  assert.equal(await f.tools.prepare("gopls", false, f.root), first);
  assert.equal(await f.tools.prepare("gopls", false, other), second);
  assert.equal(f.progress.length, 0);
});

test("cached pinned gopls works without go or another download", async t => {
  const f = await fixture(t);
  await f.writeBinary(f.installed);
  assert.equal(await f.tools.prepare("gopls", false, f.root), f.installed);
  assert.equal(f.progress.length, 0);
});

test("concurrent preparation installs once to private staging and publishes only the finished binary", async t => {
  const f = await fixture(t);
  const installer = t.mock.method(development, "execute", async options => {
    if (options.args[0] === "env") {
      assert.deepEqual(options.args, ["env", "-json", "GOHOSTOS", "GOHOSTARCH"]);
      return f.install(options);
    }
    assert.deepEqual(options.args, ["install", "golang.org/x/tools/gopls@v0.24.0"]);
    assert.equal(options.command, "go");
    assert.equal(options.cwd, options.env.GOBIN);
    assert.equal(path.dirname(options.env.GOBIN), f.directory);
    assert.match(path.basename(options.env.GOBIN), /^\.install-/);
    await assert.rejects(fs.stat(f.installed), { code: "ENOENT" });
    return f.install(options);
  });
  const first = f.tools.prepare("gopls", false, f.root);
  const second = f.tools.prepare("gopls", false, f.root);
  assert.deepEqual(await Promise.all([first, second]), [f.installed, f.installed]);
  assert.equal(installer.mock.callCount(), 2);
  assert.deepEqual(await fs.readdir(f.directory), [binaryName]);
  assert.equal(await fs.readFile(f.installed, "utf8"), "installed gopls");
  assert.equal(f.progress.length, 1);
  assert.equal(f.progress[0].cancellable, true);
  assert.match(f.progress[0].title, /v0\.24\.0/);
  f.tools.reset();
  assert.equal(await f.tools.prepare("gopls", false, f.root), f.installed);
  assert.equal(installer.mock.callCount(), 2);
});

test("installation uses the Go toolchain host even when Node has a different architecture", async t => {
  const f = await fixture(t);
  const goarch = process.arch === "arm64" ? "amd64" : "arm64";
  t.mock.method(development, "execute", async options => {
    if (options.args[0] === "env") return { code: 0, cancelled: false, stderr: "", stdout: JSON.stringify({ GOHOSTOS: "windows", GOHOSTARCH: goarch }) };
    assert.equal(options.env.GOOS, "windows");
    assert.equal(options.env.GOARCH, goarch);
    return f.install(options);
  });
  assert.equal(await f.tools.prepare("gopls", false, f.root), f.installed);
});

test("failed downloads clean staging, preserve Go errors, and can retry after restart", async t => {
  const f = await fixture(t);
  let attempts = 0;
  t.mock.method(development, "execute", async options => {
    if (options.args[0] === "env") return f.install(options);
    if (++attempts > 1) return f.install(options);
    await fs.writeFile(path.join(options.env.GOBIN, binaryName), "incomplete");
    return { code: 1, cancelled: false, stdout: "", stderr: "proxy connection refused" };
  });
  assert.equal(await f.tools.prepare("gopls", false, f.root), "off");
  assert.deepEqual(await fs.readdir(f.directory), []);
  assert.match(f.warnings[0], /go install gopls failed \(exit 1\)/);
  assert.ok(f.logs.includes("proxy connection refused"));
  assert.equal(await f.tools.prepare("gopls", false, f.root), "off");
  assert.equal(attempts, 1);
  f.tools.reset();
  assert.equal(await f.tools.prepare("gopls", false, f.root), f.installed);
  assert.equal(attempts, 2);
});

test("missing go reports the actual command and leaves preparation retryable", async t => {
  const f = await fixture(t);
  assert.equal(await f.tools.prepare("gopls", false, f.root), "off");
  assert.match(f.warnings[0], /Executable was not found: go\./);
  assert.deepEqual(await fs.readdir(f.directory), []);
  t.mock.method(development, "execute", options => f.install(options));
  f.tools.reset();
  assert.equal(await f.tools.prepare("gopls", false, f.root), f.installed);
});

test("a successful command without its executable is rejected and can retry", async t => {
  const f = await fixture(t);
  const installer = t.mock.method(development, "execute", async options => options.args[0] === "env" ? f.install(options) : { code: 0, cancelled: false, stdout: "", stderr: "" });
  assert.equal(await f.tools.prepare("gopls", false, f.root), "off");
  assert.match(f.warnings[0], /did not produce an executable/);
  assert.deepEqual(await fs.readdir(f.directory), []);
  installer.mock.mockImplementation(options => f.install(options));
  f.tools.reset();
  assert.equal(await f.tools.prepare("gopls", false, f.root), f.installed);
});

test("cancellation stops the install process and permits retry", async t => {
  const f = await fixture(t);
  const execute = development.execute;
  let started;
  const ready = new Promise(resolve => { started = resolve; });
  const installer = t.mock.method(development, "execute", (options, token) => options.args[0] === "env" ? f.install(options) : execute({
    ...options, command: process.execPath,
    args: ["-e", "process.stdout.write('{}\\n'); setInterval(() => {}, 1000)"],
  }, token, () => started()));
  const pending = f.tools.prepare("gopls", false, f.root);
  await ready;
  f.token.cancel();
  assert.equal(await pending, "off");
  assert.match(f.warnings[0], /installation was cancelled/);
  assert.deepEqual(await fs.readdir(f.directory), []);
  installer.mock.mockImplementation(options => f.install(options));
  f.token.token.isCancellationRequested = false;
  f.tools.reset();
  assert.equal(await f.tools.prepare("gopls", false, f.root), f.installed);
});

test("installation environment and paths reach a real child unchanged", async t => {
  const f = await fixture(t);
  const options = installOptions(f.root, process.platform, process.arch, { ...process.env, GOOS: "js", GOARCH: "wasm" });
  const result = await development.execute({
    ...options, command: process.execPath,
    args: ["-e", "console.log(JSON.stringify({cwd: process.cwd(), env: process.env, argv: process.argv.slice(1)}))", ...options.args],
  });
  assert.equal(result.code, 0);
  const actual = JSON.parse(result.stdout);
  assert.deepEqual(actual.argv, ["install", "golang.org/x/tools/gopls@v0.24.0"]);
  assert.equal(await fs.realpath(actual.cwd), await fs.realpath(f.root));
  for (const key of ["GOBIN", "GOWORK", "GOOS", "GOARCH", "GOFLAGS", "CGO_ENABLED"]) assert.equal(actual.env[key], options.env[key]);
});
