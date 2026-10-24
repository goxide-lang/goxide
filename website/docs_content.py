"""Publish the user guide and CLI help as Markdown pages."""

import pathlib
import re
import unicodedata
from urllib.parse import quote, unquote, urlsplit, urlunsplit


PAGES = (
    ("start", "开始使用", "选择使用方式"),
    ("install", "开始使用", "安装"),
    ("go-gen", "开始使用", "在已有 Go 项目中使用 Goxide"),
    ("first-project", "开始使用", "第一个 Goxide 项目"),
    ("language", "语言", "语言速览"),
    ("procedural-macros", "语言", "过程宏"),
    ("manifest", "项目与包管理", "清单字段"),
    ("dependencies", "项目与包管理", "依赖与命名空间"),
    ("lock-cache", "项目与包管理", "锁文件与缓存"),
    ("features", "条件编译", "Feature"),
    ("cfg", "条件编译", "条件编译"),
    ("target-env", "条件编译", "目标与环境"),
    ("cli", "CLI参考", "CLI 参考"),
    ("status", "状态与设计", "能力与设计"),
)


def slug(text):
    text = text.lower().replace("`", "")
    return "".join(c for c in text if c in "-_ " or unicodedata.category(c)[0] in "LN").replace(" ", "-")


def sections(markdown):
    result, current, fence = {}, None, None
    for line in markdown.splitlines():
        marker = re.match(r"^(`{3,}|~{3,})(.*)$", line)
        if marker:
            if fence is None:
                fence = marker[1]
            elif marker[1][0] == fence[0] and len(marker[1]) >= len(fence) and not marker[2].strip():
                fence = None
        if fence is None and line.startswith("## "):
            current = line[3:]
            result[current] = []
        elif current is not None:
            if fence is None and re.match(r"^#{3,6} ", line):
                line = line[1:]
            result[current].append(line)
    return {title: "\n".join(lines).strip() for title, lines in result.items()}


def design_sources(root):
    pages = [
        ('overview', '阅读与记号', root / 'docs/design/reading-guide.md'),
        ('grammar', '语言规范', root / 'docs/design/language-grammar.md'),
        ('semantics', '编译原理', root / 'docs/design/translation-semantics.md'),
        ('frontend', '结构与表示', root / 'docs/design/frontend-ast.md'),
        ('enum', '结构与表示', root / 'docs/design/enum-representation.md'),
        ('decisions', '设计决策', root / 'docs/design/decisions.md'),
        ('rfcs', 'RFC 设计', root / 'docs/rfcs/README.md'),
    ]
    pages.extend(('rfc-' + source.name[:4], 'RFC 设计', source)
                 for source in sorted((root / 'docs/rfcs').glob('[0-9][0-9][0-9][0-9]-*.md')))
    return pages


def public_links(markdown, source, root):
    guide = root / "docs/website-guide.md"
    section_pages = {slug(title): identity for identity, _, title in PAGES if identity != "go-gen"}
    document_pages = {
        root / "docs/using-goxide-gen.md": "/docs/?page=go-gen",
        root / "docs/mvp-gap-audit.md": "/docs/?page=status",
    }
    document_pages.update({path: '/design/?page=' + identity
                           for identity, _, path in design_sources(root)})

    def target(value):
        parsed = urlsplit(value)
        if parsed.netloc == "goxide-lang.github.io":
            return urlunsplit(("", "", parsed.path, parsed.query, parsed.fragment))
        if parsed.scheme or parsed.netloc or value.startswith("/"):
            return value
        destination = (source.parent / unquote(parsed.path)).resolve() if parsed.path else source
        fragment = unquote(parsed.fragment)
        if destination == guide and fragment in section_pages:
            return "/docs/?page=" + section_pages[fragment]
        if destination in document_pages:
            return document_pages[destination] + ("#" + parsed.fragment if fragment else "")
        if not parsed.path:
            return value
        relative = destination.relative_to(root).as_posix()
        kind = "tree" if destination.is_dir() else "blob"
        return "https://github.com/goxide-lang/goxide/" + kind + "/main/" + quote(relative, safe="/") + ("#" + parsed.fragment if fragment else "")

    lines, fence = [], None
    for line in markdown.splitlines():
        marker = re.match(r"^(`{3,}|~{3,})(.*)$", line)
        if marker:
            if fence is None:
                fence = marker[1]
            elif marker[1][0] == fence[0] and len(marker[1]) >= len(fence) and not marker[2].strip():
                fence = None
            lines.append(line)
        elif fence:
            lines.append(line)
        else:
            lines.append(re.sub(r"(\[[^]\n]+\]\()([^\s)]+)(\))", lambda match: match[1] + target(match[2]) + match[3], line))
    return "\n".join(lines)


def build(root, go, run, compiler_source):
    root = pathlib.Path(root).resolve()
    guide = root / "docs/website-guide.md"
    content = sections(guide.read_text(encoding="utf-8"))
    pages = []
    for identity, category, title in PAGES:
        source = guide
        if identity == "go-gen":
            source = root / "docs/using-goxide-gen.md"
            markdown = source.read_text(encoding="utf-8").partition("\n")[2].strip()
        else:
            markdown = content[title]
        if identity == "install":
            commit = compiler_source.get('commit') or compiler_source['baseCommit']
            assert re.fullmatch('[0-9a-f]{40}', commit), 'installation requires a complete compiler commit'
            markdown = markdown.replace('cd goxide\n', 'cd goxide\ngit checkout --detach ' + commit + '\n', 1)
            markdown += '\n\n对应编译器源码：[' + commit[:12] + '](https://github.com/goxide-lang/goxide/tree/' + commit + ')。'
        if identity == "cli":
            executable = str(root / "bin/goxide")
            for command in ((), ("gen",), ("build",), ("run",), ("test",), ("env",)):
                help_text = run(executable, *command, "--help", cwd=root).decode().strip()
                label = "goxide" + (" " + command[0] if command else "")
                markdown += "\n\n## " + label + "\n\n```text\n" + help_text + "\n```"
        if identity == "status":
            rows = []
            for line in (root / "docs/mvp-gap-audit.md").read_text(encoding="utf-8").splitlines():
                if line.startswith("| "):
                    name, state, scope = [cell.strip() for cell in line.split("|")[1:4]]
                    if name not in ("特性", "---") and "网站" not in name:
                        rows.append("| " + name + " | " + state + " | " + scope + " |")
            markdown += "\n\n## 当前能力\n\n| 能力 | 状态 | 当前范围 |\n| --- | --- | --- |\n" + "\n".join(rows)
        markdown = public_links(markdown, source, root)
        if str(root) in markdown:
            raise ValueError("local checkout path in published documentation")
        pages.append(dict(id=identity, category=category, title=title, markdown=markdown))
    return dict(pages=pages)


def build_design(root):
    root = pathlib.Path(root).resolve()
    pages = []
    for identity, category, source in design_sources(root):
        title, _, markdown = source.read_text(encoding='utf-8').partition('\n')
        assert title.startswith('# '), 'design document needs a title: ' + source.name
        pages.append(dict(id=identity, category=category, title=title[2:],
                          markdown=public_links(markdown.strip(), source, root)))
    return dict(pages=pages)
