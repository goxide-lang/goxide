import { render } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import {
  LocationProvider,
  Router,
  Route,
  useLocation,
} from "preact-iso/router";
import Home from "./Home";
import Learn from "./Learn";
import Docs from "./Docs";
import Readonly from "./Readonly";
import "./style.css";
function RFC({ manifest }) {
  const location = useLocation(),
    itemId = location.query.feature || manifest.catalog.items[0].id;
  const item = manifest.catalog.items.find((item) => item.id === itemId);
  return (
    <section>
      <p className="eyebrow">RFC 特性 · 当前编译器的公开摘要</p>
      <h1>从设计到今天能做什么。</h1>
      <p className="provenance">
        {manifest.compilerSource.kind === "worktree"
          ? `工作树 · 基线 ${manifest.compilerSource.baseCommit} · SHA-256 ${manifest.compilerSource.digest}`
          : `编译器 ${manifest.compilerSource.commit}`}
        {` · 历史证据基线 ${manifest.catalogCompilerCommit} · 复核 ${manifest.catalog.reviewedAt}`}
      </p>
      <div className="rfc-browser">
        <nav id="rfc-list" aria-label="RFC 列表">
          {manifest.catalog.items.map((row) => (
            <a
              key={row.id}
              href={"/rfc.html?feature=" + encodeURIComponent(row.id)}
              aria-current={row.id === itemId ? "page" : undefined}
            >
              {row.rfcs.map((id) => "RFC " + id).join(" / ")} · {row.title}
              <small>{manifest.catalog.statusLabels[row.status]}</small>
            </a>
          ))}
        </nav>
        <article id="rfc-content">
          {!item ? (
            <p role="alert">找不到此 RFC 条目。</p>
          ) : (
            <>
              <h2>{item.title}</h2>
              <p>
                {manifest.catalog.statusLabels[item.status]} ·{" "}
                {
                  {
                    browser: "浏览器可转译",
                    cli: "仅 CLI 工程",
                    unavailable: "设计 / 开发中",
                  }[item.support]
                }
              </p>
              <p>{item.summary}</p>
              <h3>设计与用法</h3>
              <p>{item.usage}</p>
              <p>边界：{item.limitation}</p>
              {item.development && <p>开发快照：{item.development}</p>}
              {item.snippet && (
                <>
                  <h3>{item.snippet.language}</h3>
                  <Readonly
                    key={item.id + "-snippet"}
                    label="设计示例"
                    files={[{ path: "example", source: item.snippet.text }]}
                  />
                  <p>{item.snippet.note}</p>
                </>
              )}
              {item.examples.map((id) => {
                const example = manifest.examples.find(
                  (example) => example.id === id,
                );
                return (
                  <div key={item.id + "-" + id}>
                    <h3>{example.title}</h3>
                    <a href={"/playground.html?example=" + id}>
                      打开 Playground →
                    </a>
                    <Readonly
                      label="RFC 示例"
                      files={[
                        { path: "playground.gox", source: example.source },
                        { path: "playground.go", source: example.go },
                      ]}
                    />
                    <pre>
                      CLI 运行输出（浏览器不执行）{"\n" + example.stdout}
                    </pre>
                  </div>
                );
              })}
              {item.checks.map((id) => {
                const check = manifest.catalog.checks.find(
                  (check) => check.id === id,
                );
                return (
                  <div key={item.id + "-" + id}>
                    <h3>拒绝例：{id}</h3>
                    <Readonly
                      label="拒绝例"
                      files={[
                        { path: "rejected.gox", source: check.source },
                        { path: "diagnostic.txt", source: check.diagnostic },
                      ]}
                    />
                  </div>
                );
              })}
            </>
          )}
        </article>
      </div>
      <aside className="caption">
        {manifest.catalog.rules.map((rule) => (
          <p key={rule}>{rule}</p>
        ))}
      </aside>
    </section>
  );
}
function Local({ manifest }) {
  return (
    <section>
      <p className="eyebrow">CLI 工程 · 浏览器外运行</p>
      <h1>从单文件走向工程。</h1>
      <p>
        网站构建时由当前 CLI 转译并运行以下快照。它们不属于浏览器的可编辑输入。
      </p>
      <p>
        工程使用 goxide.toml 声明包与依赖，goxide.lock 固定依赖版本。
        当前支持多包程序、Go 与本地 path 依赖、feature 条件配置，以及 build、run、test。
        workspace、import-go、vendor 和构建脚本入口将在 MVP 阶段重新开放。
      </p>
      <p>
        已有 Go 包可用 <code>goxide gen .</code> 或 <code>go generate</code>
        生成源码，目标跟随有效 Go 环境。工具链模式默认宿主，使用
        <code> --target linux/amd64</code> 可跨目标生成或构建；run/test 仅接受宿主目标。
        <code> goxide env --json</code> 可查看环境和目标，源码通过 cfg、cfg_attr
        与 cfg! 查询 feature、target_* 和原始 env。
      </p>
      <div id="cli-example">
        {manifest.cliExamples.map((example) => (
          <div key={example.id}>
            <h2>{example.title}</h2>
            <Readonly
              label="CLI 源码与结果"
              files={example.files.flatMap((file) => [
                { path: "source/" + file.path, source: file.source },
                {
                  path: "generated/" + file.path.replace(/\.gox$/, ".go"),
                  source: file.go,
                },
              ])}
            />
            <pre>{example.stdout}</pre>
          </div>
        ))}
      </div>
      <div id="project-examples">
        {manifest.projectExamples.map((project) => (
          <div key={project.id}>
            <h2>{project.title}</h2>
            {project.note && <p>{project.note}</p>}
            {project.download && (
              <p>
                <a
                  href={"/" + project.download.path}
                  download={project.download.path}
                >
                  下载完整工程 ZIP
                </a>
              </p>
            )}
            <Readonly label="工程快照" files={project.files} />
            {project.runs.map((run, index) => (
              <div key={index}>
                <p className="provenance">{run.command}</p>
                <pre>{run.stdout}</pre>
              </div>
            ))}
          </div>
        ))}
      </div>
    </section>
  );
}
function EmptyPage() {
  return null;
}
function NotFound() {
  return (
    <section>
      <h1>找不到此页面。</h1>
    </section>
  );
}
function PlaygroundLoader({ manifest, visible }) {
  const [Page, setPage] = useState(null), [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    import("./Playground.jsx")
      .then((module) => { if (active) setPage(() => module.default); })
      .catch((error) => { if (active) setError(String(error)); });
    return () => { active = false; };
  }, []);
  return Page ? <Page manifest={manifest} visible={visible} /> : visible ? (
    <section className="playground-loading">
      <h1>Playground</h1>
      {error ? <p role="alert">Playground 加载失败，请刷新重试：{error}</p> : <p role="status">正在加载 Playground…</p>}
    </section>
  ) : null;
}
function Shell({ manifest }) {
  const location = useLocation(),
    routeScroll = useRef(new Map()),
    visible = location.path === "/playground.html";
  const [visited, setVisited] = useState(visible), main = useRef(null);
  useLayoutEffect(() => {
    if (visible) setVisited(true);
    const title = {
      "/": "用 Goxide 表达业务，生成你熟悉的 Go",
      "/index.html": "用 Goxide 表达业务，生成你熟悉的 Go",
      "/learn": manifest.tutorials?.stages.find((stage) => stage.id === location.query.stage)?.title || "快速开始",
      "/docs": (manifest.docs?.pages.find((page) => page.id === location.query.page)?.title || "文档") + " · 使用参考",
      "/design": (manifest.design?.pages.find((page) => page.id === location.query.page)?.title || "总览") + " · 设计文档",
      "/rfc.html": "RFC 特性",
      "/playground.html": "Playground",
      "/local.html": "本地使用",
    }[location.path] || "找不到页面";
    document.title = title + " · Goxide";
    main.current.focus({ preventScroll: true });
    window.scrollTo(0, !location.wasPush ? routeScroll.current.get(location.url) || 0 : 0);
    const frame = requestAnimationFrame(() => {
      if (window.location.hash) {
        try { document.getElementById(decodeURIComponent(window.location.hash.slice(1)))?.scrollIntoView(); } catch {}
      }
    });
    const record = () => routeScroll.current.set(location.url, window.scrollY);
    window.addEventListener("scroll", record, { passive: true });
    return () => { cancelAnimationFrame(frame); window.removeEventListener("scroll", record); };
  }, [location.url]);
  return (
    <>
      <a className="skip" href="#main">跳到主内容</a>
      <header className="site-header">
        <a className="brand" href="/" aria-label="Goxide 首页"><span className="brand-mark" aria-hidden="true">g</span><span>Goxide</span></a>
        <nav aria-label="主导航">
          <div className="primary-navigation">
            <a href="/" aria-current={["/", "/index.html"].includes(location.path) ? "page" : undefined}>首页</a>
            <a href="/learn/" aria-current={["/learn", "/local.html"].includes(location.path) ? "page" : undefined}>快速开始</a>
            <a href="/docs/" title="使用参考" aria-current={location.path === "/docs" ? "page" : undefined}>文档</a>
            <a href="/design/" aria-current={["/design", "/rfc.html"].includes(location.path) ? "page" : undefined}>设计文档</a>
          </div>
          <div className="secondary-navigation">
            <a href="/playground.html" aria-current={visible ? "page" : undefined}>Playground <span aria-hidden="true">↗</span></a>
            <a className="github-link" href="https://github.com/goxide-lang/goxide">GitHub <span aria-hidden="true">↗</span></a>
          </div>
        </nav>
      </header>
      <main id="main" tabIndex={-1} ref={main}>
        <Router>
          <Route path="/" component={Home} manifest={manifest} />
          <Route path="/index.html" component={Home} manifest={manifest} />
          <Route path="/learn" component={Learn} manifest={manifest} />
          <Route path="/docs" component={Docs} manifest={manifest} />
          <Route path="/design" component={Docs} manifest={manifest} kind="design" />
          <Route path="/rfc.html" component={RFC} manifest={manifest} />
          <Route path="/local.html" component={Local} manifest={manifest} />
          <Route path="/playground.html" component={EmptyPage} />
          <NotFound default />
        </Router>
        {(visited || visible) && <PlaygroundLoader manifest={manifest} visible={visible} />}
      </main>
      <footer className="site-footer">
        <div><a className="footer-brand" href="/">Goxide</a><p>写下业务意图，生成普通 Go。</p><small>pre-1.0 · 网站构建 {manifest.websiteCommit.slice(0, 7)}</small></div>
        <nav aria-label="更多资源"><a href="/learn/">快速开始</a><a href="/docs/">使用参考</a><a href="/design/">设计文档</a><a href="/rfc.html" aria-current={location.path === "/rfc.html" ? "page" : undefined}>RFC 特性</a><a href="/local.html" aria-current={location.path === "/local.html" ? "page" : undefined}>本地使用</a></nav>
        <div className="footer-licenses"><span>MPL-2.0</span><a href="/GO-LICENSE.txt">Go runtime license</a><a href="/THIRD-PARTY-LICENSES.txt">前端依赖许可</a></div>
      </footer>
    </>
  );
}
function App() {
  const [manifest, setManifest] = useState(null),
    [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    (async () => {
      const response = await fetch("/examples.json", {
        cache: "no-store",
        signal: controller.signal,
      });
      if (!response.ok) throw new Error("示例清单 HTTP " + response.status);
      const data = await response.json(),
        context = data.dependencyContext,
        source = data.compilerSource;
      if (
        !/^[a-f0-9]{64}$/.test(data.wasmSha256 || "") ||
        !/^[a-f0-9]{64}$/.test(source?.digest || "") ||
        !["commit", "worktree"].includes(source?.kind) ||
        typeof source.dirty !== "boolean" ||
        source.digest !== data.compilerDigest ||
        (source.kind === "commit" ? source.commit : source.baseCommit) !== data.commit ||
        !/^[a-f0-9]{40}$/.test(data.commit || "") ||
        !context?.source ||
        !context.sha256
      )
        throw new Error("编译器清单缺少完整性信息");
      const hash = Array.from(
        new Uint8Array(
          await crypto.subtle.digest(
            "SHA-256",
            new TextEncoder().encode(context.source),
          ),
        ),
        (b) => b.toString(16).padStart(2, "0"),
      ).join("");
      if (
        hash !== context.sha256 ||
        data.catalog.compilerCommit !== data.catalogCompilerCommit
      )
        throw new Error("清单版本或项目依赖根不匹配");
      setManifest(data);
    })().catch((error) => {
      if (error.name !== "AbortError") setError(String(error));
    });
    return () => controller.abort();
  }, []);
  return error ? (
    <main>
      <h1>网站数据暂时不可用</h1>
      <p role="alert">{error}</p>
    </main>
  ) : manifest ? (
    <Shell manifest={manifest} />
  ) : (
    <main>
      <p role="status">正在载入网站数据…</p>
    </main>
  );
}
// Only app routes are intercepted. Resource/license links retain normal navigation.
const routeScope = /^\/(?:$|(?:index|rfc|playground|local)\.html(?:[?#]|$)|(?:learn|docs|design)\/?(?:[?#]|$))/;
render(
  <LocationProvider scope={routeScope}>
    <App />
  </LocationProvider>,
  document.getElementById("root"),
);
