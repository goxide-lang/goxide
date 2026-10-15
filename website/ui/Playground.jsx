import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { useLocation } from "preact-iso/router";
import FileView from "./FileView";
import { compiler, diagnostics } from "./compiler";
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
    [newName, setNewName] = useState(""),
    [fileError, setFileError] = useState(""),
    [generation, setGeneration] = useState(0);
  const service = useRef(null),
    filename = useRef(null),
    location = useLocation();
  const selectedRef = useRef(selected);
  selectedRef.current = selected;
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
  }
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
  const [dependencyPath, setDependencyPath] = useState(dependencyFiles[0].path);
  return (
    <section id="playground" hidden={!visible}>
      {!valid && <p role="alert">找不到此示例，请选择下列示例。</p>}
      <p className="eyebrow">真实编译器 · 在你的浏览器中</p>
      <h1>改一行，看 Go 如何生成。</h1>
      <p>同一用户包的源码在本地 WASM 中转译，不上传、不执行生成 Go。</p>
      <div className="toolbar">
        <label>
          示例
          <select
            id="examples"
            value={selected}
            onChange={(event) => {
              choose(event.target.value);
              location.route(
                "/playground.html?example=" +
                  encodeURIComponent(event.target.value),
                true,
              );
            }}
          >
            {examples.map((example) => (
              <option key={example.id} value={example.id}>
                {example.title}
              </option>
            ))}
          </select>
        </label>
        <button id="compile" onClick={() => service.current.compile()}>
          转译 <kbd>Ctrl / ⌘ Enter</kbd>
        </button>
        <button id="reset" onClick={() => choose(selected)}>
          重置示例
        </button>
        <button id="stop" onClick={() => service.current.stop()}>
          中断
        </button>
      </div>
      <p>{examples.find((example) => example.id === selected).description}</p>
      <div className="toolbar">
        <label hidden={layout === "result"}>
          新文件名
          <input
            ref={filename}
            id="new-file-name"
            value={newName}
            onInput={(event) => setNewName(event.currentTarget.value)}
            placeholder="types.gox"
            maxLength={128}
          />
        </label>
        <span>
          {packageMode ? "同包" : "单文件"} · {files.length} 文件 · UTF-8 总计
          ≤32KiB
        </span>
      </div>
      {fileError && (
        <p id="file-error" role="alert">
          {fileError}
        </p>
      )}
      <div className="toolbar layouts" aria-label="编辑器布局">
        {[
          ["source", "仅源码"],
          ["result", "仅结果"],
          ["horizontal", "左右"],
          ["vertical", "上下"],
        ].map(([value, label]) => (
          <button
            key={value}
            data-layout={value}
            aria-pressed={layout === value}
            onClick={() => setLayout(value)}
          >
            {label}
          </button>
        ))}
      </div>
      <div className="dual-editor" data-layout={layout}>
        <div className="source-panel">
          <FileView
            key={generation}
            label="源码"
            files={files}
            activePath={active}
            readonly={false}
            onSelect={setActive}
            onCreate={create}
            onDelete={(path) => {
              const next = files.filter((file) => file.path !== path);
              updateFiles(next);
              setActive(
                next[
                  Math.min(
                    files.findIndex((file) => file.path === path),
                    next.length - 1,
                  )
                ].path,
              );
            }}
            onChange={(path, source) =>
              updateFiles((current) =>
                current.map((file) =>
                  file.path === path ? { ...file, source } : file,
                ),
              )
            }
            onCompile={() => service.current.compile()}
            markers={diagnostics(result.error, files)}
          />
        </div>
        <div className="result-panel">
          <FileView
            label="生成结果"
            files={result.files}
            activePath={outputPath}
            onSelect={setGenerated}
          />
        </div>
      </div>
      <p
        id="status"
        role="status"
        aria-live="polite"
        data-state={result.error ? "error" : ""}
      >
        {result.status}
      </p>
      <pre id="diagnostic" tabIndex={0} hidden={!result.error}>
        {result.error || ""}
      </pre>
      <p className="caption">
        Ctrl / ⌘ Enter 转译；Monaco 提供编辑、撤销和查找。这里仅提供 Go
        基础着色与 WASM 诊断，不具备完整 Go/Goxide
        语言服务。单次转译10秒，最多一个编译器Worker。Tab按编辑器默认方式缩进；Ctrl
        M（macOS为Ctrl Shift M）切换Tab导航。
      </p>
      <h2>依赖源码（只读）</h2>
      <p>
        构建随附的固定包源码，不属于可编辑输入；配置只读展示，不能在此加载任意项目。
      </p>
      <p className="provenance">
        {
          dependencyFiles.find((file) => file.path === dependencyPath)
            ?.provenance
        }
      </p>
      <FileView
        label="只读依赖"
        files={dependencyFiles}
        activePath={dependencyPath}
        onSelect={setDependencyPath}
      />
      <p className="provenance">
        {manifest.compilerSource.kind === "worktree"
          ? `工作树 · 基线 ${manifest.compilerSource.baseCommit} · SHA-256 ${manifest.compilerSource.digest}`
          : `编译器 ${manifest.compilerSource.commit}`} · {manifest.toolchain} ·{" "}
        <a href="/examples.json">完整构建清单</a>
      </p>
    </section>
  );
}
