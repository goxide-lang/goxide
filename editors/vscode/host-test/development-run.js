"use strict";

const fs = require("node:fs/promises");
const path = require("node:path");
const os = require("node:os");
const { spawn } = require("node:child_process");

async function main() {
  const compiler = process.env.GOXIDE_TEST_COMPILER;
  if (!compiler || !path.isAbsolute(compiler)) throw new Error("Set GOXIDE_TEST_COMPILER to an already built Goxide executable.");
  const executable = process.env.VSCODE_EXECUTABLE || (process.platform === "darwin" ? "/Applications/Visual Studio Code.app/Contents/MacOS/Code" : "code");
  const temporary = await fs.realpath(await fs.mkdtemp(path.join(process.platform === "darwin" ? "/private/tmp" : os.tmpdir(), "goxide-dev-host-")));
  try {
    const root = path.join(temporary, "project");
    const files = {
      "goxide.toml": "[go_mod]\nmodule='example.org/editor'\ngo='1.27'\n[lib]\nname='app'\npath='.'\n[[bin]]\nname='main'\npath='cmd/main'\n[dependencies]\ntesting={go='testing'}\n",
      "lib.gox": "package app\npub mod value\npub mod skipped\n",
      "skipped/skipped.gox": "package skipped\n",
      "skipped/skipped_test.gox": "package skipped\nuse testing\nfunc TestMain(m *testing::M) {}\nfunc TestNotRun(t *testing::T) { t.Fatal(\"TestMain must skip this test\") }\n",
      "value/value.gox": "package value\npub func Answer() int { return 42 }\n",
      "value/value_test.gox": "package value\nuse testing\nfunc TestPass(t *testing::T) {\n    if Answer() != 42 { t.Fatal(\"incorrect answer\") }\n}\nfunc TestFail(t *testing::T) {\n    t.Fatal(\"host failure location\")\n}\n",
      "cmd/main/main.gox": "package main\nuse app::value\nfunc main() { println(value::Answer()) }\n",
      ".vscode/settings.json": JSON.stringify({
        "goxide.executable": compiler, "goxide.gopls": "off", "goxide.bin": "main", "goxide.dlv": process.env.GOXIDE_TEST_DLV || "dlv",
        "workbench.startupEditor": "none", "telemetry.telemetryLevel": "off", "extensions.autoUpdate": false,
      }),
    };
    for (const [name, text] of Object.entries(files)) {
      const file = path.join(root, name);
      await fs.mkdir(path.dirname(file), { recursive: true }); await fs.writeFile(file, text);
    }
    const env = { ...process.env, GOXIDE_VSCODE_TEST_ROOT: root };
    delete env.ELECTRON_RUN_AS_NODE;
    const child = spawn(executable, [root, `--user-data-dir=${path.join(temporary, "user-data")}`, `--extensions-dir=${path.join(temporary, "extensions")}`,
      `--extensionDevelopmentPath=${path.dirname(__dirname)}`, `--extensionTestsPath=${path.join(__dirname, "development-suite.js")}`,
      "--disable-extensions", "--disable-workspace-trust", "--skip-welcome", "--skip-release-notes", "--disable-updates", "--disable-gpu",
    ], { env, stdio: ["ignore", "pipe", "pipe"], detached: process.platform !== "win32" });
    let output = "", success;
    for (const stream of [child.stdout, child.stderr]) stream.on("data", data => {
      output = (output + data).slice(-32768);
      success = output.match(/^Goxide development host test passed: .+\.\r?$/m)?.[0].trim() || success;
    });
    let timedOut = false, killed = false, killTimer, closeTimer, rejectHost;
    const stopHost = signal => {
      if (killed || !Number.isInteger(child.pid) || child.pid <= 0) return;
      if (signal === "SIGKILL") killed = true;
      if (process.platform === "win32") {
        const killer = spawn("taskkill", ["/pid", String(child.pid), "/T", "/F"], { stdio: "ignore" });
        killer.once("error", () => child.kill("SIGKILL")); killer.unref();
      } else {
        try { process.kill(-child.pid, signal); } catch (error) { if (error.code !== "ESRCH") throw error; }
      }
    };
    const failure = (code, signal) => new Error(`Development host test ${timedOut ? "timed out" : `exited ${code ?? signal}`}\n${output.slice(-8000)}`);
    const timeout = setTimeout(() => {
      timedOut = true;
      stopHost("SIGTERM");
      killTimer = setTimeout(() => stopHost("SIGKILL"), 1000);
      closeTimer = setTimeout(() => {
        stopHost("SIGKILL"); child.stdout.destroy(); child.stderr.destroy(); child.unref(); rejectHost(failure());
      }, 3000);
    }, 120000);
    try {
      await new Promise((resolve, reject) => {
        rejectHost = reject;
        child.once("error", reject);
        child.once("close", (code, signal) => !timedOut && code === 0 ? resolve() : reject(failure(code, signal)));
      });
      if (!success) throw new Error(`Development host exited successfully without the suite success marker\n${output.slice(-8000)}`);
      console.log(success);
    } finally { clearTimeout(timeout); clearTimeout(killTimer); clearTimeout(closeTimer); stopHost("SIGKILL"); }
  } finally { await fs.rm(temporary, { recursive: true, force: true }); }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
