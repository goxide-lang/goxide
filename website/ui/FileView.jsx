import { useState, useLayoutEffect, useErrorBoundary } from "preact/hooks";
import lazy, { ErrorBoundary } from "preact-iso/lazy";
const MonacoPane = lazy(() =>
  import("./MonacoPane.jsx").catch((error) => ({
    // Resolve failed imports to an explicit fallback; native lazy only resumes fulfilled loads.
    default: function EditorLoadFailure({ onReady }) {
      useLayoutEffect(() => onReady(), []);
      return <p role="alert">编辑器加载失败，请刷新重试：{String(error)}</p>;
    },
  })),
);
export default function FileView({
  label,
  files,
  activePath,
  readonly = true,
  onSelect,
  onChange,
  onCreate,
  onDelete,
  onCompile,
  markers,
  children,
}) {
  const [collapsed, setCollapsed] = useState(false),
    [copied, setCopied] = useState(""),
    [editorReady, setEditorReady] = useState(false);
  const [loadError] = useErrorBoundary();
  const current = files.find((file) => file.path === activePath);
  const groups = Map.groupBy(files, (file) =>
    file.path.includes("/")
      ? file.path.slice(0, file.path.lastIndexOf("/"))
      : label,
  );
  function fileButton(file) {
    return (
      <button
        key={file.path}
        type="button"
        data-path={file.path}
        aria-pressed={file.path === activePath}
        title={file.path}
        onClick={() => onSelect(file.path)}
      >
        {file.path.split("/").at(-1)}
      </button>
    );
  }
  const create =
    !readonly && onCreate ? (
      <button type="button" className="new-file" onClick={onCreate}>
        + 新建文件
      </button>
    ) : null;
  return (
    <section className="file-view" aria-label={label}>
      <div className="view-toolbar">
        <button
          type="button"
          aria-expanded={!collapsed}
          onClick={() => setCollapsed(!collapsed)}
        >
          {collapsed ? "展开文件树" : "收起文件树"}
        </button>
        <strong>{label}</strong>
        <span>{readonly ? "只读" : "可编辑"}</span>
      </div>
      <div className={"view-body" + (collapsed ? " collapsed" : "")}>
        <nav
          className="file-tree"
          aria-label={label + "文件树"}
          hidden={collapsed}
        >
          {[...groups].map(([directory, entries]) => (
            <div key={directory}>
              <p>{directory}</p>
              {entries.map(fileButton)}
            </div>
          ))}
          {create}
        </nav>
        <div className="file-pane">
          <div
            className="tabs-row"
            role="group"
            aria-label={label + "文件 tabs"}
          >
            <div className="file-tabs">{files.map(fileButton)}</div>
            {create}
          </div>
          <div className="filebar">
            <span>{current?.path || "暂无文件"}</span>
            {!readonly && onDelete && (
              <button
                type="button"
                disabled={files.length <= 1}
                onClick={() => onDelete(activePath)}
              >
                删除文件
              </button>
            )}
            <button
              type="button"
              disabled={!current}
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(current.source);
                  setCopied("已复制");
                } catch {
                  setCopied("请在编辑器中选择并复制");
                }
              }}
            >
              复制文件
            </button>
            <span role="status">{copied}</span>
          </div>
          {children}
          {files.length > 0 ? (
            <>
              {!editorReady && !loadError && (
                <p className="loading" role="status">
                  正在加载编辑器…
                </p>
              )}
              {loadError ? (
                <p role="alert">
                  编辑器加载失败，请刷新重试：{String(loadError)}
                </p>
              ) : (
                <ErrorBoundary>
                  <MonacoPane
                    files={files}
                    activePath={activePath}
                    readonly={readonly}
                    markers={markers}
                    onChange={readonly ? undefined : onChange}
                    onCompile={onCompile}
                    label={label + "编辑器"}
                    onReady={() => setEditorReady(true)}
                  />
                </ErrorBoundary>
              )}
            </>
          ) : (
            <p className="empty">{readonly ? "尚无转译结果" : "没有文件"}</p>
          )}
        </div>
      </div>
    </section>
  );
}
