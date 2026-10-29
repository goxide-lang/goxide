import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { useLocation } from "preact-iso/router";
import FileView from "./FileView";
import { compiler, compileTimeoutMs, diagnostics } from "./compiler";
const layouts = [
  { value: "horizontal", label: "左右并排", icon: "split-horizontal" },
  { value: "vertical", label: "上下排列", icon: "split-vertical" },
  { value: "source", label: "仅源码", icon: "layout-sidebar-right-off" },
  { value: "result", label: "仅结果", icon: "layout-sidebar-left-off" },
];
export default function Playground({ manifest, visible }) {
  const examples = [...manifest.examples, ...(manifest.packageExamples || [])];
  const first = examples[0];
  const [selected, setSelected] = useState(first.id),
    [files, setFiles] = useState(
      first.files || [{ path: "playground.gox", source: first.source }],
    ),
    [active, setActive] = useState(
      (first.files || [])[0]?.path || "playground.gox",
    ),
    [packageMode, setPackage] = useState(!!first.files);
  const [result, setResult] = useState({
      status: "正在加载编译器…",
      files: [],
    }),
    [generated, setGenerated] = useState(""),
    [layout, setLayout] = useState("horizontal"),
    [openMenu, setOpenMenu] = useState(""),
    [newName, setNewName] = useState(""),
    [creating, setCreating] = useState(false),
    [position, setPosition] = useState(null),
    [fileError, setFileError] = useState(""),
    [generation, setGeneration] = useState(0);
  const service = useRef(null),
    actions = useRef(null),
    filename = useRef(null),
    location = useLocation();
  const selectedRef = useRef(selected);
  selectedRef.current = selected;
  useLayoutEffect(() => {
    if (!openMenu) return;
    function dismiss(event) {
      if (event.type === "keydown" && event.key === "Escape") {
        actions.current?.querySelector('[aria-expanded="true"]')?.focus();
        setOpenMenu("");
      } else if (event.type === "pointerdown" && !actions.current?.contains(event.target)) setOpenMenu("");
    }
    document.addEventListener("pointerdown", dismiss);
    document.addEventListener("keydown", dismiss);
    return () => {
      document.removeEventListener("pointerdown", dismiss);
      document.removeEventListener("keydown", dismiss);
    };
  }, [openMenu]);
  useLayoutEffect(() => {
    service.current = compiler(manifest, setResult);
    return () => service.current.dispose();
  }, [manifest]);
  useLayoutEffect(() => {
    service.current.update({ files, packageMode });
  }, [files, packageMode]);
  function updateFiles(next) {
    service.current.invalidate();
    setFiles(next);
  }
  function choose(id) {
    const example = examples.find((example) => example.id === id);
    if (!example) return;
    const next = example.files
      ? example.files.map((file) => ({ ...file }))
      : [{ path: "playground.gox", source: example.source }];
    setSelected(id);
    updateFiles(next);
    setActive(next[0].path);
    setPackage(!!example.files);
    setGenerated("");
    setFileError("");
    setCreating(false);
    setPosition(null);
    setGeneration((value) => value + 1);
  }
  const requested = location.query.example;
  useEffect(() => {
    if (visible && requested && requested !== selectedRef.current)
      choose(requested);
  }, [visible, requested]);
  const valid =
    !requested || examples.some((example) => example.id === requested);
  function create() {
    if (!newName) {
      filename.current.focus();
      return;
    }
    let error = "";
    if (
      !/^[A-Za-z][A-Za-z0-9_-]*\.gox$/.test(newName) ||
      newName.endsWith("_test.gox") ||
      newName.startsWith("goxide_")
    )
      error =
        "请输入普通 .gox 文件名（字母开头，无目录；不接受测试或保留文件名）。";
    else if (
      files.some((file) => file.path.toLowerCase() === newName.toLowerCase())
    )
      error = "文件名已存在。";
    else if (files.length >= 16) error = "最多 16 个同包文件。";
    setFileError(error);
    if (error) return;
    const packageName =
      files
        .map(
          (file) =>
            file.source
              .replace(/\/\*[\s\S]*?\*\/|\/\/[^\n]*/g, " ")
              .match(/^\s*package\s+((?:r#)?[\p{L}_][\p{L}\p{Nd}_]*)/mu)?.[1],
        )
        .find(Boolean) || "main";
    updateFiles([
      ...files,
      { path: newName, source: `package ${packageName}\n` },
    ]);
    setActive(newName);
    setPackage(true);
    setNewName("");
    setCreating(false);
  }
  const diagnosticText = [result.messages, result.error].filter(Boolean).join("\n");
  const outputPath = result.files.some((file) => file.path === generated)
    ? generated
    : result.files[0]?.path || "";
  const dependencyFiles = [
    {
      path: manifest.dependencyContext.path,
      source: manifest.dependencyContext.source,
      provenance: `SHA-256 ${manifest.dependencyContext.sha256}`,
    },
  ];
  for (const dependency of manifest.dependencies || [])
    for (const file of dependency.files)
      dependencyFiles.push({
        path: dependency.package + "/" + file.path,
        source: file.source,
        provenance: `${dependency.staticPaths.join(", ")} → ${dependency.package} @ ${dependency.version} · ${dependency.source} · SHA-256 ${file.sha256}`,
      });
  const example = examples.find((example) => example.id === selected);
  const sourceFiles = [
    ...files,
    ...(example.macroSources || []).map((file) => ({ ...file, readonly: true, group: "随附宏源码 · 只读" })),
    ...dependencyFiles.map((file) => ({ ...file, readonly: true, group: "依赖源码 · 只读" })),
  ];
  const markers = diagnostics(diagnosticText, files);
  function showImplementation() {
    setActive(example.macroEntryPath || example.macroSources[0].path);
    if (layout === "result") setLayout("horizontal");
  }
  function jump(marker) {
    setActive(marker.path);
    setPosition({ ...marker });
    if (layout === "result") setLayout("source");
  }
  return (
    <section id="playground" hidden={!visible}>
      {!valid && <p role="alert">找不到此示例，请选择下列示例。</p>}
      <header className="playground-heading">
        <h1>{example.macroSources ? "过程宏，一次看懂调用与实现。" : "改一行，看 Go 如何生成。"}</h1>
        <p>编辑多文件源码，独立浏览生成 Go。转译在浏览器中完成，不上传源码。</p>
      </header>
      <div className="ide-workspace">
        <div className="ide-toolbar">
          <label>示例<select id="examples" value={selected} onChange={(event) => {
            choose(event.target.value);
            location.route("/playground.html?example=" + encodeURIComponent(event.target.value), true);
          }}>{examples.map((item) => <option key={item.id} value={item.id}>{item.title}</option>)}</select></label>
          <button className="primary" id="compile" onClick={() => service.current.compile()}><span aria-hidden="true" className="codicon codicon-play" />转译 <kbd>⌘ / Ctrl Enter</kbd></button>
          {example.macroSources && <button onClick={showImplementation}><span aria-hidden="true" className="codicon codicon-code" />查看宏实现</button>}
          {example.macroDownload && <a className="button" href={"/" + example.macroDownload.path} download><span aria-hidden="true" className="codicon codicon-desktop-download" />下载完整项目</a>}
          <div className="workspace-actions" ref={actions}>
            <div className="workspace-menu">
              <button className="icon-button layout-button" aria-label="编辑器布局" title={"编辑器布局：" + layouts.find((item) => item.value === layout).label} aria-expanded={openMenu === "layout"} aria-controls="layout-options" onClick={() => setOpenMenu(openMenu === "layout" ? "" : "layout")}>
                <span aria-hidden="true" className={"codicon codicon-" + layouts.find((item) => item.value === layout).icon} /><span aria-hidden="true" className="codicon codicon-chevron-down" />
              </button>
              {openMenu === "layout" && <div id="layout-options" className="workspace-popover" role="group" aria-label="选择编辑器布局">
                {layouts.map((item) => <button key={item.value} aria-pressed={layout === item.value} onClick={() => { setLayout(item.value); setOpenMenu(""); actions.current.querySelector('.layout-button').focus(); }}><span aria-hidden="true" className={"codicon codicon-" + item.icon} />{item.label}</button>)}
              </div>}
            </div>
            <div className="workspace-menu">
              <button className="icon-button more-button" aria-label="更多操作" title="更多操作" aria-expanded={openMenu === "more"} aria-controls="workspace-options" onClick={() => setOpenMenu(openMenu === "more" ? "" : "more")}><span aria-hidden="true" className="codicon codicon-ellipsis" /></button>
              {openMenu === "more" && <div id="workspace-options" className="workspace-popover" role="group" aria-label="工作区操作">
                <button id="reset" onClick={() => { choose(selected); setOpenMenu(""); actions.current.querySelector('.more-button').focus(); }}><span aria-hidden="true" className="codicon codicon-refresh" />重置</button>
                <button id="stop" onClick={() => { service.current.stop(); setOpenMenu(""); actions.current.querySelector('.more-button').focus(); }}><span aria-hidden="true" className="codicon codicon-debug-stop" />中断</button>
              </div>}
            </div>
          </div>
        </div>
        <div className="dual-editor" data-layout={layout}>
          <div className="source-panel">
            <FileView key={generation} label="源码" files={sourceFiles} activePath={active} readonly={false}
              onSelect={(path) => { setActive(path); setPosition(null); }} onCreate={() => { setCreating(true); requestAnimationFrame(() => filename.current?.focus()); }}
              onDelete={(path) => {
                const next = files.filter((file) => file.path !== path);
                updateFiles(next);
                setActive(next[Math.min(files.findIndex((file) => file.path === path), next.length - 1)].path);
              }}
              onChange={(path, source) => updateFiles((current) => current.map((file) => file.path === path ? { ...file, source } : file))}
              onCompile={() => service.current.compile()} markers={markers} position={position}>
              {creating && <form className="new-file-form" onSubmit={(event) => { event.preventDefault(); create(); }}>
                <label htmlFor="new-file-name">新文件名</label>
                <input ref={filename} id="new-file-name" value={newName} onInput={(event) => setNewName(event.currentTarget.value)} placeholder="types.gox" maxLength={128} />
                <div><button type="submit">创建</button><button type="button" onClick={() => { setCreating(false); setFileError(""); }}>取消</button></div>
                {fileError && <p id="file-error" role="alert">{fileError}</p>}
              </form>}
            </FileView>
          </div>
          <div className="result-panel">
            <div className="result-heading"><strong>生成 Go</strong><span>只读</span></div>
            <FileView label="生成结果" files={result.files} activePath={outputPath} onSelect={setGenerated} />
          </div>
        </div>
        <div className="example-note">
          <p>{example.description}</p>
          {example.macroSources && <><button onClick={showImplementation}>查看实现</button><a href={example.macroDocumentation}>学习编写过程宏</a><span>宏源码只读，下载后可在本地修改。</span></>}
        </div>
        <div className="ide-statusbar">
          <p id="status" role="status" aria-live="polite" data-state={result.error ? "error" : ""}>{result.status}</p>
          <span>诊断 {markers.length || (diagnosticText ? 1 : 0)}</span>
          <span>{files.length} 个源文件</span><span>{result.files.length} 个生成文件</span>
          <span className="target-context">目标 {manifest.exportTarget.GOOS}/{manifest.exportTarget.GOARCH} · feature: 无</span>
        </div>
        {diagnosticText && <div className="diagnostics-panel" aria-label="转译诊断">
          {markers.length > 0 && <div className="diagnostic-links">{markers.map((marker, index) => <button key={index} onClick={() => jump(marker)}>{marker.path}:{marker.startLineNumber}:{marker.startColumn} — {marker.message}</button>)}</div>}
          <pre id="diagnostic" tabIndex={0}>{diagnosticText}</pre>
        </div>}
        {!diagnosticText && <pre id="diagnostic" hidden />}
      </div>
      <p className="caption editor-help">Ctrl / ⌘ Enter 转译 · 最多 16 个同包 .gox 文件，总计 32 KiB · 单次转译 {compileTimeoutMs(manifest) / 1000} 秒 · 生成 Go 不在浏览器运行。<br />Monaco 提供撤销、查找和基础着色；点击诊断可定位源码。Ctrl M（macOS 为 Ctrl Shift M）切换 Tab 导航。</p>
      <p className="provenance">
        {manifest.compilerSource.kind === "worktree" ? `工作树 · 基线 ${manifest.compilerSource.baseCommit} · SHA-256 ${manifest.compilerSource.digest}` : `编译器 ${manifest.compilerSource.commit}`} · {manifest.toolchain} · <a href="/examples.json">完整构建清单</a>
      </p>
    </section>
  );
}
