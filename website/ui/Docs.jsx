import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { useLocation } from "preact-iso/router";
import Markdown, { parseMarkdown, plainText } from "./Markdown";

function TableOfContents({ headings }) {
  return <nav aria-label="本页目录">{headings.map((heading) => <a key={heading.id} className={heading.level > 2 ? "nested" : undefined} href={`#${heading.id}`}>{heading.title}</a>)}</nav>;
}

export default function Docs({ manifest, kind = "docs" }) {
  const location = useLocation(), pages = manifest[kind]?.pages || [];
  const design = kind === "design", basePath = design ? "/design/" : "/docs/";
  const title = design ? "设计文档" : "使用参考", searchLabel = design ? "搜索设计文档" : "搜索文档";
  const [query, setQuery] = useState(""), [navigationOpen, setNavigationOpen] = useState(false);
  const search = useRef(null);
  const requested = location.query.page || pages[0]?.id, page = pages.find((item) => item.id === requested);
  const blocks = useMemo(() => page ? parseMarkdown(page.markdown).filter((block, index) => !(index === 0 && block.type === "heading" && plainText(block.text) === page.title)) : [], [page]);
  const headings = blocks.filter((block) => block.type === "heading" && block.level <= 3);
  const terms = query.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
  const filtered = pages.filter((item) => terms.every((term) => `${item.category} ${item.title} ${item.markdown}`.toLocaleLowerCase().includes(term)));
  const categories = [...new Set(filtered.map((item) => item.category))];
  useLayoutEffect(() => { setQuery(""); setNavigationOpen(false); }, [kind]);
  useEffect(() => {
    const focusSearch = (event) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
        event.preventDefault(); setNavigationOpen(true); requestAnimationFrame(() => search.current?.focus());
      }
    };
    window.addEventListener("keydown", focusSearch);
    return () => window.removeEventListener("keydown", focusSearch);
  }, []);
  return (
    <section className="docs-page" aria-label={title}>
      <aside className="section-sidebar docs-sidebar" data-open={navigationOpen}>
        <button className="sidebar-toggle" type="button" aria-expanded={navigationOpen} aria-controls={`${kind}-navigation`} onClick={() => setNavigationOpen(!navigationOpen)}>{design ? "设计文档导航与搜索" : "文档导航与搜索"} <span aria-hidden="true">{navigationOpen ? "−" : "+"}</span></button>
        <div className="sidebar-content" id={`${kind}-navigation`}>
          <p className="reader-title">{title}</p>
          <p className="reader-caption">{design ? "语言规范 · 编译器设计 · RFC" : "语言用法 · 项目配置 · CLI"}</p>
          <label className="docs-search"><span className="sr-only">{searchLabel}</span><span aria-hidden="true">⌕</span><input ref={search} type="search" value={query} placeholder={`${searchLabel}…`} onInput={(event) => setQuery(event.currentTarget.value)} /><kbd aria-hidden="true">⌘ K</kbd></label>
          {terms.length > 0 && <p className="search-count" role="status">{filtered.length ? `找到 ${filtered.length} 篇文档` : "未找到匹配的文档"}</p>}
          <nav aria-label={design ? "设计文档分类" : "文档分类"}>{categories.map((category) => <div className="docs-category" key={category}><h2>{category}</h2>{filtered.filter((item) => item.category === category).map((item) => <a key={item.id} href={`${basePath}?page=${item.id}`} aria-current={item.id === requested ? "page" : undefined} onClick={() => setNavigationOpen(false)}>{item.title}</a>)}</div>)}</nav>
          <div className="sidebar-help">{design ? <><span>查找实际用法与配置？</span><a href="/docs/">打开使用参考 →</a></> : <><span>想跟着项目开始？</span><a href="/learn/">阅读快速开始 →</a></>}</div>
        </div>
      </aside>
      {!pages.length ? <p className="content-loading" role="status">正在载入{title}…</p> : !page ? <div className="missing-content" role="alert"><h1>找不到此文档。</h1><p>请从分类中选择页面，或搜索需要的内容。</p><a href={basePath}>打开{title}首页 →</a></div> : <>
        <article className="docs-article" data-page={page.id}>
          <p className="eyebrow">{title} · {page.category}</p><h1>{page.title}</h1>
          {design && (page.category.replace(/\s/g, "") === "RFC设计" || page.id === "frontend") && <aside className="design-reading-note" aria-label="设计文档阅读说明">本页记录设计与历史方案。当前支持范围见<a href="/docs/?page=status">能力表</a>，现行形式规则见<a href="/design/?page=grammar">语法规范</a>与<a href="/design/?page=semantics">语义规范</a>。</aside>}
          {headings.length > 0 && <details className="inline-toc"><summary>本页目录</summary><TableOfContents headings={headings} /></details>}
          <div className="markdown-content"><Markdown blocks={blocks} /></div>
          <div className="docs-bottom"><a href={design ? "/docs/" : "/learn/"}>{design ? "查看使用参考" : "用完整项目试一遍"} →</a><a href={design ? "https://github.com/goxide-lang/goxide" : "/design/"}>{design ? "查看仓库源码 ↗" : "阅读设计文档 →"}</a></div>
        </article>
        <aside className="docs-toc"><div><p>本页目录</p><TableOfContents headings={headings} /><a className="back-to-top" href="#main">回到顶部 ↑</a></div></aside>
      </>}
    </section>
  );
}
