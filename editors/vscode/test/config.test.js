"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const path = require("node:path");
const { pathToFileURL } = require("node:url");
const { RelativePattern } = require("vscode-languageserver-protocol");
const { settingNames, contains, projectPlan, launchOptions, documentSelector, owns, watched } = require("../config");
const manifest = require("../package.json");

const defaults = Object.fromEntries(Object.entries(manifest.contributes.configuration.properties)
  .map(([key, value]) => [key.slice("goxide.".length), value.default]));
const root = path.resolve("fixture project");
const vscode = { Uri: { file: filename => ({ toString: () => pathToFileURL(filename).href }) } };

test("settings schema covers exactly the settings used by the extension", () => {
  assert.deepEqual(Object.keys(defaults).sort(), [...settingNames].sort());
  for (const property of Object.values(manifest.contributes.configuration.properties)) {
    assert.equal(property.scope, "resource");
    assert.equal(Array.isArray(property.default) ? "array" : typeof property.default, property.type);
  }
  assert.equal(defaults.enableManagedGo, false);
  assert.equal(manifest.capabilities.untrustedWorkspaces.supported, false);
  assert.equal(manifest.capabilities.virtualWorkspaces.supported, false);
  assert.equal(manifest.contributes.languages[0].id, "goxide");
  assert.deepEqual(manifest.contributes.languages[0].extensions, [".gox"]);
  assert.equal(manifest.main, "./extension.js");
});

test("CLI arguments preserve paths and feature values without shell splitting", () => {
  const plan = projectPlan(root, "demo project/goxide.toml");
  const options = launchOptions(root, plan, {
    ...defaults, executable: "./bin/goxide executable", gopls: "./bin/gopls executable",
    features: ["fast", "network"], target: "linux/amd64",
  }, false);
  assert.deepEqual(options, {
    command: path.join(root, "bin/goxide executable"),
    args: ["lsp", "--gopls", path.join(root, "bin/gopls executable"), "--features", "fast,network", "--target", "linux/amd64", "--", path.join(root, "demo project/goxide.toml")],
    options: { cwd: path.join(root, "demo project") },
  });
  assert.equal(launchOptions(root, plan, defaults, true).args[2], "off");
  assert.equal(launchOptions(root, plan, { ...defaults, gopls: "off" }, false).args[2], "off");
});

test("Go selectors require an explicitly managed project and explicit Go opt-in", () => {
  const project = projectPlan(root, ".");
  const single = projectPlan(root, "one.gox", true);
  assert.deepEqual(documentSelector(vscode, project, false).map(value => value.language), ["goxide"]);
  const selectors = documentSelector(vscode, project, true);
  assert.deepEqual(selectors.map(value => value.language), ["goxide", "go"]);
  assert.equal(selectors[1].scheme, "file");
  assert.ok(selectors.every(filter => RelativePattern.is(filter.pattern)), "language client requires protocol relative patterns");
  assert.equal(selectors[1].pattern.baseUri, pathToFileURL(root).href);
  assert.equal(selectors[1].pattern.pattern, "**/*.go");
  assert.deepEqual(documentSelector(vscode, single, true).map(value => value.language), ["goxide"]);
  assert.equal(documentSelector(vscode, single, true)[0].pattern.pattern, "one.gox");
  const literal = projectPlan(root, "file[1].gox", true);
  assert.equal(documentSelector(vscode, literal, false)[0].pattern.pattern, "file[[]1[]].gox");
  assert.equal(owns(single, path.join(root, "two.gox")), false);
  assert.equal(contains(root, `${root}-neighbor/main.go`), false);
});

test("project targets are mutually exclusive and build configuration reaches the CLI", () => {
  const plan = projectPlan(root, ".");
  for (const [selected, expected] of [
    [{ bin: "demo" }, ["--bin", "demo"]], [{ example: "demo" }, ["--example", "demo"]], [{ lib: true }, ["--lib"]],
  ]) {
    const options = launchOptions(root, plan, { ...defaults, ...selected, tags: ["debug", "trace"], noDefaultFeatures: true }, false);
    assert.deepEqual(options.args, ["lsp", "--gopls", "gopls", ...expected, "--tags", "debug,trace", "--no-default-features", "--", root]);
  }
  for (const selected of [{ bin: "demo", example: "demo" }, { bin: "demo", lib: true }, { example: "demo", lib: true }]) {
    assert.throws(() => launchOptions(root, plan, { ...defaults, ...selected }, false), /mutually exclusive/);
  }
});

test("file events cover dependencies and resources but ignore managed output and tooling directories", () => {
  const plan = projectPlan(root, ".");
  for (const file of ["main.gox", "pkg/helper.go", "goxide.toml", "goxide.lock", "go.mod", "go.sum", "assets/template.txt", "pkg/target/ordinary.gox"]) {
    assert.equal(watched(plan, path.join(root, file)), true, file);
  }
  for (const file of ["target/private/main.go", ".git/index", "tools/node_modules/a/index.js", "../outside.go"]) {
    assert.equal(watched(plan, path.join(root, file)), false, file);
  }
});

module.exports = { defaults };
