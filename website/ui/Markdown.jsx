import { Code, CopyButton } from "./Readonly";

export function plainText(text) {
  return text.replace(/\[([^\]]+)\]\([^)]*\)/g, "$1").replace(/[`*_~]/g, "");
}

function safeHref(value) {
  if (/[\u0000-\u001f\u007f]/.test(value)) return null;
  try {
    const url = new URL(value, window.location.href);
    return ["http:", "https:", "mailto:"].includes(url.protocol) ? value : null;
  } catch {
    return null;
  }
}

function inline(text) {
  const pattern = /(`+)([\s\S]*?)\1|\[([^\]]+)\]\(([^\s)]+)(?:\s+"[^"]*")?\)|\*\*([^*]+)\*\*|__([^_]+)__|\*([^*\n]+)\*|_([^_\n]+)_/g;
  const result = [];
  let offset = 0;
  for (const match of text.matchAll(pattern)) {
    result.push(text.slice(offset, match.index));
    const [, ticks, code, label, href, bold, boldAlt, italic, italicAlt] = match;
    if (ticks) result.push(<code key={match.index}>{code}</code>);
    else if (label) {
      const target = safeHref(href);
      result.push(target ? <a key={match.index} href={target}>{inline(label)}</a> : <span key={match.index}>{label}</span>);
    } else if (bold || boldAlt) result.push(<strong key={match.index}>{inline(bold || boldAlt)}</strong>);
    else result.push(<em key={match.index}>{inline(italic || italicAlt)}</em>);
    offset = match.index + match[0].length;
  }
  result.push(text.slice(offset));
  return result;
}

function cells(line) {
  const row = line.trim().replace(/^\|/, "").replace(/\|$/, ""), result = [];
  let cell = "", code = false;
  for (let index = 0; index < row.length; index++) {
    const char = row[index];
    if (char === "\\" && row[index + 1] === "|") { cell += "|"; index++; }
    else if (char === "`") { code = !code; cell += char; }
    else if (char === "|" && !code) { result.push(cell.trim()); cell = ""; }
    else cell += char;
  }
  result.push(cell.trim());
  return result;
}

