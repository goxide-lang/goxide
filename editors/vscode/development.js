"use strict";

const fs = require("node:fs/promises");
const path = require("node:path");
const { spawn } = require("node:child_process");
const { randomUUID } = require("node:crypto");
const { projectPlan, executable, selectionArguments, owns } = require("./config");

function commandOptions(session, action, extra = []) {
  const base = session.folder?.uri.fsPath || session.plan.root;
  return {
    command: executable(base, session.settings.executable),
    args: [...(action === "run" ? ["run"] : ["dev", action]), ...selectionArguments(session.settings), ...extra, "--", session.plan.input],
    cwd: session.plan.root,
  };
}

function stopProcess(child, signal) {
  if (!child.pid) return;
  try {
    if (process.platform === "win32") {
      const killer = spawn("taskkill", ["/pid", String(child.pid), "/T", "/F"], { stdio: "ignore", windowsHide: true });
      killer.on("error", () => child.kill());
    } else process.kill(-child.pid, signal);
  } catch (error) { if (error.code !== "ESRCH") throw error; }
}

function execute(options, token, onLine) {
  return new Promise((resolve, reject) => {
    if (token?.isCancellationRequested) return resolve({ code: null, cancelled: true, stdout: "", stderr: "" });
    const child = spawn(options.command, options.args, {
      cwd: options.cwd, env: options.env, stdio: ["ignore", "pipe", "pipe"], detached: process.platform !== "win32", windowsHide: true,
    });
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    let stdout = "", stderr = "", pending = "", failure, killTimer;
    const stop = () => {
      if (killTimer) return;
      stopProcess(child, "SIGTERM");
      killTimer = setTimeout(() => stopProcess(child, "SIGKILL"), 1000);
      killTimer.unref();
    };
    const cancellation = token?.onCancellationRequested(stop);
    child.stdout.on("data", data => {
      const text = data.toString();
      if (!onLine) stdout += text;
      else {
        pending += text;
        let index;
        while ((index = pending.indexOf("\n")) >= 0) {
          const line = pending.slice(0, index); pending = pending.slice(index + 1);
          try { if (line.trim()) onLine(JSON.parse(line)); }
          catch (error) { failure = error; stop(); }
        }
      }
    });
    child.stderr.on("data", data => { stderr = (stderr + data.toString()).slice(-65536); });
    child.on("error", error => {
      clearTimeout(killTimer);
      cancellation?.dispose();
      reject(new Error(error.code === "ENOENT" ? `Executable was not found: ${options.command}. Check the extension host PATH and configured executable paths.` : error.message));
    });
    child.on("close", code => {
      // Closing the group leader's pipes does not prove detached/background
      // descendants exited. Finish cancellation before retiring its timer.
      if (killTimer) stopProcess(child, "SIGKILL");
      clearTimeout(killTimer);
      cancellation?.dispose();
      if (pending.trim() && !failure) {
        try { onLine(JSON.parse(pending)); } catch (error) { failure = error; }
      }
      if (failure) reject(failure);
      else resolve({ code, cancelled: token?.isCancellationRequested === true, stdout, stderr });
    });
  });
}

class Development {
  constructor(extension) {
    this.extension = extension;
    this.vscode = extension.vscode;
    this.metadata = new WeakMap();
    this.projects = new Map();
    this.selections = new Map();
  }

