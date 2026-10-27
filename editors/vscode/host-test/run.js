"use strict";

// Uses an already installed VS Code. No Electron downloads or user settings.
const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const { spawn } = require("node:child_process");

async function main() {
  const executable = process.env.VSCODE_EXECUTABLE || (process.platform === "darwin"
    ? "/Applications/Visual Studio Code.app/Contents/MacOS/Code" : "code");
  if (process.platform === "win32") throw new Error("The local fake-server launcher requires a POSIX host.");
  // macOS per-user temp paths can exceed its 103-byte Unix socket limit once
  // VS Code appends the profile IPC socket name.
  const temporaryBase = process.platform === "darwin" ? "/private/tmp" : os.tmpdir();
  const temporary = await fs.realpath(await fs.mkdtemp(path.join(temporaryBase, "goxide-vscode-host-")));
  try {
    const workspace = path.join(temporary, "project");
    await fs.mkdir(path.join(workspace, ".vscode"), { recursive: true });
    const launcher = path.join(temporary, "goxide test executable");
    await fs.writeFile(launcher, `#!${process.execPath}\nrequire(${JSON.stringify(path.join(__dirname, "server.js"))});\n`, { mode: 0o700 });
    await fs.writeFile(path.join(workspace, ".vscode/settings.json"), JSON.stringify({
      "goxide.executable": launcher, "goxide.gopls": "fixture-gopls", "goxide.enableManagedGo": true,
      "goxide.features": ["fast"], "goxide.target": "linux/amd64", "extensions.autoUpdate": false,
      "goxide.lib": true, "goxide.tags": ["debug", "trace"], "goxide.noDefaultFeatures": true,
      "workbench.startupEditor": "none", "telemetry.telemetryLevel": "off",
    }));
    await fs.writeFile(path.join(workspace, "goxide.toml"), "[package]\nname = 'fixture'\n");
    await fs.writeFile(path.join(workspace, "main.gox"), "package main\n// 😀\nfunc main() {}\n");
    await fs.writeFile(path.join(workspace, "native.go"), "package main\nvar native = 1\n");
    const environment = { ...process.env, GOXIDE_VSCODE_TEST_ROOT: workspace, GOXIDE_VSCODE_TEST_LOG: path.join(temporary, "protocol.jsonl") };
    delete environment.ELECTRON_RUN_AS_NODE;
    const child = spawn(executable, [workspace,
      `--user-data-dir=${path.join(temporary, "user-data")}`, `--extensions-dir=${path.join(temporary, "extensions")}`,
      `--extensionDevelopmentPath=${path.dirname(__dirname)}`, `--extensionTestsPath=${path.join(__dirname, "suite.js")}`,
      "--disable-extensions", "--disable-workspace-trust", "--skip-welcome", "--skip-release-notes", "--disable-updates", "--disable-gpu",
    ], { env: environment, stdio: ["ignore", "pipe", "pipe"], detached: true });
    let output = "", success;
    for (const stream of [child.stdout, child.stderr]) stream.on("data", data => {
      output = (output + data.toString()).slice(-32768);
      success = output.match(/^Goxide extension-host test passed: .+\.\r?$/m)?.[0].trim() || success;
    });
    let timedOut = false, killed = false, killTimer, closeTimer, rejectHost;
    const stopHost = signal => {
      if (killed || !Number.isInteger(child.pid) || child.pid <= 0) return;
      if (signal === "SIGKILL") killed = true;
      try { process.kill(-child.pid, signal); } catch (error) { if (error.code !== "ESRCH") throw error; }
    };
    const failure = (code, signal) => new Error(`VS Code host test ${timedOut ? "timed out" : `exited ${code ?? signal}`}\n${output.slice(-12000)}`);
    const timeout = setTimeout(() => {
      timedOut = true;
      stopHost("SIGTERM");
      killTimer = setTimeout(() => stopHost("SIGKILL"), 1000);
      closeTimer = setTimeout(() => {
        stopHost("SIGKILL"); child.stdout.destroy(); child.stderr.destroy(); child.unref(); rejectHost(failure());
      }, 3000);
    }, 60000);
    try {
      await new Promise((resolve, reject) => {
        rejectHost = reject;
        child.once("error", reject);
        child.once("close", (code, signal) => !timedOut && code === 0 ? resolve() : reject(failure(code, signal)));
      });
      if (!success) throw new Error(`VS Code host exited successfully without the suite success marker\n${output.slice(-12000)}`);
      console.log(success);
    } finally {
      clearTimeout(timeout); clearTimeout(killTimer); clearTimeout(closeTimer); stopHost("SIGKILL");
    }
  } finally {
    await fs.rm(temporary, { recursive: true, force: true });
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