const listLine = /^(\s*)([-+*]|\d+[.)])\s+(.*)$/;
const divider = /^\s*\|?\s*:?-{3,}:?\s*(?:\|\s*:?-{3,}:?\s*)+\|?\s*$/;
const blockStart = /^(?:\s*(?:`{3,}|~{3,}|#{1,6}\s|>\s?|[-+*]\s|\d+[.)]\s)|\s*(?:-{3,}|\*{3,}|_{3,})\s*$)/;

export function parseMarkdown(markdown, slugs = new Map()) {
  const lines = markdown.replace(/\r\n?/g, "\n").split("\n"), blocks = [];
  let index = 0;
  while (index < lines.length) {
    const line = lines[index];
    if (!line.trim()) { index++; continue; }
    const fence = line.match(/^\s*(`{3,}|~{3,})([^\s]*)\s*$/);
    if (fence) {
      const code = []; index++;
      while (index < lines.length && !new RegExp(`^\\s*${fence[1][0]}{${fence[1].length},}\\s*$`).test(lines[index])) code.push(lines[index++]);
      if (index < lines.length) index++;
      blocks.push({ type: "code", language: fence[2], source: code.join("\n") }); continue;
    }
    const heading = line.match(/^(#{1,6})\s+(.+?)\s*#*$/);
    if (heading) {
      const title = plainText(heading[2]);
      const base = title.toLowerCase().replace(/[^\p{L}\p{N}\s_-]/gu, "").trim().replace(/[\s_]+/g, "-") || "section";
      const count = (slugs.get(base) || 0) + 1; slugs.set(base, count);
      blocks.push({ type: "heading", level: heading[1].length, text: heading[2], title, id: base + (count > 1 ? `-${count}` : "") }); index++; continue;
    }
    if (index + 1 < lines.length && divider.test(lines[index + 1])) {
      const header = cells(line), align = cells(lines[index + 1]).map((cell) => cell.startsWith(":") ? cell.endsWith(":") ? "center" : "left" : cell.endsWith(":") ? "right" : undefined);
      index += 2; const rows = [];
      while (index < lines.length && lines[index].trim() && lines[index].includes("|")) rows.push(cells(lines[index++]));
      blocks.push({ type: "table", header, align, rows }); continue;
    }
    const list = line.match(listLine);
    if (list) {
      const ordered = /\d/.test(list[2]), indent = list[1].length, items = [];
      while (index < lines.length) {
        const item = lines[index].match(listLine);
        if (!item || item[1].length !== indent || /\d/.test(item[2]) !== ordered) break;
        const content = [item[3]], padding = item[0].length - item[3].length;
        index++;
        while (index < lines.length && (/^\s+\S/.test(lines[index]) && lines[index].match(/^\s*/)[0].length > indent)) content.push(lines[index++].slice(padding));
        items.push(parseMarkdown(content.join("\n"), slugs));
        if (!lines[index]?.trim() && lines[index + 1]?.match(listLine)?.[1].length === indent) index++;
      }
      blocks.push({ type: "list", ordered, start: ordered ? Number.parseInt(list[2], 10) : undefined, items }); continue;
    }
    if (/^\s*>/.test(line)) {
      const quote = [];
      while (index < lines.length && /^\s*>/.test(lines[index])) quote.push(lines[index++].replace(/^\s*>\s?/, ""));
      blocks.push({ type: "quote", blocks: parseMarkdown(quote.join("\n"), slugs) }); continue;
    }
    if (/^\s*(?:-{3,}|\*{3,}|_{3,})\s*$/.test(line)) { blocks.push({ type: "rule" }); index++; continue; }
    const paragraph = [line]; index++;
    while (index < lines.length && lines[index].trim() && !blockStart.test(lines[index]) && !(index + 1 < lines.length && divider.test(lines[index + 1]))) paragraph.push(lines[index++]);
    blocks.push({ type: "paragraph", text: paragraph.join("\n") });
  }
  return blocks;
}

export default function Markdown({ blocks }) {
  return blocks.map((block, index) => {
    if (block.type === "heading") {
      const Heading = `h${Math.max(2, block.level)}`;
      return <Heading key={index} id={block.id}>{inline(block.text)}<a className="heading-anchor" href={`#${block.id}`} aria-label={`链接到${block.title}`}>#</a></Heading>;
    }
    if (block.type === "code") return <div className="markdown-code" key={index}><div className="readonly-filebar"><span>{block.language || "代码"}</span><CopyButton text={block.source} /></div><Code source={block.source} label={`${block.language || "文档"} 代码示例`} /></div>;
    if (block.type === "table") return <div className="table-scroll" key={index} tabIndex={0} aria-label="文档表格"><table><thead><tr>{block.header.map((cell, cellIndex) => <th key={cellIndex} scope="col" style={{ textAlign: block.align[cellIndex] }}>{inline(cell)}</th>)}</tr></thead><tbody>{block.rows.map((row, rowIndex) => <tr key={rowIndex}>{block.header.map((_, cellIndex) => <td key={cellIndex} style={{ textAlign: block.align[cellIndex] }}>{inline(row[cellIndex] || "")}</td>)}</tr>)}</tbody></table></div>;
    if (block.type === "list") { const List = block.ordered ? "ol" : "ul"; return <List key={index} start={block.start}>{block.items.map((item, itemIndex) => <li key={itemIndex}><Markdown blocks={item} /></li>)}</List>; }
    if (block.type === "quote") return <blockquote key={index}><Markdown blocks={block.blocks} /></blockquote>;
    if (block.type === "rule") return <hr key={index} />;
    return <p key={index}>{inline(block.text)}</p>;
  });
}
