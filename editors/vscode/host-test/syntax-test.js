"use strict";

// Reuse an installed VS Code's actual tokenizer without opening a window.
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { spawnSync } = require("node:child_process");

async function main() {
  if (!process.versions.electron) {
    const executable = process.env.VSCODE_EXECUTABLE || (process.platform === "darwin"
      ? "/Applications/Visual Studio Code.app/Contents/MacOS/Code" : undefined);
    if (!executable) throw new Error("Set VSCODE_EXECUTABLE to the installed VS Code application executable, not its CLI launcher.");
    const result = spawnSync(executable, [__filename], {
      env: { ...process.env, ELECTRON_RUN_AS_NODE: "1" }, stdio: "inherit", windowsHide: true,
    });
    if (result.error) throw result.error;
    process.exitCode = result.status ?? 1;
    return;
  }
  const application = path.join(process.resourcesPath, "app");
  const modules = ["node_modules.asar", "node_modules"].map(name => path.join(application, name))
    .find(directory => fs.existsSync(path.join(directory, "vscode-textmate")));
  assert.ok(modules, "VS Code's TextMate runtime was not found");
  const textmate = require(path.join(modules, "vscode-textmate"));
  const oniguruma = require(path.join(modules, "vscode-oniguruma"));
  const wasm = fs.readFileSync(path.join(modules, "vscode-oniguruma/release/onig.wasm"));
  await oniguruma.loadWASM(wasm.buffer.slice(wasm.byteOffset, wasm.byteOffset + wasm.byteLength));
  const manifest = require("../package.json");
  const contribution = manifest.contributes.grammars.find(grammar => grammar.language === "goxide");
  assert.ok(contribution, "Goxide grammar must be registered for the language");
  const filename = path.resolve(__dirname, "..", contribution.path);
  const registry = new textmate.Registry({
    onigLib: Promise.resolve(oniguruma),
    loadGrammar: async scope => scope === contribution.scopeName
      ? textmate.parseRawGrammar(fs.readFileSync(filename, "utf8"), filename) : null,
  });
  try {
    const grammar = await registry.loadGrammar(contribution.scopeName);
    assert.ok(grammar);
    function tokenize(source) {
      let state = textmate.INITIAL;
      return source.split("\n").map(line => {
        const result = grammar.tokenizeLine(line, state, 1000);
        assert.equal(result.stoppedEarly, false, `tokenization exceeded its budget: ${line}`);
        state = result.ruleStack;
        return { line, tokens: result.tokens };
      });
    }
    function at(row, text, occurrence = 0) {
      let index = -1;
      for (let i = 0; i <= occurrence; i++) index = row.line.indexOf(text, index + 1);
      assert.ok(index >= 0, `missing fixture text ${text}`);
      const token = row.tokens.find(token => token.startIndex <= index && token.endIndex > index);
      assert.ok(token, `missing token for ${text}`);
      return token;
    }
    function scoped(row, text, scope, occurrence = 0) {
      const token = at(row, text, occurrence);
      assert.ok(token.scopes.includes(`${scope}.goxide`), `${text}: ${token.scopes.join(" ")} lacks ${scope}`);
    }

    const source = tokenize([
      "package sample",
      "pub use crate::model::{Item as Model, r#match};",
      "#[derive(std::derive::Clone)]",
      "pub enum State[T] { Idle, Value(T) }",
      "trait Read { func get(self &Self) Option[i32] }",
      "impl Read for State[i32] { pub func get(self &Self) Option[i32] {",
      "    return match &self { State::Value(ref value) if value > 0 => Some(value), _ => None }",
      "} }",
      "macro_rules! capture { ($value:expr) => { $caller::emit!($value?) }; }",
      "func main() { var n = 0x1.fp+2; p := &mut n; fmt::Println(capture!(n)) }",
    ].join("\n"));
    for (const [line, word, scope] of [
      [0, "package", "keyword.control.import"], [0, "sample", "entity.name.namespace"],
      [1, "pub", "storage.modifier"], [1, "use", "keyword.control.import"], [1, "as", "keyword.control.import"],
      [2, "derive", "entity.other.attribute-name"], [3, "enum", "storage.type"], [3, "State", "entity.name.type"],
      [4, "trait", "storage.type"], [4, "get", "entity.name.function"], [4, "Self", "storage.type.self"],
      [4, "Option", "support.type"], [4, "i32", "support.type"], [5, "impl", "storage.type"],
      [6, "match", "keyword.control"], [6, "ref", "storage.modifier.reference"], [6, "if", "keyword.control"],
      [6, "=>", "keyword.operator"], [8, "macro_rules", "keyword.declaration.macro"], [8, "$value", "variable.parameter.macro"],
      [8, "expr", "storage.type.macro-fragment"], [8, "$caller", "variable.parameter.macro"], [8, "emit", "entity.name.function.macro"],
      [8, "?", "keyword.operator"], [9, "main", "entity.name.function"], [9, "mut", "storage.modifier.reference"],
    ]) scoped(source[line], word, scope);

    const ordinary = "mut ref as where self super in macro_rules async await let fn loop while macro yield".split(" ");
    for (const name of ordinary) scoped(tokenize(`var ${name} int`)[0], name, "variable.other");
    for (const name of ["r#match", "r#Self", "r#你好", "r#where", "match值", "𐐀value"]) {
      const row = tokenize(name)[0], token = at(row, name);
      scoped(row, name, name.startsWith("r#") ? "variable.other.raw" : "variable.other");
      assert.equal(token.startIndex, 0);
      assert.equal(token.endIndex, name.length);
    }
    const references = tokenize("a := &mut value; b := &mut; c := &r#mut; d := &mutable")[0];
    scoped(references, "mut", "storage.modifier.reference");
    scoped(references, "mut", "variable.other", 1);
    scoped(references, "r#mut", "variable.other.raw");
    scoped(references, "mutable", "variable.other");
    scoped(tokenize("func keep[T any](value T) T where T: any { return value }")[0], "where", "keyword.other.constraint");
    scoped(tokenize("match e { E::Record{field: ref value} => value, _ => 0 }")[0], "ref", "storage.modifier.reference");
    for (const line of [
      "match e { _ => func(ref int) int { return ref }(1) }",
      "match e { _ if func(ref int) bool { return true }(1) => 0, _ => 1 }",
    ]) scoped(tokenize(line)[0], "ref", "variable.other");

    const macros = tokenize("crate::m!(x); m! (x); m !(); a != b; r#match![x]")[0];
    scoped(macros, "m!", "entity.name.function.macro");
    scoped(macros, "m!", "variable.other", 1);
    scoped(macros, "m !", "variable.other");
    scoped(macros, "!=", "keyword.operator");
    scoped(macros, "r#match", "entity.name.function.macro");
    scoped(tokenize("macro_rules ! named { ($node:type) => { $node }; }")[0], "named", "entity.name.function.macro");
    scoped(tokenize("macro_rules!compact { () => { 1 }; }")[0], "compact", "entity.name.function.macro");

    // These are accepted by internal/syntax/lexer/TestNumericLiterals.
    const numbers = "0 42 0600 0_600 0b1010 0B_1010 0o600 0O_600 0xBadFace 0X_Bad_Face 1_000 0. 72.40 072.40 .25 1e6 1.e+0 1_5.0_0e+0_2 0x1p-2 0x1.fp+2 0X.8p-0 0x_1.ff_fp+0_2 0i 0129i 0o123i 0xabci 0.i .5i 1e3i 0x1p-2i".split(" ");
    for (const literal of numbers) {
      const row = tokenize(literal)[0], token = at(row, literal);
      scoped(row, literal, "constant.numeric");
      assert.equal(token.startIndex, 0);
      assert.equal(token.endIndex, literal.length, `numeric literal was split: ${literal}`);
    }
    scoped(tokenize("-0x1p-2i")[0], "-", "keyword.operator");

    const literals = tokenize('var s = "// #[derive] \\"x\\" \\u4e16"\nvar r = \'世\'\nvar raw = `match // #[derive] \\n\nstill raw`\npub func next() {}');
    scoped(literals[0], "#[derive]", "string.quoted.double");
    scoped(literals[0], "\\u4e16", "constant.character.escape");
    scoped(literals[1], "世", "string.quoted.single");
    scoped(literals[2], "match", "string.quoted.raw");
    scoped(literals[3], "still raw", "string.quoted.raw");
    scoped(literals[4], "pub", "storage.modifier");
    scoped(tokenize('"unterminated\npub func next() {}')[1], "pub", "storage.modifier");
    const comments = tokenize("/* a /* b */ match e { _ => 0 }\n// pub enum E {}\n/* more\ncomments */ pub mod child");
    scoped(comments[0], "a", "comment.block");
    scoped(comments[0], "match", "keyword.control");
    scoped(comments[1], "enum", "comment.line.double-slash");
    scoped(comments[3], "comments", "comment.block");
    scoped(comments[3], "mod", "keyword.control.import");

    const attributes = tokenize('#[cfg_attr(feature = "fast", custom([1, { value: "x]" }]))]\npub enum E { #[default] A }');
    scoped(attributes[0], "cfg_attr", "entity.other.attribute-name");
    scoped(attributes[0], "x]", "string.quoted.double");
    scoped(attributes[0], "1", "constant.numeric");
    scoped(attributes[1], "default", "entity.other.attribute-name");
    scoped(tokenize("# [derive(Clone)]")[0], "derive", "entity.other.attribute-name");
    assert.ok(!at(attributes[1], "pub").scopes.includes("meta.attribute.goxide"), "nested attribute delimiters leaked into the next declaration");
    console.log("Goxide TextMate tokenization passed: declarations, literals, Unicode/raw names, contextual words, macros, references, attributes, multiline state.");
  } finally {
    registry.dispose();
  }
}

main().catch(error => { console.error(error); process.exitCode = 1; });
