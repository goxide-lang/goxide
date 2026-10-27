"use strict";

const path = require("node:path");

const settingNames = ["enabled", "executable", "gopls", "input", "bin", "example", "lib", "features", "tags", "noDefaultFeatures", "target", "enableManagedGo"];
const ignoredDirectories = new Set([".git", "node_modules"]);

function contains(root, filename) {
  const relative = path.relative(root, filename);
  return relative === "" || (!relative.startsWith(`..${path.sep}`) && relative !== ".." && !path.isAbsolute(relative));
}

function projectPlan(base, input, standalone = false) {
  const resolved = path.resolve(base, input || ".");
  const singleFile = path.extname(resolved) === ".gox";
  return {
    input: resolved,
    root: singleFile || path.basename(resolved) === "goxide.toml" ? path.dirname(resolved) : resolved,
    singleFile,
    managed: !standalone && !singleFile,
  };
}

function executable(base, value) {
  return value.includes("/") || value.includes("\\") ? path.resolve(base, value) : value;
}

function launchOptions(base, plan, settings, goConflict) {
  if ([settings.bin, settings.example, settings.lib].filter(Boolean).length > 1) {
    throw new Error("goxide.bin, goxide.example, and goxide.lib are mutually exclusive; select one project target.");
  }
  const args = ["lsp", "--gopls", goConflict ? "off" : executable(base, settings.gopls)];
  if (settings.bin) args.push("--bin", settings.bin);
  if (settings.example) args.push("--example", settings.example);
  if (settings.lib) args.push("--lib");
  if (settings.features.length) args.push("--features", settings.features.join(","));
  if (settings.tags.length) args.push("--tags", settings.tags.join(","));
  if (settings.noDefaultFeatures) args.push("--no-default-features");
  if (settings.target) args.push("--target", settings.target);
  args.push("--", plan.input);
  return { command: executable(base, settings.executable), args, options: { cwd: plan.root } };
}

function documentSelector(vscode, plan, managedGo) {
  const pattern = plan.singleFile ? path.basename(plan.input).replace(/[?*\[\]{}]/g, character => `[${character}]`) : "**/*.gox";
  // LanguageClient accepts protocol selectors; a VS Code RelativePattern has a
  // Uri object that the protocol converter drops instead of restricting files.
  const baseUri = vscode.Uri.file(plan.root).toString();
  const selectors = [{ scheme: "file", language: "goxide", pattern: { baseUri, pattern } }];
  if (plan.managed && managedGo) {
    selectors.push({ scheme: "file", language: "go", pattern: { baseUri, pattern: "**/*.go" } });
  }
  return selectors;
}

function owns(plan, filename) {
  return plan.singleFile ? filename === plan.input : contains(plan.root, filename);
}

function watched(plan, filename) {
  if (!contains(plan.root, filename)) return false;
  const parts = path.relative(plan.root, filename).split(path.sep);
  return parts[0] !== "target" && !parts.some(part => ignoredDirectories.has(part));
}

module.exports = { settingNames, contains, projectPlan, launchOptions, documentSelector, owns, watched };