  register() {
    const { vscode, extension } = this;
    this.controller = vscode.tests.createTestController("goxide", "Goxide");
    this.controller.resolveHandler = item => this.refresh(item);
    this.controller.refreshHandler = token => this.refresh(undefined, token);
    this.controller.createRunProfile("Run", vscode.TestRunProfileKind.Run, (request, token) => this.runTests(request, token), true);
    this.controller.createRunProfile("Debug", vscode.TestRunProfileKind.Debug, (request, token) => this.debugTests(request, token), true);
    const register = (name, action) => vscode.commands.registerCommand(name, (...args) => action(...args).catch(error => vscode.window.showErrorMessage(`Goxide: ${error.message}`)));
    return [this.controller,
      register("goxide.run", () => this.run()),
      register("goxide.test", () => this.runAll()),
      register("goxide.debug", () => this.debug()),
      register("goxide.selectTarget", () => this.selectTarget()),
      vscode.debug.registerDebugConfigurationProvider("goxide", {
        provideDebugConfigurations: () => [{ name: "Debug Goxide", type: "goxide", request: "launch" }],
        resolveDebugConfiguration: (_folder, config) => ({ ...config, type: "goxide", request: "launch", name: config.name || "Debug Goxide" }),
        resolveDebugConfigurationWithSubstitutedVariables: async (folder, config) => {
          if (!vscode.workspace.isTrusted) throw new Error("Trust this workspace before running Goxide programs.");
          const selected = config.input ? await this.sessionForInput(folder, config.input) : await this.current();
          if (!selected) return undefined;
          let session = { ...selected, settings: { ...selected.settings } };
          const explicitTarget = ["bin", "example", "lib"].some(key => config[key] !== undefined);
          if (explicitTarget) Object.assign(session.settings, { bin: "", example: "", lib: false });
          for (const key of ["bin", "example", "lib", "features", "tags", "noDefaultFeatures", "target"]) {
            if (config[key] !== undefined) session.settings[key] = config[key];
          }
          session = await this.select(session, config.test ? "test" : "run", false, !explicitTarget);
          if (!session) return undefined;
          await this.save(session);
          return { ...config, input: session.plan.input, __goxide: { plan: session.plan, settings: session.settings, base: session.folder?.uri.fsPath || session.plan.root } };
        },
      }),
      vscode.debug.registerDebugAdapterDescriptorFactory("goxide", {
        createDebugAdapterDescriptor: session => {
          const config = session.configuration;
          const selected = config.__goxide;
          if (!selected) throw new Error("Goxide debug input was not resolved.");
          const settings = vscode.workspace.getConfiguration("goxide", session.workspaceFolder?.uri);
          const extra = ["--dlv", executable(selected.base, config.dlv || settings.get("dlv"))];
          if (config.test) extra.push("--test");
          if (config.package) extra.push("--package", config.package);
          if (config.run) extra.push("--run", config.run);
          const options = commandOptions({ plan: selected.plan, settings: selected.settings, folder: session.workspaceFolder }, "debug", extra);
          return new vscode.DebugAdapterExecutable(options.command, options.args, { cwd: options.cwd });
        },
      }),
      vscode.workspace.onDidSaveTextDocument(document => {
        for (const project of this.projects.values()) {
          if (owns(project.session.plan, document.uri.fsPath)) project.item.canResolveChildren = true;
        }
      }),
    ];
  }

  async sessionForInput(folder, input) {
    const base = folder?.uri.fsPath || process.cwd();
    const plan = projectPlan(base, input);
    const settings = this.extension.settings(folder?.uri || this.vscode.Uri.file(plan.root));
    if (!settings.enabled) throw new Error("Goxide is disabled for this workspace.");
    return { plan, folder, settings };
  }

  async available() {
    if (!this.vscode.workspace.isTrusted) throw new Error("Trust this workspace before running Goxide programs.");
    const sessions = [...new Map([...this.extension.sessions.values()].map(session => this.fresh(session)).filter(session => session.settings.enabled).map(session => [session.plan.input, session])).values()];
    for (const folder of this.vscode.workspace.workspaceFolders || []) {
      if (folder.uri.scheme !== "file") continue;
      const settings = this.extension.settings(folder.uri);
      if (!settings.enabled) continue;
      const input = settings.input || ".";
      const plan = projectPlan(folder.uri.fsPath, input);
      if (sessions.some(session => session.plan.input === plan.input)) continue;
      if (settings.input || await fs.stat(path.join(plan.root, "goxide.toml")).then(() => true, () => false)) sessions.push({ plan, folder, settings });
    }
    return sessions;
  }

  fresh(session) {
    const settings = this.extension.settings(session.folder?.uri || this.vscode.Uri.file(session.plan.root));
    const plan = settings.input ? projectPlan(session.folder?.uri.fsPath || session.plan.root, settings.input) : session.plan;
    return { ...session, plan, settings };
  }

  async current() {
    const sessions = await this.available();
    const uri = this.vscode.window.activeTextEditor?.document.uri;
    const current = uri?.scheme === "file" && sessions.filter(session => owns(session.plan, uri.fsPath)).sort((a, b) => b.plan.root.length - a.plan.root.length)[0];
    if (current) return current;
    if (sessions.length === 1) return sessions[0];
    if (!sessions.length) throw new Error("Open a Goxide project or source file first.");
    const pick = await this.vscode.window.showQuickPick(sessions.map(session => ({ label: path.basename(session.plan.root), description: session.plan.input, session })), { placeHolder: "Select a Goxide project" });
    return pick?.session;
  }

