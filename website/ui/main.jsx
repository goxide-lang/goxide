import { render } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import {
  LocationProvider,
  Router,
  Route,
  useLocation,
} from "preact-iso/router";
import Playground from "./Playground";
import FileView from "./FileView";
import "./style.css";
function Readonly({ label, files }) {
  const [active, setActive] = useState(files[0]?.path || "");
  return (
    <FileView
      label={label}
      files={files}
      activePath={
        files.some((file) => file.path === active) ? active : files[0]?.path
      }
      onSelect={setActive}
    />
  );
}
function Home() {
  return (
    <section>
      <p className="eyebrow">GO 生态 · 显式语义 · 普通 GO 输出</p>
      <h1>
        写得更明确。
        <br />
        <em>仍然是 Go。</em>
      </h1>
      <p className="intro">
        把重复交给宏，把状态交给 enum。Goxide 为熟悉的 Go
        加上有限、可检查的语言能力，生成你能阅读、审查和继续使用的 Go 源码。
      </p>
      <div className="toolbar">
        <a className="button primary" href="/playground.html">
          亲手试一段代码 →
        </a>
        <a className="button" href="/rfc.html">
          了解语言设计 →
        </a>
      </div>
      <p className="caption">
        语言名 Goxide · 命令 goxide · 源文件 .gox · 可执行子集，持续演进。
      </p>
      <div className="language-topics">
        <div>
          <h2>可审查的宏</h2>
          <p>有限捕获、重复与嵌套，明确求值与名字绑定边界。</p>
          <a href="/playground.html?example=macro">看声明宏 →</a>
        </div>
        <div>
          <h2>enum 与 match</h2>
          <p>闭合状态、穷尽检查、guard 与有限表达式组合。</p>
          <a href="/playground.html?example=enum">看状态匹配 →</a>
        </div>
        <div>
          <h2>缺失与失败</h2>
          <p>Option/Result 与显式传播，Go 返回值的静态适配。</p>
          <a href="/playground.html?example=result">看错误传播 →</a>
        </div>
      </div>
      <h2>当前边界</h2>
      <p>
        用户泛型函数已支持单关联协议约束；泛型协议、泛型 impl、多协议组合、完整
        trait、用户过程宏与 LSP 尚未交付。部分 match / ? 组合仍有限。
      </p>
      <p>
        路径只读不等于全局不可变，可写引用不承诺独占；这里不宣称完整 Rust
        借用安全或零分配。
      </p>
      <p>
        浏览器仅转译一个用户包与固定随附依赖，不加载任意模块、不执行
        Go。当前能力以实际编译器清单与 RFC 摘要为准。
      </p>
    </section>
  );
}
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
function Shell({ manifest }) {
  const location = useLocation(),
    routeScroll = useRef(new Map()),
    visible = location.path === "/playground.html";
  const [visited, setVisited] = useState(visible),
    main = useRef(null);
  useLayoutEffect(() => {
    if (visible) setVisited(true);
    const title =
      {
        "/": "语言介绍",
        "/index.html": "语言介绍",
        "/rfc.html": "RFC 特性",
        "/playground.html": "Playground",
        "/local.html": "本地使用",
      }[location.path] || "找不到页面";
    document.title = title + " · Goxide";
    main.current.focus({ preventScroll: true });
    window.scrollTo(
      0,
      !location.wasPush ? routeScroll.current.get(location.url) || 0 : 0,
    );
    const record = () => routeScroll.current.set(location.url, window.scrollY);
    window.addEventListener("scroll", record, { passive: true });
    return () => window.removeEventListener("scroll", record);
  }, [location.url]);
  return (
    <>
      <a className="skip" href="#main">
        跳到主内容
      </a>
      <header>
        <a className="brand" href="/">
          g <span>Goxide</span>
        </a>
        <nav aria-label="主导航">
          <a
            href="/rfc.html"
            aria-current={location.path === "/rfc.html" ? "page" : undefined}
          >
            RFC 特性
          </a>
          <a
            href="/playground.html"
            aria-current={visible ? "page" : undefined}
          >
            Playground
          </a>
          <a
            href="/local.html"
            aria-current={location.path === "/local.html" ? "page" : undefined}
          >
            本地使用
          </a>
        </nav>
      </header>
      <main id="main" tabIndex={-1} ref={main}>
        <Router>
          <Route path="/" component={Home} />
          <Route path="/index.html" component={Home} />
          <Route path="/rfc.html" component={RFC} manifest={manifest} />
          <Route path="/local.html" component={Local} manifest={manifest} />
          <Route path="/playground.html" component={EmptyPage} />
          <NotFound default />
        </Router>
        {(visited || visible) && (
          <Playground manifest={manifest} visible={visible} />
        )}
      </main>
      <footer>
        <a href="/">Goxide · goxide</a>
        <span>
          MPL-2.0 · <a href="/GO-LICENSE.txt">Go runtime license</a> ·{" "}
          <a href="/THIRD-PARTY-LICENSES.txt">前端依赖许可</a>
        </span>
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
const routeScope = /^\/(?:$|(?:index|rfc|playground|local)\.html(?:[?#]|$))/;
render(
  <LocationProvider scope={routeScope}>
    <App />
  </LocationProvider>,
  document.getElementById("root"),
);
