import { useId, useState } from "preact/hooks";
import Readonly, { Code, CopyButton, Tabs } from "./Readonly";

function modelFile(files, extension) {
  const candidates = (files || []).filter((file) => file.path.endsWith(extension) && !file.path.endsWith(`_test${extension}`));
  return candidates.find((file) => /(?:model|order|state)/.test(file.path) && !/(?:^|\/)main\./.test(file.path)) || candidates[0];
}

function Comparison({ stages }) {
  const id = useId(), [active, setActive] = useState("gox");
  const go = stages.find((stage) => stage.id === "go"), mixed = stages.find((stage) => stage.id === "mixed");
  const source = modelFile(mixed?.files, ".gox");
  const generated = mixed?.generatedFiles?.find((file) => file.path.endsWith(source?.path.replace(/\.gox$/, ".go"))) || modelFile(mixed?.generatedFiles, ".go");
  const choices = [
    { id: "go", title: "原生 Go", file: modelFile(go?.files, ".go") },
    { id: "gox", title: "Gox", file: source },
    { id: "generated", title: "生成 Go", file: generated },
  ];
  const selected = choices.find((choice) => choice.id === active);
  return (
    <div className="hero-code">
      <div className="code-window-title"><span className="window-dots" aria-hidden="true">● ● ●</span><span>同一个订单状态，三种视角</span></div>
      <Tabs id={id} label="订单状态代码对照" items={choices} value={active} onChange={setActive} />
      <div id={`${id}-panel`} role="tabpanel" aria-labelledby={`${id}-tab-${active}`}>
        {selected.file ? <>
          <div className="readonly-filebar"><span>{selected.file.path}</span><CopyButton text={selected.file.source} /></div>
          <Code source={selected.file.source} label={`${selected.title} 订单状态示例`} />
        </> : <p className="loading" role="status">正在载入经过 CLI 验证的订单示例…</p>}
      </div>
      <div className="code-window-caption"><span className="status-dot" />真实项目源码 · 生成结果经过构建与测试</div>
    </div>
  );
}

const capabilities = [
  { id: "enum", number: "01", title: "让状态成为类型", text: "用 enum 表达业务的不同状态，用 match 一次写全每一种处理。", link: "enum 与 match", start: /^(?:pub\s+)?enum\s/ },
  { id: "result", number: "02", title: "让缺失与失败可见", text: "Option 与 Result 把分支写进返回值；传播语法保持成功路径的连贯。", link: "Option 与 Result", start: /^func\s/ },
  { id: "macro", number: "03", title: "把重复留给编译器", text: "声明宏和显式派生处理重复结构，生成的 Go 仍可直接阅读与审查。", link: "宏与派生", start: /^macro/ },
];