  async select(session, purpose, force = false, remembered = true) {
    const settings = { ...session.settings };
    const configuredTarget = JSON.stringify([settings.bin, settings.example, settings.lib]);
    const previous = this.selections.get(session.plan.input);
    if (previous && previous.configuredTarget === configuredTarget && !force && remembered) Object.assign(settings, previous.selection);
    if (!force && (settings.bin || settings.example || settings.lib)) {
      if (purpose === "run" && settings.lib) throw new Error("Library targets cannot run; use Goxide: Select Project Target to choose a binary or example.");
      return { ...session, settings };
    }
    const options = commandOptions({ ...session, settings: { ...settings, bin: "", example: "", lib: false } }, "targets");
    const result = await execute(options);
    if (result.code !== 0) throw new Error(result.stderr || "Could not read Goxide targets.");
    const targets = JSON.parse(result.stdout).filter(target => purpose !== "run" || target.kind !== "lib");
    if (!targets.length) throw new Error("No executable target is available; select an ordinary binary or example.");
    const target = targets.length === 1 ? targets[0] : (await this.vscode.window.showQuickPick(targets.map(target => ({
      label: target.name, description: target.kind, detail: target.requiredFeatures?.length ? `${target.path} — requires ${target.requiredFeatures.join(", ")}` : target.path, target,
    })), { placeHolder: "Select the target for this Goxide session" }))?.target;
    if (!target) return undefined;
    const selection = { bin: target.kind === "bin" ? target.name : "", example: target.kind === "example" ? target.name : "", lib: target.kind === "lib" };
    this.selections.set(session.plan.input, { configuredTarget, selection });
    return { ...session, settings: { ...settings, ...selection } };
  }

  async selectTarget() {
    const session = await this.current();
    if (!session) return;
    const selected = await this.select(session, "test", true);
    if (selected) await this.refresh();
  }

  async save(session) {
    for (const document of this.vscode.workspace.textDocuments) {
      if (document.isDirty && document.uri.scheme === "file" && owns(session.plan, document.uri.fsPath) && !await document.save()) {
        throw new Error(`Save ${document.uri.fsPath} before running it.`);
      }
    }
  }

  async run() {
    const current = await this.current();
    const session = current && await this.select(current, "run");
    if (!session) return;
    await this.save(session);
    const options = commandOptions(session, "run");
    const task = new this.vscode.Task({ type: "goxide", input: session.plan.input }, session.folder || this.vscode.TaskScope.Workspace,
      "Run Goxide", "Goxide", new this.vscode.ProcessExecution(options.command, options.args, { cwd: options.cwd }), []);
    return this.vscode.tasks.executeTask(task);
  }

  async debug() {
    const current = await this.current();
    const session = current && await this.select(current, "run");
    if (!session) return;
    return this.vscode.debug.startDebugging(session.folder, this.debugConfig(session));
  }

  debugConfig(session, extra = {}) {
    const settings = Object.fromEntries(["bin", "example", "lib", "features", "tags", "noDefaultFeatures", "target"].map(key => [key, session.settings[key]]));
    return { name: "Debug Goxide", type: "goxide", request: "launch", input: session.plan.input, ...settings, ...extra };
  }

  async refresh(item, token) {
    const sessions = item ? [this.metadata.get(item)?.session].filter(Boolean).map(session => this.fresh(session)) : await this.available();
    for (const original of sessions) {
      if (token?.isCancellationRequested) return;
      const session = await this.select(original, "test");
      if (!session) { this.projects.get(original.plan.input)?.item.children.replace([]); continue; }
      const id = session.plan.input;
      let project = this.projects.get(id);
      if (!project) {
        const projectItem = this.controller.createTestItem(id, path.basename(session.plan.root), this.vscode.Uri.file(session.plan.root));
        project = { item: projectItem, session };
        this.projects.set(id, project); this.controller.items.add(projectItem);
      }
      project.session = session; project.item.busy = true;
      this.metadata.set(project.item, { session });
      try {
        await this.save(session);
        const result = await execute(commandOptions(session, "tests"), token);
        if (result.cancelled) return;
        if (result.code !== 0) throw new Error(result.stderr || "Test discovery failed.");
        const packages = new Map();
        for (const test of JSON.parse(result.stdout)) {
          let parent = packages.get(test.package);
          if (!parent) {
            parent = this.controller.createTestItem(JSON.stringify([id, test.package]), test.package);
            this.metadata.set(parent, { session, package: test.package }); packages.set(test.package, parent);
          }
          const child = this.controller.createTestItem(JSON.stringify([id, test.package, test.name]), test.name, this.vscode.Uri.file(test.file));
          child.range = new this.vscode.Range(test.line - 1, 0, test.line - 1, 0);
          this.metadata.set(child, { session, ...test }); parent.children.add(child);
        }
        project.item.children.replace([...packages.values()]);
        project.item.error = undefined; project.item.canResolveChildren = false;
      } catch (error) { project.item.children.replace([]); project.item.error = error.message; }
      finally { project.item.busy = false; }
    }
    if (!item) {
      const inputs = new Set(sessions.map(session => session.plan.input));
      for (const [id] of this.projects) if (!inputs.has(id)) { this.controller.items.delete(id); this.projects.delete(id); }
    }
  }

