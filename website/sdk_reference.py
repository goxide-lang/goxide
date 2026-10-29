"""Publish API references from the compiler's pinned, unreplaced public SDK."""
import json
import pathlib
import re


MODULE = 'github.com/goxide-lang/syntax'
PAGES = {
    'token': ('sdk-token', 'syntax 语法工具', 'token API 参考'),
    'proc_macro': ('sdk-proc-macro', '过程宏与 proc_macro', 'proc_macro API 参考'),
    'syn': ('sdk-syn', 'syntax 语法工具', 'syn API 参考'),
}
INTRO = {
    'token': 'TokenStream、Token、绑定上下文与源码位置是过程宏的基础。构造和修改 token 时，显式选择绑定上下文；复用输入 token 可保留其原有来源。',
    'proc_macro': '编写宏时先查 Context、Output、Success、Failure 与 FromParse。Request、Scope、读写协议和 Dispatch 用于测试、启动器与宿主集成。',
    'syn': 'Parse 将一个 type、struct 或 enum 声明解析成只读语法视图。通过 Item、Field、Variant 读取结构，通过 ToTokens 复用原 token；语义解析仍由编译器完成。',
}


def anchor(name):
    return re.sub(r'[\s_]+', '-', re.sub(r'[^\w\s-]', '', name.lower())).strip('-')


def build(root, go, run):
    root = pathlib.Path(root).resolve()
    manifest = json.loads(run(go, 'mod', 'edit', '-json', cwd=root))
    pinned = next(item['Version'] for item in manifest['Require'] if item['Path'] == MODULE)
    selected = json.loads(run(go, 'list', '-mod=readonly', '-m', '-json', MODULE, cwd=root))
    if selected.get('Replace') or selected.get('Version') != pinned:
        raise ValueError('SDK reference requires the compiler require pin without replace')
    downloaded = json.loads(run(go, 'mod', 'download', '-json', MODULE + '@' + pinned, cwd=root))
    commit = downloaded.get('Origin', {}).get('Hash', '')
    if (downloaded.get('Path') != MODULE or downloaded.get('Version') != pinned
            or not re.fullmatch('[0-9a-f]{40}', commit)
            or not downloaded.get('Sum') or not downloaded.get('GoModSum')
            or downloaded['Sum'] != selected.get('Sum')
            or downloaded['GoModSum'] != selected.get('GoModSum')
            or pathlib.Path(downloaded['Dir']).resolve() != pathlib.Path(selected['Dir']).resolve()):
        raise ValueError('SDK reference requires verified public module provenance')
    packages = json.loads(run(go, 'run', str(root / 'website/sdk_reference.go'), downloaded['Dir'], cwd=root))
    pages = []
    for package in packages:
        name = package['package'].removeprefix(MODULE + '/')
        identity, category, title = PAGES[name]
        source_base = 'https://github.com/goxide-lang/syntax/blob/' + commit + '/'
        lines = [INTRO[name],
                 f'本文从编译器锁定的公开 SDK `{pinned}` 自动生成，包含全部公开声明与可调用方法。',
                 f'导入路径：`{package["package"]}`。源码版本：[{commit[:12]}](https://github.com/goxide-lang/syntax/tree/{commit}/{name})。',
                 '声明与示例使用 SDK 原有的 Go 语法；说明注释保留发布版本原文。',
                 package['doc'].strip(), '## API 索引']
        for kind, label in [('func', '函数'), ('type', '类型'), ('const', '常量'), ('var', '变量')]:
            items = [item for item in package['declarations'] if item['kind'] == kind]
            if items:
                lines.append(label + '：' + ' · '.join(f'[{item["name"]}](#{anchor(item["name"])})' for item in items))

        def emit(item, heading, level):
            lines.extend([level + ' ' + heading, '```go\n' + item['signature'].strip() + '\n```'])
            if item.get('promoted'):
                lines.append('此方法通过嵌入类型提供，可直接在该公开类型上调用。')
            if item['doc'].strip():
                lines.append(item['doc'].strip())
            lines.append(f'[查看源码]({source_base}{item["file"]}#L{item["line"]})')

        for kind, label in [('func', '函数'), ('type', '类型与方法'), ('const', '常量'), ('var', '变量')]:
            items = [item for item in package['declarations'] if item['kind'] == kind]
            if not items:
                continue
            lines.append('## ' + label)
            for item in items:
                emit(item, item['name'], '###')
                for method in item.get('methods', []):
                    emit(method, item['name'] + '.' + method['name'], '####')
        if package['examples']:
            lines.append('## 官方示例')
            for example in package['examples']:
                lines.extend(['### ' + example['name'], example['doc'].strip(),
                              '```go\n' + example['code'].strip() + '\n```'])
                if example['output']:
                    lines.extend(['预期输出：', '```text\n' + example['output'].rstrip() + '\n```'])
                lines.append(f'[示例源码]({source_base}{example["file"]}#L{example["line"]})')
        lines.extend(['## 继续阅读', ' · '.join(f'[{other_title}](/docs/?page={other_id})'
                     for other_id, _, other_title in PAGES.values() if other_id != identity)])
        pages.append(dict(id=identity, category=category, title=title,
                          markdown='\n\n'.join(line for line in lines if line) + '\n'))
    return pages
