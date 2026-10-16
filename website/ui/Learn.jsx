import { useId, useState } from "preact/hooks";
import { useLocation } from "preact-iso/router";
import Readonly, { Code, CopyButton, Tabs } from "./Readonly";

function Commands({ commands }) {
  return <div className="command-list">{commands.map((entry, index) => (
    <section className="command-card" key={index}>
      <div className="command-bar"><code>$ {entry.command}</code><CopyButton text={entry.command} label="复制命令" /></div>
      {entry.stdout ? <Code source={entry.stdout} label="CLI 验证输出" /> : <p className="command-success">命令执行成功，无标准输出。</p>}
    </section>
  ))}</div>;
}

function Stage({ stage, previous, next }) {
  const id = useId();
  const [view, setView] = useState("source"), [presetID, setPresetID] = useState(stage.presets?.[0]?.id || "");
  const preset = stage.presets?.find((item) => item.id === presetID);
  const files = preset?.files || stage.files;
  const generated = preset?.generatedFiles || stage.generatedFiles;
  const views = [{ id: "source", title: "源码" }, { id: "generated", title: "生成 Go" }, { id: "output", title: "CLI 输出" }];
  return (
    <article className="tutorial-content">
      <div className="page-kicker"><span className="eyebrow">订单状态 CLI · 循序渐进</span><a href="/docs/?page=start">查阅文档 <span aria-hidden="true">↗</span></a></div>
      <h1>{stage.title}</h1>
      <p className="page-intro">{stage.description}</p>
      <div className="tutorial-actions">
        {stage.download && <a className="button primary" href={`/${stage.download.path}`} download={stage.download.path}>下载此阶段完整项目 <span aria-hidden="true">↓</span></a>}
        <span className="verified-note"><span className="status-dot" />源码、生成 Go 与输出来自同一次 CLI 验证</span>
      </div>
      {stage.presets?.length > 0 && (
        <section className="preset-panel" aria-label="已验证的 feature 配置">
          <div className="preset-heading"><div><h2>切换已验证的配置</h2><p>查看 feature 如何影响源码、依赖和实际输出。</p></div><a href="/docs/?page=features">feature 规则 <span aria-hidden="true">↗</span></a></div>
          <div className="preset-options" role="group" aria-label="feature 预设">
            {stage.presets.map((item) => <button type="button" key={item.id} aria-pressed={presetID === item.id} onClick={() => setPresetID(item.id)}>{item.title}</button>)}
          </div>
          {preset && <>
            <dl className="preset-facts"><div><dt>启用的 feature</dt><dd>{preset.features.length ? preset.features.map((feature) => <code key={feature}>{feature}</code>) : "无"}</dd></div><div><dt>当前依赖</dt><dd>{preset.dependencies.length ? preset.dependencies.map((dependency) => <code key={dependency}>{dependency}</code>) : "无额外依赖"}</dd></div></dl>
            <div className="preset-command"><code>{preset.command}</code><CopyButton text={preset.command} label="复制命令" /></div>
          </>}
        </section>
      )}
      <div className="tutorial-workbench">
        <Tabs id={id} label="快速开始项目视图" items={views} value={view} onChange={setView} />
        <div id={`${id}-panel`} role="tabpanel" aria-labelledby={`${id}-tab-${view}`}>
          {view === "source" && <Readonly key={`${stage.id}-${presetID}-source`} label="快速开始源码" files={files} />}
          {view === "generated" && (generated.length ? <Readonly key={`${stage.id}-${presetID}-generated`} label="CLI 生成的 Go" files={generated} /> : <p className="content-note">这一阶段直接使用原生 Go 源码，无需转译。可以在「源码」中查看。</p>)}
          {view === "output" && <div className="tutorial-output"><h2>CLI 验证输出</h2><p className="caption">以下结果在网站构建时由真实 CLI 执行所得。</p><Commands commands={preset ? [{ command: preset.command, stdout: preset.stdout }] : stage.commands} />{preset && <details className="validation-commands"><summary>查看此项目的完整验证命令</summary><Commands commands={stage.commands} /></details>}</div>}
        </div>
      </div>
      {view !== "output" && <section className="run-preview"><div><h2>运行结果</h2><span>CLI 验证输出</span></div><Code source={preset?.stdout ?? stage.stdout} label="此配置的运行结果" /><button type="button" onClick={() => setView("output")}>查看完整命令与输出 <span aria-hidden="true">→</span></button></section>}
      <div className="tutorial-next-steps"><div><h2>在本地继续</h2><p>下载项目后，在项目目录执行「CLI 输出」中的命令。完整项目的包管理与配置说明可以随时在文档中查阅。</p></div><a href={`/docs/?page=${stage.id === "go" || stage.id === "mixed" ? "go-gen" : "manifest"}`}>阅读对应工作流 <span aria-hidden="true">→</span></a></div>
      <nav className="step-pagination" aria-label="快速开始翻页">
        {previous ? <a href={`/learn/?stage=${previous.id}`}><small>上一步</small><strong>← {previous.title}</strong></a> : <span />}
        {next ? <a href={`/learn/?stage=${next.id}`}><small>下一步</small><strong>{next.title} →</strong></a> : <a href="/docs/?page=features"><small>继续查阅</small><strong>feature 完整规则 →</strong></a>}
      </nav>
    </article>
  );
}

export default function Learn({ manifest }) {
  const location = useLocation(), stages = manifest.tutorials?.stages || [];
  const requested = location.query.stage || stages[0]?.id;
  const index = stages.findIndex((stage) => stage.id === requested), stage = stages[index];
  const [navigationOpen, setNavigationOpen] = useState(false);
  return (
    <section className="learn-page">
      <aside className="section-sidebar tutorial-sidebar" data-open={navigationOpen}>
        <button className="sidebar-toggle" type="button" aria-expanded={navigationOpen} aria-controls="tutorial-navigation" onClick={() => setNavigationOpen(!navigationOpen)}>快速开始步骤 <span aria-hidden="true">{navigationOpen ? "−" : "+"}</span></button>
        <div className="sidebar-content" id="tutorial-navigation"><p className="sidebar-title">从 Go 到 Goxide</p><p className="sidebar-caption">一个案例，四个阶段。</p><nav aria-label="快速开始步骤">{stages.map((item, position) => <a key={item.id} href={`/learn/?stage=${item.id}`} aria-current={item.id === requested ? "step" : undefined} onClick={() => setNavigationOpen(false)}><span className="step-number">{String(position + 1).padStart(2, "0")}</span><span>{item.title}</span></a>)}</nav><div className="sidebar-help"><span>第一次接触 Goxide？</span><a href="/docs/?page=install">先准备本地环境 →</a></div></div>
      </aside>
      {!stages.length ? <p className="content-loading" role="status">正在载入经过验证的快速开始项目…</p> : stage ? <Stage key={stage.id} stage={stage} previous={stages[index - 1]} next={stages[index + 1]} /> : <div className="missing-content" role="alert"><h1>找不到此快速开始阶段。</h1><p>请选择左侧的快速开始步骤。</p><a href="/learn/">从第一步开始 →</a></div>}
    </section>
  );
}