  leaves(request) {
    const excluded = new Set((request.exclude || []).map(item => item.id)), result = [];
    const included = request.include && new Set(request.include.map(item => item.id));
    const visit = (item, selected) => {
      if (excluded.has(item.id)) return;
      selected ||= !included || included.has(item.id);
      if (this.metadata.get(item)?.name) { if (selected) result.push(item); }
      else item.children.forEach(child => visit(child, selected));
    };
    this.controller.items.forEach(item => visit(item, false));
    return result;
  }

  async runAll() {
    return this.runTests(new this.vscode.TestRunRequest(), undefined);
  }

  async runTests(request, token) {
    await this.refresh(undefined, token);
    if (token?.isCancellationRequested) return;
    const run = this.controller.createTestRun(request);
    const items = this.leaves(request), groups = new Map(), settled = new Set();
    if (!items.length && request.include?.length) run.appendOutput("Selected tests are no longer available in the current project configuration.\r\n");
    for (const item of items) {
      run.enqueued(item);
      const data = this.metadata.get(item), key = `${data.session.plan.input}\0${data.package}`;
      if (!groups.has(key)) groups.set(key, []);
      groups.get(key).push(item);
    }
    try {
      for (const group of groups.values()) {
        if (token?.isCancellationRequested) break;
        const data = this.metadata.get(group[0]);
        await this.save(data.session);
        const names = group.map(item => this.metadata.get(item).name);
        const byName = new Map(group.map(item => [this.metadata.get(item).name, item]));
        const done = new Set(), messages = new Map();
        const pattern = `^(?:${names.map(name => name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|")})$`;
        const result = await execute(commandOptions(data.session, "test", ["--package", data.package, "--run", pattern]), token, event => {
          const item = byName.get(event.Test?.split("/")[0]);
          if (event.Output) {
            let location;
            if (event.Location) location = new this.vscode.Location(this.vscode.Uri.file(event.Location.file), new this.vscode.Position(event.Location.line - 1, 0));
            run.appendOutput(event.Output.replace(/\r?\n/g, "\r\n"), location, item);
            if (item && event.Location) {
              const message = new this.vscode.TestMessage(event.Output.trim()); message.location = location;
              if (!messages.has(item)) messages.set(item, []);
              messages.get(item).push(message);
            }
          }
          if (!item || event.Test.includes("/")) return;
          if (event.Action === "run") run.started(item);
          if (event.Action === "pass") { run.passed(item, event.Elapsed * 1000); done.add(item); settled.add(item); }
          if (event.Action === "skip") { run.skipped(item); done.add(item); settled.add(item); }
          if (event.Action === "fail") { run.failed(item, messages.get(item) || new this.vscode.TestMessage("Test failed; see output."), event.Elapsed * 1000); done.add(item); settled.add(item); }
        });
        if (result.stderr) run.appendOutput(result.stderr.replace(/\r?\n/g, "\r\n"));
        for (const item of group) if (!done.has(item)) {
          if (result.cancelled || result.code === 0) run.skipped(item);
          else run.errored(item, new this.vscode.TestMessage(result.stderr || "Test process failed before reporting a result."));
          settled.add(item);
        }
      }
    } catch (error) {
      run.appendOutput(`${error.message}\r\n`);
      for (const item of items) if (!settled.has(item)) { run.errored(item, new this.vscode.TestMessage(error.message)); settled.add(item); }
    } finally {
      for (const item of items) if (!settled.has(item)) run.skipped(item);
      run.end();
    }
  }

