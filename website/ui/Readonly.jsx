import { useEffect, useId, useState } from "preact/hooks";

export function Tabs({ label, items, value, onChange, id }) {
  return (
    <div
      className="code-tabs"
      role="tablist"
      aria-label={label}
      onKeyDown={(event) => {
        const index = items.findIndex((item) => item.id === value);
        const next = {
          ArrowRight: (index + 1) % items.length,
          ArrowLeft: (index - 1 + items.length) % items.length,
          Home: 0,
          End: items.length - 1,
        }[event.key];
        if (next === undefined) return;
        event.preventDefault();
        onChange(items[next].id);
        event.currentTarget.querySelectorAll('[role="tab"]')[next].focus();
      }}
    >
      {items.map((item) => (
        <button
          key={item.id}
          id={`${id}-tab-${item.id}`}
          type="button"
          role="tab"
          aria-selected={value === item.id}
          aria-controls={`${id}-panel`}
          tabIndex={value === item.id ? 0 : -1}
          onClick={() => onChange(item.id)}
        >
          {item.title}
        </button>
      ))}
    </div>
  );
}

export function CopyButton({ text, label = "复制代码" }) {
  const [status, setStatus] = useState("");
  useEffect(() => setStatus(""), [text]);
  return (
    <span className="copy-control">
      <button
        type="button"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(text);
            setStatus("已复制");
          } catch {
            setStatus("请选中代码后复制");
          }
        }}
      >
        {label}
      </button>
      <span className="sr-only" role="status">{status}</span>
      {status && <span aria-hidden="true" className="copy-status">{status}</span>}
    </span>
  );
}

export function Code({ source, label, className = "" }) {
  const tokens = String(source).split(
    /(\/\*[\s\S]*?\*\/|\/\/[^\n]*|#[^\n]*|"(?:\\.|[^"\\])*"|`[^`]*`|\b(?:package|import|use|pub|func|fn|return|type|struct|enum|match|case|default|if|else|for|range|let|var|const|impl|trait|true|false|nil)\b|\b\d+(?:\.\d+)?\b)/g,
  );
  return (
    <pre className={`code-source ${className}`} tabIndex={0} aria-label={label}>
      <code>
        {tokens.map((token, index) => {
          if (index % 2 === 0) return token;
          const kind = /^(\/\/|\/\*|#)/.test(token)
            ? "comment"
            : /^["`]/.test(token)
              ? "string"
              : /^\d/.test(token)
                ? "number"
                : "keyword";
          return <span key={index} className={`token-${kind}`}>{token}</span>;
        })}
      </code>
    </pre>
  );
}

export default function Readonly({ label, files, initialPath, compact = false }) {
  const id = useId();
  const [selected, setSelected] = useState(initialPath || files[0]?.path || "");
  const [treeOpen, setTreeOpen] = useState(!compact);
  const current = files.find((file) => file.path === selected) || files[0];
  return (
    <section className={`file-view readonly-view${compact ? " compact" : ""}`} aria-label={label}>
      <div className="view-toolbar">
        {files.length > 1 && (
          <button type="button" aria-expanded={treeOpen} onClick={() => setTreeOpen(!treeOpen)}>
            {treeOpen ? "收起文件树" : "展开文件树"}
          </button>
        )}
        <strong>{label}</strong>
        <span>只读</span>
      </div>
      {current ? (
        <div className={`readonly-body${treeOpen && files.length > 1 ? " with-tree" : ""}`}>
          {treeOpen && files.length > 1 && (
            <nav className="readonly-tree" aria-label={`${label}文件树`}>
              {files.map((file) => (
                <button key={file.path} type="button" aria-pressed={current.path === file.path} onClick={() => setSelected(file.path)}>
                  {file.path}
                </button>
              ))}
            </nav>
          )}
          <div className="readonly-pane">
            {files.length > 1 && (
              <Tabs
                id={id}
                label={`${label}文件`}
                items={files.map((file) => ({ id: file.path, title: file.path.split("/").at(-1) }))}
                value={current.path}
                onChange={setSelected}
              />
            )}
            <div className="readonly-filebar">
              <span>{current.path}</span>
              <CopyButton text={current.source} label="复制文件" />
            </div>
            <div id={`${id}-panel`} role={files.length > 1 ? "tabpanel" : undefined} aria-labelledby={files.length > 1 ? `${id}-tab-${current.path}` : undefined}>
              <Code source={current.source} label={`${current.path} 内容`} />
            </div>
          </div>
        </div>
      ) : <p className="empty">此阶段没有此类文件。</p>}
    </section>
  );
}
