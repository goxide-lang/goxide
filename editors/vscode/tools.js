"use strict";

const fs = require("node:fs/promises");
const { constants } = require("node:fs");
const path = require("node:path");
const development = require("./development");

const goplsVersion = "v0.24.0";

function installOptions(directory, platform = process.platform, arch = process.arch, environment = process.env) {
  const env = { ...environment };
  const overrides = {
    GOBIN: directory, GOWORK: "off", GO111MODULE: "on", GOFLAGS: "", CGO_ENABLED: "0",
    GOOS: platform === "win32" ? "windows" : platform,
    GOARCH: arch === "x64" ? "amd64" : arch,
    GOAMD64: "v1", GOARM64: "v8.0",
  };
  // Windows environment names are case-insensitive; do not leave two values.
  for (const key of Object.keys(env)) if (Object.hasOwn(overrides, key.toUpperCase())) delete env[key];
  return {
    command: "go", args: ["install", `golang.org/x/tools/gopls@${goplsVersion}`],
    cwd: directory, env: Object.assign(env, overrides),
  };
}

async function executable(filename) {
  try {
    if (!(await fs.stat(filename)).isFile()) return false;
    await fs.access(filename, process.platform === "win32" ? constants.F_OK : constants.X_OK);
    return true;
  } catch (error) {
    if (["ENOENT", "ENOTDIR", "EACCES"].includes(error.code)) return false;
    throw error;
  }
}

async function fromPath(cwd) {
  const name = process.platform === "win32" ? "gopls.exe" : "gopls";
  const key = Object.keys(process.env).find(key => key.toUpperCase() === "PATH");
  for (const directory of (process.env[key] || "").split(path.delimiter)) {
    if (!directory) continue;
    const filename = path.resolve(cwd, directory, name);
    if (await executable(filename)) return filename;
  }
}

class Gopls {
  constructor(vscode, storageURI, output) {
    this.vscode = vscode;
    this.storageURI = storageURI;
    this.output = output;
  }

  async prepare(configured, goConflict, cwd) {
    if (goConflict || configured === "off") return "off";
    if (configured !== "gopls") return configured;
    const existing = await fromPath(cwd);
    if (existing) return existing;
    // Share one preparation across projects, including its degraded result.
    // Restart clears the result so a failed or cancelled install can be retried.
    if (!this.pending) this.pending = this.prepareDefault().catch(error => {
      this.output.appendLine(String(error.stack || error));
      void this.vscode.window.showWarningMessage(`Goxide: ${error.message} Go-backed services are off; run Goxide: Restart Language Server to retry, or configure goxide.gopls.`);
      return "off";
    });
    return this.pending;
  }

  reset() { this.pending = undefined; }
  dispose() { this.disposed = true; this.cancellation?.cancel(); }

  async prepareDefault() {
    if (this.storageURI?.scheme !== "file") throw new Error("The extension host has no local tool storage.");
    const directory = path.join(this.storageURI.fsPath, "tools", `gopls-${goplsVersion}`, `${process.platform}-${process.arch}`);
    const name = process.platform === "win32" ? "gopls.exe" : "gopls";
    const installed = path.join(directory, name);
    if (await executable(installed)) return installed;
    return this.vscode.window.withProgress({
      location: this.vscode.ProgressLocation.Notification,
      title: `Goxide: Installing gopls ${goplsVersion}`, cancellable: true,
    }, async (_progress, token) => {
      const cancellation = new this.vscode.CancellationTokenSource();
      this.cancellation = cancellation;
      const listener = token.onCancellationRequested(() => cancellation.cancel());
      if (this.disposed || token.isCancellationRequested) cancellation.cancel();
      let staging;
      try {
        await fs.mkdir(directory, { recursive: true });
        staging = await fs.mkdtemp(path.join(directory, ".install-"));
        const host = await development.execute({ ...installOptions(staging), args: ["env", "-json", "GOHOSTOS", "GOHOSTARCH"] }, cancellation.token);
        if (host.stderr) this.output.appendLine(host.stderr);
        if (host.cancelled || cancellation.token.isCancellationRequested) throw new Error("gopls installation was cancelled.");
        if (host.code !== 0) throw new Error(`go env failed (exit ${host.code}). See Output → Goxide for the Go error.`);
        const { GOHOSTOS, GOHOSTARCH } = JSON.parse(host.stdout);
        if (!GOHOSTOS || !GOHOSTARCH) throw new Error("go env did not report the Go host platform.");
        // Go may run through emulation on a different architecture than Node.
        // GOBIN installs must use Go's own host, not the project's or Node's target.
        const options = installOptions(staging, GOHOSTOS, GOHOSTARCH);
        this.output.appendLine(`Installing gopls: ${options.command} ${options.args.join(" ")} (GOBIN=${staging})`);
        const result = await development.execute(options, cancellation.token);
        if (result.stderr) this.output.appendLine(result.stderr);
        if (result.cancelled || cancellation.token.isCancellationRequested) throw new Error("gopls installation was cancelled.");
        if (result.code !== 0) throw new Error(`go install gopls failed (exit ${result.code}). See Output → Goxide for the Go error.`);
        const binary = path.join(staging, name);
        if (!await executable(binary)) throw new Error("go install did not produce an executable gopls.");
        if (cancellation.token.isCancellationRequested) throw new Error("gopls installation was cancelled.");
        try { await fs.rename(binary, installed); }
        catch (error) {
          // Another extension host may have installed the same pinned tool.
          if (!["EEXIST", "EPERM"].includes(error.code) || !await executable(installed)) throw error;
        }
        this.output.appendLine(`Using gopls ${goplsVersion}: ${installed}`);
        return installed;
      } finally {
        listener.dispose();
        cancellation.dispose();
        this.cancellation = undefined;
        if (staging) await fs.rm(staging, { recursive: true, force: true });
      }
    });
  }
}

module.exports = { Gopls, goplsVersion, installOptions };
