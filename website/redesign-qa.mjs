import assert from "node:assert/strict";
import crypto from "node:crypto";
import fs from "node:fs/promises";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { chromium } from "playwright";
import { done, read, write } from "./monaco-qa.mjs";

const base = (process.env.QA_URL || "http://127.0.0.1:8084").replace(/\/$/, "");
const out = process.env.QA_OUT || fileURLToPath(new URL("qa-artifacts/redesign/", import.meta.url));
const browser = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium", headless: true, chromiumSandbox: true });
try {
  await fs.mkdir(out, { recursive: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 }, permissions: ["clipboard-read", "clipboard-write"], reducedMotion: "reduce" });
  const page = await context.newPage(), errors = [], requests = [];
  page.on("pageerror", (error) => errors.push(error.message));
  context.on("request", (request) => requests.push({ url: request.url(), method: request.method() }));
  const manifest = await (await context.request.get(base + "/examples.json")).json();
  assert.deepEqual(manifest.tutorials.stages.map((stage) => stage.id), ["go", "mixed", "gox", "features"]);
  assert.ok(manifest.docs.pages.length > 0, "documentation payload exists");
  assert.ok(manifest.design.pages.length > 0, "design documentation payload exists");
  const firstRFC = manifest.design.pages.find((item) => item.id.startsWith("rfc-"));
  assert.ok(firstRFC, "design includes RFC pages");
  const stage = (id) => manifest.tutorials.stages.find((item) => item.id === id);

  async function staticFiles(label, files) {
    const pane = page.getByRole("region", { name: label, exact: true });
    await pane.waitFor();
    if (files.length > 1) {
      assert.deepEqual(await pane.locator(".readonly-tree button").allTextContents(), files.map((file) => file.path));
      for (const file of files) {
        await pane.locator(".readonly-tree").getByRole("button", { name: file.path, exact: true }).click();
        assert.equal(await pane.locator(".code-source").textContent(), file.source, label + ": " + file.path);
      }
    } else if (files.length) assert.equal(await pane.locator(".code-source").textContent(), files[0].source);
  }
  async function readingWidth() {
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), "page has no horizontal overflow: " + page.url());
  }
  async function screenshot(name) { await page.screenshot({ path: path.join(out, name + ".png"), fullPage: true }); }

  await page.goto(base + "/");
  await page.getByRole("heading", { level: 1, name: /用 Goxide 表达业务/ }).waitFor();
  assert.deepEqual(await page.locator(".primary-navigation a").allTextContents(), ["首页", "快速开始", "文档", "设计文档"]);
  const hero = page.locator(".hero-code");
  for (const [tab, files] of [["原生 Go", stage("go").files], ["Gox", stage("mixed").files], ["生成 Go", stage("mixed").generatedFiles]]) {
    await hero.getByRole("tab", { name: tab, exact: true }).click();
    const source = await hero.locator(".code-source").textContent();
    assert.ok(files.some((file) => file.source === source), tab + " displays verified source bytes");
    await hero.getByRole("button", { name: "复制代码", exact: true }).click();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), source, "code copy preserves bytes");
  }
  await hero.getByRole("tab", { name: "原生 Go", exact: true }).click();
  await page.keyboard.press("ArrowRight");
  assert.equal(await hero.getByRole("tab", { name: "Gox", exact: true }).getAttribute("aria-selected"), "true", "tabs support arrow navigation");
  await screenshot("home-desktop");
  await page.locator(".hero-copy").getByRole("link", { name: /在 Go 项目中使用/ }).click();
  await page.waitForURL("**/learn/?stage=mixed");
  assert.equal(await page.locator(".tutorial-content h1").textContent(), stage("mixed").title);

  for (const item of manifest.tutorials.stages) {
    await page.getByRole("navigation", { name: "快速开始步骤", exact: true }).getByRole("link", { name: new RegExp(item.title.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")) }).click();
    await page.waitForURL("**/learn/?stage=" + item.id);
    assert.equal(await page.locator(".tutorial-content h1").textContent(), item.title);
    const files = item.presets?.[0]?.files || item.files;
    await staticFiles("快速开始源码", files);
    assert.equal(await page.getByRole("tab", { name: "清单与锁文件", exact: true }).count(), 0, "metadata belongs to the source tree");
    if (["gox", "features"].includes(item.id)) {
      const sourceTree = page.getByRole("region", { name: "快速开始源码", exact: true }).locator(".readonly-tree");
      for (const filename of ["goxide.toml", "goxide.lock"]) {
        await sourceTree.getByRole("button", { name: filename, exact: true }).click();
        assert.equal(await page.getByRole("region", { name: "快速开始源码", exact: true }).locator(".code-source").textContent(), files.find((file) => file.path === filename).source);
      }
    }
    await page.getByRole("tab", { name: "生成 Go", exact: true }).click();
    const generated = item.presets?.[0]?.generatedFiles || item.generatedFiles;
    if (generated.length) await staticFiles("CLI 生成的 Go", generated);
    else await page.getByText("这一阶段直接使用原生 Go 源码，无需转译。可以在「源码」中查看。", { exact: true }).waitFor();
    await page.getByRole("tab", { name: "CLI 输出", exact: true }).click();
    await page.getByRole("heading", { name: "CLI 验证输出", exact: true }).waitFor();
    const download = page.getByRole("link", { name: /下载此阶段完整项目/ });
    assert.equal(await download.getAttribute("href"), "/" + item.download.path);
    const response = await context.request.get(base + "/" + item.download.path);
    assert.equal(response.status(), 200);
    const bytes = await response.body();
    assert.equal(bytes.subarray(0, 2).toString(), "PK");
    assert.equal(crypto.createHash("sha256").update(bytes).digest("hex"), item.download.sha256, "download matches verified archive");
  }
  for (const preset of stage("features").presets) {
    await page.getByRole("group", { name: "feature 预设", exact: true }).getByRole("button", { name: preset.title, exact: true }).click();
    assert.equal(await page.locator(".preset-command > code").textContent(), preset.command);
    await page.locator(".preset-command").getByRole("button", { name: "复制命令", exact: true }).click();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), preset.command, "preset command copy preserves flags");
    assert.equal(await page.locator(".tutorial-output > .command-list .code-source").first().textContent(), preset.stdout);
    await page.getByRole("tab", { name: "生成 Go", exact: true }).click();
    await staticFiles("CLI 生成的 Go", preset.generatedFiles);
    await page.getByRole("tab", { name: "CLI 输出", exact: true }).click();
  }
  await page.getByRole("tab", { name: "源码", exact: true }).click();
  await screenshot("learn-features-desktop");

  await page.getByRole("navigation", { name: "主导航" }).getByRole("link", { name: "文档", exact: true }).click();
  await page.locator(".docs-article h1").waitFor();
  for (const item of manifest.docs.pages) {
    await page.getByRole("navigation", { name: "文档分类", exact: true }).getByRole("link", { name: item.title, exact: true }).click();
    await page.waitForURL("**/docs/?page=" + item.id);
    assert.equal(await page.locator(".docs-article h1").textContent(), item.title);
  }
  await page.keyboard.press("ControlOrMeta+k");
  await page.waitForFunction(() => document.activeElement?.getAttribute("type") === "search");
  assert.equal(await page.getByRole("searchbox", { name: "搜索文档", exact: true }).evaluate((element) => document.activeElement === element), true);
  await page.getByRole("searchbox", { name: "搜索文档", exact: true }).fill("cfg_attr");
  const matches = await page.getByRole("navigation", { name: "文档分类", exact: true }).getByRole("link").count();
  assert.ok(matches > 0 && matches < manifest.docs.pages.length, "search filters document bodies");
  await page.getByRole("searchbox", { name: "搜索文档", exact: true }).fill("no-such-goxide-guide-98324");
  await page.getByRole("status").filter({ hasText: "未找到匹配的文档" }).waitFor();
  await page.getByRole("searchbox", { name: "搜索文档", exact: true }).fill("");
  await page.getByRole("navigation", { name: "文档分类", exact: true }).locator('a[href="/docs/?page=manifest"]').click();
  await page.locator(".docs-article h1").waitFor();
  const tocLink = page.locator(".docs-toc nav a").first(), heading = await tocLink.getAttribute("href");
  await tocLink.click();
  assert.equal(decodeURIComponent(new URL(page.url()).hash), heading);
  assert.equal((await page.reload()).status(), 200);
  await page.waitForFunction((id) => !!document.getElementById(id), heading.slice(1));
  await page.goto(base + "/docs/?page=manifest");
  await page.locator(".docs-article h1").waitFor();
  await screenshot("docs-desktop");
  await page.getByRole("navigation", { name: "主导航" }).getByRole("link", { name: "设计文档", exact: true }).click();
  await page.getByRole("region", { name: "设计文档", exact: true }).waitFor();
  assert.equal(await page.locator(".reader-title").textContent(), "设计文档");
  for (const item of manifest.design.pages) {
    await page.getByRole("navigation", { name: "设计文档分类", exact: true }).locator(`a[href="/design/?page=${item.id}"]`).click();
    await page.waitForURL("**/design/?page=" + item.id);
    assert.equal(await page.locator(".docs-article h1").textContent(), item.title);
    const historical = item.category.replace(/\s/g, "") === "RFC设计" || item.id === "frontend";
    assert.equal(await page.getByRole("complementary", { name: "设计文档阅读说明", exact: true }).count(), historical ? 1 : 0);
    if (historical) {
      for (const href of ["/docs/?page=status", "/design/?page=grammar", "/design/?page=semantics"]) assert.equal(await page.locator(`.design-reading-note a[href="${href}"]`).count(), 1);
    }
    if (["grammar", "semantics"].includes(item.id)) {
      const examples = [...item.markdown.matchAll(/^```[^\n]*\n([\s\S]*?)\n```[ \t]*$/gm)].map((match) => match[1]);
      assert.ok(examples.length > 0, item.id + " provides formal code examples");
      assert.deepEqual(await page.locator(".markdown-code .code-source").allTextContents(), examples, item.id + " preserves grammar and semantic code bytes");
      await page.locator(".markdown-code").first().getByRole("button", { name: "复制代码", exact: true }).click();
      assert.equal(await page.evaluate(() => navigator.clipboard.readText()), examples[0], item.id + " code copy preserves formal notation");
    }
  }
  await page.getByRole("navigation", { name: "设计文档分类", exact: true }).locator('a[href="/design/?page=rfcs"]').click();
  await page.locator(`.markdown-content a[href="/design/?page=${firstRFC.id}"]`).click();
  await page.waitForURL("**/design/?page=" + firstRFC.id);
  assert.equal(await page.locator(".docs-article h1").textContent(), firstRFC.title, "RFC index opens its complete design page");
  await page.getByRole("searchbox", { name: "搜索设计文档", exact: true }).fill(firstRFC.title);
  await page.getByRole("navigation", { name: "设计文档分类", exact: true }).locator(`a[href="/design/?page=${firstRFC.id}"]`).waitFor();
  assert.ok(await page.getByRole("navigation", { name: "设计文档分类", exact: true }).getByRole("link").count() < manifest.design.pages.length, "design search filters its own collection");
  await page.getByRole("navigation", { name: "主导航" }).getByRole("link", { name: "文档", exact: true }).click();
  await page.getByRole("region", { name: "使用参考", exact: true }).waitFor();
  assert.equal(await page.getByRole("searchbox", { name: "搜索文档", exact: true }).inputValue(), "", "design search does not leak into usage docs");
  assert.equal(await page.getByRole("navigation", { name: "文档分类", exact: true }).getByRole("link").count(), manifest.docs.pages.length);
  await page.goto(base + "/design/?page=grammar");
  await page.locator(".docs-article h1").waitFor();
  const designAnchor = await page.locator(".docs-toc nav a").first().getAttribute("href");
  await page.locator(".docs-toc nav a").first().click();
  assert.equal(decodeURIComponent(new URL(page.url()).hash), designAnchor);
  assert.equal((await page.reload()).status(), 200);
  await page.waitForFunction((id) => !!document.getElementById(id), designAnchor.slice(1));
  await page.goto(base + "/design/?page=overview");
  await page.locator(".docs-article h1").waitFor();
  await screenshot("design-desktop");
  for (const route of ["/learn/?stage=gox", "/docs/?page=features", "/design/?page=" + firstRFC.id, "/rfc.html?feature=traits", "/local.html"]) {
    assert.equal((await page.goto(base + route)).status(), 200, route);
    await page.getByRole("navigation", { name: "主导航" }).waitFor();
    assert.equal((await page.reload()).status(), 200, route + " reload");
    await page.getByRole("navigation", { name: "主导航" }).waitFor();
  }
  assert.ok(!requests.some((request) => /(?:MonacoPane|Playground-.*\.js|editor\.worker|compiler\.wasm)/.test(request.url)), "static routes never load the editor or compiler");

  for (const width of [900, 390, 320]) {
    await page.setViewportSize({ width, height: 900 });
    for (const [route, name] of [["/", "home"], ["/learn/?stage=features", "learn"], ["/design/?page=overview", "design"], ["/docs/?page=manifest", "docs"]]) {
      await page.goto(base + route);
      await page.getByRole("navigation", { name: "主导航" }).waitFor();
      await readingWidth();
      if (width === 390) await screenshot(name + "-mobile");
    }
    await page.getByRole("button", { name: /文档导航与搜索/ }).click();
    await page.getByRole("searchbox", { name: "搜索文档", exact: true }).fill("cfg_attr");
    assert.ok(await page.getByRole("navigation", { name: "文档分类", exact: true }).getByRole("link").count());
    await readingWidth();
    await page.goto(base + "/design/?page=overview");
    await page.getByRole("button", { name: /设计文档导航与搜索/ }).click();
    await page.getByRole("searchbox", { name: "搜索设计文档", exact: true }).fill(firstRFC.title);
    await page.getByRole("navigation", { name: "设计文档分类", exact: true }).locator(`a[href="/design/?page=${firstRFC.id}"]`).click();
    assert.equal(await page.getByRole("button", { name: /设计文档导航与搜索/ }).getAttribute("aria-expanded"), "false");
    await readingWidth();
    await page.goto(base + "/learn/?stage=gox");
    await page.getByRole("button", { name: /快速开始步骤/ }).click();
    await page.getByRole("navigation", { name: "快速开始步骤", exact: true }).getByRole("link").first().click();
    assert.equal(await page.getByRole("button", { name: /快速开始步骤/ }).getAttribute("aria-expanded"), "false");
    await readingWidth();
  }

  const safe = await context.newPage();
  await safe.route("**/examples.json", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify({ ...manifest, docs: { pages: [{ id: "safe", category: "验证", title: "安全渲染", markdown: '# 安全渲染\n\n<script>window.unsafeMarkdown = true</script>\n\n[危险链接](javascript:alert%281%29)\n\n## 标题\n\n- 第一项\n  - 嵌套项\n\n| 字段 | 说明 |\n| --- | --- |\n| `a|b` | 普通文本 |\n\n```html\n<img onerror="alert(1)">\n```' }] } }) }));
  await safe.goto(base + "/docs/?page=safe");
  await safe.getByRole("heading", { name: "安全渲染", exact: true }).waitFor();
  assert.equal(await safe.locator(".markdown-content script, .markdown-content img, .markdown-content a[href^='javascript:']").count(), 0);
  assert.equal(await safe.evaluate(() => window.unsafeMarkdown), undefined);
  assert.equal(await safe.locator(".markdown-content li li").textContent(), "嵌套项");
  assert.equal(await safe.locator("tbody td").first().textContent(), "a|b");
  await safe.close();

  await page.setViewportSize({ width: 1440, height: 1000 });
  await page.goto(base + "/playground.html?example=macro");
  await done(page);
  const original = manifest.examples.find((example) => example.id === "macro");
  assert.equal(await read(page, "生成结果"), original.go);
  const draft = original.source + "\n// tutorial navigation draft\n";
  await write(page, draft); await done(page);
  await page.evaluate(() => window.redesignDocumentMarker = "same document");
  await page.getByRole("navigation", { name: "主导航" }).getByRole("link", { name: "文档", exact: true }).click();
  await page.locator(".docs-article h1").waitFor();
  await page.getByRole("navigation", { name: "主导航" }).getByRole("link", { name: /^Playground/ }).click();
  assert.equal(await read(page, "源码"), draft, "Playground draft survives usage reference navigation");
  await page.getByRole("navigation", { name: "主导航" }).getByRole("link", { name: "设计文档", exact: true }).click();
  await page.locator(".docs-article h1").waitFor();
  await page.getByRole("navigation", { name: "主导航" }).getByRole("link", { name: /^Playground/ }).click();
  assert.equal(await read(page, "源码"), draft, "Playground draft survives design documentation navigation");
  assert.equal(await page.evaluate(() => window.redesignDocumentMarker), "same document");
  assert.deepEqual(errors, []);
  assert.ok(requests.every((request) => request.url.startsWith(base + "/") && request.method === "GET"), "same-origin GET requests only");
  console.log(JSON.stringify({ url: base, stages: manifest.tutorials.stages.length, presets: stage("features").presets.length, docs: manifest.docs.pages.length, design: manifest.design.pages.length, sourceIncludesMetadata: true, codeSourceParity: true, formalExamples: true, rfcNavigation: true, downloads: true, localSearch: true, safeMarkdown: true, keyboardTabs: true, staticRoutesLazy: true, mobileWidths: [900, 390, 320], deepLinksReload: true, playgroundDraft: true, errors, screenshots: out }, null, 2));
} finally {
  await browser.close();
}
