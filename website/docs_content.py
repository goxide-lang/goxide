"""Publish the user guide and CLI help as Markdown pages."""

import pathlib
import re
import unicodedata
from urllib.parse import quote, unquote, urlsplit, urlunsplit

import sdk_reference


PAGES = (
    ("start", "开始使用", "选择使用方式", "website-guide.md"),
    ("install", "开始使用", "安装", "website-guide.md"),
    ("go-gen", "开始使用", "在已有 Go 项目中使用 Goxide", "using-goxide-gen.md"),
    ("first-project", "开始使用", "第一个 Goxide 项目", "website-guide.md"),
    ("language", "语言", "语言速览", "website-guide.md"),
    ("serde-overview", "serde 与 JSON", "serde 与 JSON 概览", "serde-json.md"),
    ("serde-struct", "serde 与 JSON", "struct 与字段标记", "serde-json.md"),
    ("serde-enum", "serde 与 JSON", "enum 与分派格式", "serde-json.md"),
    ("serde-option", "serde 与 JSON", "Option 与缺失值", "serde-json.md"),
    ("serde-limits", "serde 与 JSON", "限制和改进方向", "serde-json.md"),
    ("procedural-macros", "过程宏与 proc_macro", "过程宏概览", "procedural-macros.md"),
    ("proc-first", "过程宏与 proc_macro", "第一个过程宏", "procedural-macros.md"),
    ("proc-function", "过程宏与 proc_macro", "函数式过程宏", "procedural-macros.md"),
    ("proc-attribute", "过程宏与 proc_macro", "属性过程宏", "procedural-macros.md"),
    ("proc-derive", "过程宏与 proc_macro", "派生宏与 helper", "procedural-macros.md"),
    ("proc-context", "过程宏与 proc_macro", "Context 与名称解析", "procedural-macros.md"),
    ("proc-diagnostics", "过程宏与 proc_macro", "源码位置与诊断", "procedural-macros.md"),
    ("proc-config", "过程宏与 proc_macro", "目标与条件编译", "procedural-macros.md"),
    ("proc-project", "过程宏与 proc_macro", "宏项目测试与发布", "procedural-macros.md"),
    ("proc-host", "过程宏与 proc_macro", "执行环境与浏览器", "procedural-macros.md"),
    ("proc-standard", "过程宏与 proc_macro", "标准库与 serde 派生", "procedural-macros.md"),
    ("syntax-overview", "syntax 语法工具", "语法工具概览", "syntax-tools.md"),
    ("syntax-token", "syntax 语法工具", "读取与构造 token", "syntax-tools.md"),
    ("syntax-syn", "syntax 语法工具", "读取类型声明", "syntax-tools.md"),
    ("syntax-fragments", "syntax 语法工具", "保留语法与来源", "syntax-tools.md"),
    ("syntax-host", "syntax 语法工具", "高级宿主协议", "syntax-tools.md"),
    ("manifest", "项目与包管理", "清单字段", "website-guide.md"),
    ("dependencies", "项目与包管理", "依赖与命名空间", "website-guide.md"),
    ("lock-cache", "项目与包管理", "锁文件与缓存", "website-guide.md"),
    ("features", "条件编译", "Feature", "website-guide.md"),
    ("cfg", "条件编译", "条件编译", "website-guide.md"),
    ("target-env", "条件编译", "目标与环境", "website-guide.md"),
    ("cli", "CLI参考", "CLI 参考", "website-guide.md"),
    ("status", "状态与设计", "能力与设计", "website-guide.md"),
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
    section_pages = {(root / "docs" / filename, slug(title)): identity
                     for identity, _, title, filename in PAGES if identity != "go-gen"}
    section_pages[(guide, slug("过程宏"))] = "procedural-macros"
    document_pages = {
        root / "docs/using-goxide-gen.md": "/docs/?page=go-gen",
        root / "docs/mvp-gap-audit.md": "/docs/?page=status",
        root / "docs/procedural-macros.md": "/docs/?page=procedural-macros",
        root / "docs/syntax-tools.md": "/docs/?page=syntax-overview",
        root / "docs/serde-json.md": "/docs/?page=serde-overview",
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
        if (destination, fragment) in section_pages:
            return "/docs/?page=" + section_pages[(destination, fragment)]
        if parsed.path and destination in document_pages:
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
    content = {filename: sections((root / "docs" / filename).read_text(encoding="utf-8"))
               for filename in dict.fromkeys(page[3] for page in PAGES)}
    pages = []
    for identity, category, title, filename in PAGES:
        source = root / "docs" / filename
        if identity == "go-gen":
            markdown = source.read_text(encoding="utf-8").partition("\n")[2].strip()
        else:
            markdown = content[filename][title]
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
    pages.extend(sdk_reference.build(root, go, run))
    assert len({page['id'] for page in pages}) == len(pages), 'duplicate documentation page ID'
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