export default function Home({ manifest }) {
  const stages = manifest.tutorials?.stages || [];
  const project = stages.find((stage) => stage.id === "gox");
  const config = project?.files.find((file) => file.path === "goxide.toml" || file.path.endsWith("/goxide.toml"));
  return (
    <section className="home-page">
      <div className="hero">
        <div className="hero-copy">
          <p className="eyebrow">表达更清晰，沿用 Go 生态</p>
          <h1>用 <span>Goxide</span> <span className="hero-verb">表达业务，</span><em>生成你熟悉的 Go。</em></h1>
          <p className="hero-description">用 enum、match、宏与派生写下意图。生成普通 Go 源码，接入已有项目，也可以从一个完整的 Goxide 项目开始。</p>
          <div className="hero-actions">
            <a className="button primary" href="/learn/?stage=mixed">在 Go 项目中使用 <span aria-hidden="true">↗</span></a>
            <a className="button" href="/learn/?stage=gox">创建 Goxide 项目 <span aria-hidden="true">→</span></a>
          </div>
          <p className="hero-note"><code>.gox</code> 编写 · <code>.go</code> 生成 · 熟悉的工具链</p>
          <p className="hero-download"><a href="/docs/?page=install">下载 CLI v0.0.1 实验版 <span aria-hidden="true">→</span></a><span>配套 Go 1.27.2</span></p>
        </div>
        <Comparison stages={stages} />
      </div>

      <div className="proof-strip" aria-label="Goxide 工作方式">
        <span>源码可读、可审查</span><span>Go 依赖直接使用</span><span>从一个包逐步接入</span><span>CLI 构建、运行与测试</span>
      </div>

      <section className="home-section" aria-labelledby="capabilities-title">
        <div className="section-heading"><div><p className="eyebrow">语言，为日常业务服务</p><h2 id="capabilities-title">少一点样板，多一点意图。</h2></div><a href="/docs/?page=language">查阅语言能力 <span aria-hidden="true">→</span></a></div>
        <div className="capability-grid">
          {capabilities.map((capability) => {
            const example = manifest.examples.find((item) => item.id === capability.id);
            const lines = example?.source.trimEnd().split("\n") || [];
            const start = Math.max(0, lines.findIndex((line) => capability.start.test(line)));
            return <article className="capability-card" key={capability.id}>
              <span className="card-number">{capability.number}</span><h3>{capability.title}</h3><p>{capability.text}</p>
              {example ? <Code source={lines.slice(start, start + 9).join("\n")} label={`${capability.link} 示例片段`} /> : <p role="status">示例正在加载…</p>}
              <a href={`/playground.html?example=${capability.id}`}>试试{capability.link} <span aria-hidden="true">↗</span></a>
            </article>;
          })}
        </div>
      </section>

      <section className="home-section" aria-labelledby="workflows-title">
        <div className="section-heading"><div><p className="eyebrow">按你的项目，选择起点</p><h2 id="workflows-title">已有 Go 项目，或全新的开始。</h2></div></div>
        <div className="workflow-grid">
          <a className="workflow-card" href="/learn/?stage=mixed"><span className="workflow-label">路线 A · 渐进接入</span><h3>保留 Go 项目的工作方式。</h3><p>把一个业务模型改成 Gox，生成 Go 后继续用已有的包、测试与构建命令。</p><div className="workflow-chain"><span>Go 项目</span><b aria-hidden="true">→</b><span>加入 .gox</span><b aria-hidden="true">→</b><span>go generate</span></div><strong>在 Go 项目中使用 <span aria-hidden="true">→</span></strong></a>
          <a className="workflow-card tinted" href="/learn/?stage=gox"><span className="workflow-label">路线 B · 完整项目</span><h3>从清单到运行，一路写 Gox。</h3><p>用 goxide.toml 组织包、入口与依赖。通过同一套 CLI 生成、构建、运行和测试。</p><div className="workflow-chain"><span>goxide.toml</span><b aria-hidden="true">→</b><span>.gox 源码</span><b aria-hidden="true">→</b><span>build / run / test</span></div><strong>创建 Goxide 项目 <span aria-hidden="true">→</span></strong></a>
        </div>
      </section>

      <section className="project-preview home-section" aria-labelledby="project-title">
        <div><p className="eyebrow">从一段代码，到一个工程</p><h2 id="project-title">项目的全貌，都看得见。</h2><p>同一个订单状态 CLI，贯穿原生 Go、混合项目和完整 Gox。继续学习依赖、可选 feature、锁文件与离线运行。</p><a className="text-link" href="/learn/?stage=features">项目与 feature：快速开始 <span aria-hidden="true">→</span></a>
          {project && <div className="project-file-list" aria-label="完整 Gox 项目文件">{project.files.map((file) => <code key={file.path}>{file.path}</code>)}</div>}
        </div>
        {config ? <Readonly label="项目清单" files={[config]} compact /> : <div className="content-loading" role="status">正在载入完整项目清单…</div>}
      </section>

      <section className="try-banner"><div><p className="eyebrow">先试一段，再开始</p><h2>打开浏览器，看 Go 如何生成。</h2><p>真实编译器在本地浏览器中转译，源码留在你的设备上。</p></div><a className="button primary" href="/playground.html?example=enum">打开 Playground <span aria-hidden="true">↗</span></a></section>

      <section className="home-status"><div><span className="status-pill">v0.0.1 · 实验性预发布</span><h2>从当前能力开始。</h2><p>尚非正式稳定发行。后续 patch/minor 更新仍可能对语言、CLI、配置格式和生成代码作出不兼容变更。使用参考记录当前用法与支持范围，设计文档解释语言规则与设计背景。</p></div><div><a href="https://github.com/goxide-lang/goxide/releases/tag/v0.0.1">v0.0.1 版本说明 <span aria-hidden="true">↗</span></a><a href="/docs/?page=status">能力与状态 <span aria-hidden="true">→</span></a><a href="/design/">设计文档 <span aria-hidden="true">→</span></a><a href="https://github.com/goxide-lang/goxide">在 GitHub 查看源码 <span aria-hidden="true">↗</span></a></div></section>
    </section>
  );
}
