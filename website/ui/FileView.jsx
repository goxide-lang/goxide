import { useState, useLayoutEffect, useErrorBoundary } from "preact/hooks";
import lazy, { ErrorBoundary } from "preact-iso/lazy";
const MonacoPane = lazy(() =>
  import("./MonacoPane.jsx").catch((error) => ({
    default: function EditorLoadFailure({ onReady }) {
      useLayoutEffect(() => onReady(), []);
      return <p role="alert">编辑器加载失败，请刷新重试：{String(error)}</p>;
    },
  })),
);
export default function FileView({
  label, files, activePath, readonly = true, onSelect, onChange, onCreate,
  onDelete, onCompile, markers, position, children,
}) {
  const [collapsed, setCollapsed] = useState(() => window.matchMedia("(max-width: 620px)").matches),
    [copied, setCopied] = useState(""),
    [editorReady, setEditorReady] = useState(false);
  const [loadError] = useErrorBoundary();
  const current = files.find((file) => file.path === activePath);
  const currentReadonly = readonly || !!current?.readonly;
  const groups = Map.groupBy(files, (file) => file.group || (readonly ? "生成文件 · 只读" : "源码 · 可编辑"));
  const tabs = readonly ? files : files.filter((file) => !file.readonly || file.path === activePath);
  useLayoutEffect(() => setCopied(""), [activePath, current?.source]);
  useLayoutEffect(() => {
    const narrow = window.matchMedia("(max-width: 620px)");
    const change = () => setCollapsed(narrow.matches);
    narrow.addEventListener("change", change);
    return () => narrow.removeEventListener("change", change);
  }, []);
  function fileButton(file) {
    return <button key={file.path} type="button" data-path={file.path}
      aria-pressed={file.path === activePath} title={file.path}
      onClick={() => onSelect(file.path)}><span aria-hidden="true" className={"codicon codicon-" + (readonly || file.readonly ? "lock" : "file-code")} />{file.path.split("/").at(-1)}</button>
  }
  const copyButton = <button type="button" className="icon-button" aria-label="复制文件" title={copied || "复制文件"} disabled={!current} onClick={async () => {
    try { await navigator.clipboard.writeText(current.source); setCopied("已复制"); }
    catch { setCopied("请在编辑器中选择并复制"); }
  }}><span aria-hidden="true" className={"codicon codicon-" + (copied === "已复制" ? "check" : "copy")} /></button>;
  return (
    <section className="file-view" aria-label={label}>
      <div className={"view-body" + (collapsed ? " collapsed" : "")}>
        <aside className="file-explorer" hidden={collapsed}>
          <div className="explorer-toolbar">
            <strong>{readonly ? "生成文件" : "文件"}</strong>
            {!readonly && onCreate && <button type="button" className="icon-button new-file" aria-label="新建文件" title="新建文件" onClick={onCreate}><span aria-hidden="true" className="codicon codicon-add" /></button>}
            <button type="button" className="icon-button" aria-label="收起文件树" title="收起文件树" aria-expanded="true" onClick={() => setCollapsed(true)}><span aria-hidden="true" className="codicon codicon-layout-sidebar-left-off" /></button>
          </div>
          <nav className="file-tree" aria-label={label + "文件树"}>
          {[...groups].map(([group, entries]) => group === "依赖源码 · 只读" ? (
            <details key={group}><summary>{group}</summary>{entries.map(fileButton)}</details>
          ) : <div key={group}>{(!readonly || groups.size > 1) && <p>{group}</p>}{entries.map(fileButton)}</div>)}
          {children}
          </nav>
        </aside>
        <div className="file-pane">
          <div className="tabs-row" role="group" aria-label={label + "文件 tabs"}>
            {collapsed && <button type="button" className="icon-button expand-tree" aria-label="展开文件树" title="展开文件树" aria-expanded="false" onClick={() => setCollapsed(false)}><span aria-hidden="true" className="codicon codicon-layout-sidebar-left" /></button>}
            <div className="file-tabs">{tabs.map(fileButton)}</div>
            {collapsed && !readonly && onCreate && <button type="button" className="icon-button new-file" aria-label="新建文件" title="新建文件" onClick={() => { setCollapsed(false); onCreate(); }}><span aria-hidden="true" className="codicon codicon-add" /></button>}
            {readonly && <div className="output-tab-actions"><span className="readonly-badge">只读</span>{copyButton}</div>}
          </div>
          {!readonly && <div className="filebar">
            <span>{current?.path || "暂无文件"}</span>
            {currentReadonly && current && <span className="readonly-badge">只读</span>}
            {!currentReadonly && onDelete && <button type="button" className="icon-button" aria-label="删除文件" title="删除文件" disabled={files.filter((file) => !file.readonly).length <= 1} onClick={() => onDelete(activePath)}><span aria-hidden="true" className="codicon codicon-trash" /></button>}
            {copyButton}
          </div>}
          {copied && <span className="sr-only" role="status">{copied}</span>}
          {current?.provenance && <p className="file-provenance">{current.provenance}</p>}
          {files.length > 0 ? <>
            {!editorReady && !loadError && <p className="loading" role="status">正在加载编辑器…</p>}
            {loadError ? <p role="alert">编辑器加载失败，请刷新重试：{String(loadError)}</p> : <ErrorBoundary>
              <MonacoPane files={files} activePath={activePath} readonly={readonly} markers={markers} position={position}
                onChange={onChange} onCompile={onCompile} label={label + "编辑器"} onReady={() => setEditorReady(true)} />
            </ErrorBoundary>}
          </> : <div className="empty"><strong>生成的 Go 会出现在这里</strong><p>编辑源码后自动转译，也可以点击「转译」。</p></div>}
        </div>
      </div>
    </section>
  );
}
