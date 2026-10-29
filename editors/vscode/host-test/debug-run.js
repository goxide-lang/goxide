"use strict";

const fs = require("node:fs/promises");
const os = require("node:os");
const path = require("node:path");
const { spawn } = require("node:child_process");

async function main() {
  const compiler = process.env.GOXIDE_TEST_COMPILER;
  const delve = process.env.GOXIDE_TEST_DLV;
  if (!compiler || !delve || !path.isAbsolute(compiler) || !path.isAbsolute(delve)) {
    throw new Error("Set GOXIDE_TEST_COMPILER and GOXIDE_TEST_DLV to absolute paths for a working debugger environment.");
  }
  const temporary = await fs.realpath(await fs.mkdtemp(path.join(process.platform === "darwin" ? "/private/tmp" : os.tmpdir(), "goxide-debug-host-")));
  const executable = process.env.VSCODE_EXECUTABLE || (process.platform === "darwin" ? "/Applications/Visual Studio Code.app/Contents/MacOS/Code" : "code");
  try {
    const root = path.join(temporary, "project");
    const files = {
      "goxide.toml": "[go_mod]\nmodule='example.org/debugeditor'\ngo='1.27'\n[lib]\nname='app'\npath='.'\n[[bin]]\nname='main'\npath='cmd/main'\n",
      "lib.gox": "package app\npub mod value\n",
      "value/value.gox": "package value\npub func Answer() int {\n    answer := 40\n    answer += 2\n    return answer\n}\n",
      "cmd/main/main.gox": "package main\nuse app::value\nfunc main() {\n    result := value::Answer()\n    println(result)\n}\n",
      ".vscode/settings.json": JSON.stringify({
        "goxide.executable": compiler, "goxide.gopls": "off", "goxide.bin": "main", "goxide.dlv": delve,
        "workbench.startupEditor": "none", "telemetry.telemetryLevel": "off", "extensions.autoUpdate": false,
      }),
    };
    for (const [name, text] of Object.entries(files)) {
      const filename = path.join(root, name);
      await fs.mkdir(path.dirname(filename), { recursive: true });
      await fs.writeFile(filename, text);
    }
    const env = { ...process.env, GOXIDE_VSCODE_TEST_ROOT: root };
    delete env.ELECTRON_RUN_AS_NODE;
    const child = spawn(executable, [root, `--user-data-dir=${path.join(temporary, "user-data")}`, `--extensions-dir=${path.join(temporary, "extensions")}`,
      `--extensionDevelopmentPath=${path.dirname(__dirname)}`, `--extensionTestsPath=${path.join(__dirname, "debug-suite.js")}`,
      "--disable-extensions", "--disable-workspace-trust", "--skip-welcome", "--skip-release-notes", "--disable-updates", "--disable-gpu",
    ], { env, detached: process.platform !== "win32", stdio: ["ignore", "pipe", "pipe"] });
    let output = "", success;
    for (const stream of [child.stdout, child.stderr]) stream.on("data", data => {
      output = (output + data).slice(-32768);
      success = output.match(/^Goxide debug host test passed: .+\.\r?$/m)?.[0].trim() || success;
    });
    let timeout, forced, deadline, killed = false, timedOut = false;
    const stop = signal => {
      if (killed || !Number.isInteger(child.pid) || child.pid <= 0) return;
      if (signal === "SIGKILL") killed = true;
      if (process.platform === "win32") {
        spawn("taskkill", ["/PID", String(child.pid), "/T", "/F"], { stdio: "ignore", windowsHide: true }).on("error", () => {});
      } else {
        try { process.kill(-child.pid, signal); } catch (error) { if (error.code !== "ESRCH") throw error; }
      }
    };
    try {
      await new Promise((resolve, reject) => {
        timeout = setTimeout(() => {
          timedOut = true;
          stop("SIGTERM");
          forced = setTimeout(() => stop("SIGKILL"), 1000);
          deadline = setTimeout(() => {
            stop("SIGKILL"); child.stdout.destroy(); child.stderr.destroy(); child.unref();
            reject(new Error(`Debug host test timed out\n${output.slice(-16000)}`));
          }, 3000);
        }, 120000);
        child.on("error", reject);
        child.on("close", (code, signal) => !timedOut && code === 0 ? resolve() :
          reject(new Error(`Debug host test ${timedOut ? "timed out" : `exited ${code ?? signal}`}\n${output.slice(-16000)}`)));
      });
      if (!success) throw new Error(`Debug host exited successfully without the suite success marker\n${output.slice(-16000)}`);
      console.log(success);
    } finally { clearTimeout(timeout); clearTimeout(forced); clearTimeout(deadline); stop("SIGKILL"); }
  } finally { await fs.rm(temporary, { recursive: true, force: true }); }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