  async debugTests(request, token) {
    await this.refresh(undefined, token);
    const items = this.leaves(request);
    if (!items.length || token?.isCancellationRequested) return;
    const picked = items.length === 1 ? items[0] : (await this.vscode.window.showQuickPick(items.map(item => ({ label: item.label, description: this.metadata.get(item).package, item })), { placeHolder: "Select one test to debug" }))?.item;
    if (!picked) return;
    const data = this.metadata.get(picked);
    const name = data.name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    const run = this.controller.createTestRun(request), id = randomUUID();
    const messages = [];
    const testResult = new RegExp(`^--- (PASS|FAIL|SKIP): ${name}(?:\\s|$)`);
    let debugSession, exited, outcome, finished = false, output = "", pending = "", complete;
    const done = new Promise(resolve => { complete = resolve; });
    const finish = error => {
      if (finished) return;
      finished = true;
      if (pending) {
        outcome = testResult.exec(pending)?.[1] || outcome;
        run.appendOutput(pending.replace(/\r?\n/g, "\r\n"), undefined, picked);
      }
      if (token?.isCancellationRequested || (!error && exited === undefined)) run.skipped(picked);
      else if (error) run.errored(picked, new this.vscode.TestMessage(error.message || String(error)));
      else if (exited !== 0 || outcome === "FAIL") run.failed(picked, messages.length ? messages : new this.vscode.TestMessage(output.trim() || `Test exited with code ${exited}.`));
      else if (outcome === "PASS") run.passed(picked);
      else run.skipped(picked);
      run.end(); complete();
    };
    const consume = text => {
      output = (output + text).slice(-65536);
      pending += text;
      let index;
      while ((index = pending.indexOf("\n")) >= 0) {
        const line = pending.slice(0, index); pending = pending.slice(index + 1);
        outcome = testResult.exec(line)?.[1] || outcome;
        const match = /^\s*(.+\.(?:gox|go)):(\d+)(?::\d+)?:\s/.exec(line);
        let location;
        if (match) {
          let filename = match[1];
          if (!path.isAbsolute(filename)) filename = this.leaves(new this.vscode.TestRunRequest()).map(item => this.metadata.get(item)).find(test => test.package === data.package && path.basename(test.file) === filename)?.file;
          if (filename) location = new this.vscode.Location(this.vscode.Uri.file(filename), new this.vscode.Position(Number(match[2]) - 1, 0));
          const message = new this.vscode.TestMessage(line.trim()); message.location = location; messages.push(message);
        }
        run.appendOutput(`${line.replace(/\r$/, "")}\r\n`, location, picked);
      }
    };
    const isOurs = session => session.configuration.__goxideTestRun === id;
    const disposables = [
      this.vscode.debug.registerDebugAdapterTrackerFactory("goxide", { createDebugAdapterTracker: session => isOurs(session) ? {
        onDidSendMessage: message => {
          if (message.type === "response" && message.command === "launch" && !message.success) finish(new Error(message.message || "The test debugger could not launch."));
          if (message.type !== "event") return;
          if (message.event === "output") consume(message.body.output || "");
          if (message.event === "exited") exited = message.body.exitCode;
        },
        onError: finish, onExit: () => finish(),
      } : undefined }),
      this.vscode.debug.onDidStartDebugSession(session => {
        if (!isOurs(session)) return;
        debugSession = session; run.started(picked);
        if (token?.isCancellationRequested) void this.vscode.debug.stopDebugging(session);
      }),
      this.vscode.debug.onDidTerminateDebugSession(session => { if (isOurs(session)) finish(); }),
      token?.onCancellationRequested(() => { if (debugSession) void this.vscode.debug.stopDebugging(debugSession); }),
    ];
    run.enqueued(picked);
    try {
      const started = await this.vscode.debug.startDebugging(data.session.folder, this.debugConfig(data.session, {
        name: `Debug ${data.name}`, test: true, package: data.package, run: `^${name}$`, args: ["-test.v"], __goxideTestRun: id,
      }));
      if (!started) finish(new Error("The test debugger did not start."));
      await done;
    } catch (error) { finish(error); }
    finally { for (const disposable of disposables) disposable?.dispose(); }
  }
}

module.exports = { Development, commandOptions, execute };
