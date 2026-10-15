"""Version-bound public summaries; private evidence stays in this repository."""
import hashlib, json, pathlib, re, subprocess
ROOT = pathlib.Path(__file__).resolve().parent.parent

def validate_identities(data):
    def require_hex(value, width, label):
        if not isinstance(value, str) or not re.fullmatch(f'[0-9a-f]{{{width}}}', value):
            raise ValueError(f'{label} must be a complete {width}-digit lowercase hexadecimal identity')
    require_hex(data.get('compilerCommit'), 40, 'compilerCommit')
    require_hex(data.get('compilerDigest'), 64, 'compilerDigest')
    for key, ref in data.get('evidenceRefs', {}).items():
        require_hex(ref, 40, f'evidenceRefs[{key}]')
    for entry, snapshot in data.get('evidenceSnapshots', {}).items():
        require_hex(snapshot.get('sourceCommit'), 40, f'{entry} sourceCommit')
        require_hex(snapshot.get('sha256'), 64, f'{entry} sha256')
        require_hex(snapshot.get('gitBlob'), 40, f'{entry} gitBlob')

def load():
    data = json.loads((ROOT/'website/catalog-source.json').read_text())
    assert data['schemaVersion'] == 1
    validate_identities(data)
    ids = [e['id'] for e in data['examples']]
    assert len(ids) == len(set(ids))
    assert set(ids) == {p.stem for p in (ROOT/'website/examples').glob('*.gox')}
    cards = data['items']; assert len({c['id'] for c in cards}) == len(cards)
    coverage = {r for c in cards for r in c['rfcs']}
    assert {f'{n:04}' for n in range(1,27)} <= coverage
    for c in cards:
        assert c['status'] in data['statusLabels'] and c['topic'] in data['topics']
        assert c['support'] in ['browser','cli','unavailable']
        assert c['summary'] and c['usage'] and c['limitation'] and c['evidence']
        assert set(c['examples']) <= set(ids)
        assert bool(c['examples']) == (c['support'] == 'browser')
        if c['status'] in ['proposal','accepted']: assert c['support'] == 'unavailable'
        if c['status'] == 'verified': assert c['examples'] and any('_test.go' in e for e in c['evidence'])
        for name in c['checks']: assert (ROOT/'website/checks/catalog'/f'{name}.gox').is_file()
        for e in c['evidence']:
            evidence_bytes(data,e)
    return data

def evidence_bytes(data, entry):
    validate_identities(data)
    if entry.startswith('@'):
        key, path = entry[1:].split(':',1)
        # A proposal may live on a non-ancestor branch. Keep its exact reviewed
        # bytes here rather than requiring an unrelated ref in a clean clone.
        snapshot = data.get('evidenceSnapshots', {}).get(entry)
        if not snapshot:
            raise ValueError(f'versioned evidence snapshot is required: {entry}')
        if (snapshot['sourceCommit'] != data['evidenceRefs'][key] or
                snapshot['sourcePath'] != path):
            raise ValueError(f'evidence snapshot source does not match reference: {entry}')
        target = (ROOT / snapshot['path']).resolve()
        if not target.is_relative_to((ROOT / 'website/evidence').resolve()):
            raise ValueError(f'evidence snapshot must be inside website/evidence: {entry}')
        content = target.read_bytes()
        git_blob = hashlib.sha1(b'blob ' + str(len(content)).encode() + b'\0' + content).hexdigest()
        if (hashlib.sha256(content).hexdigest() != snapshot['sha256'] or
                git_blob != snapshot['gitBlob']):
            raise ValueError(f'evidence snapshot hash differs from reviewed source: {entry}')
        return content
    # Historical review evidence remains bound to its reviewed commit.
    return subprocess.check_output(['git','show',data['compilerCommit']+':'+entry],cwd=ROOT)

def export(data, checks):
    out = {k:data[k] for k in ['schemaVersion','compilerCommit','reviewedAt','statusLabels','topics']}
    out['items'] = [{k:v for k,v in c.items() if k!='evidence'} for c in data['items']]
    out['checks'] = checks
    out['rules'] = ['状态针对卡片写明的范围，不代表整篇 RFC 已完成。','示例由清单记录的编译器重新生成并运行；历史证据基线与当前编译器来源分别标注。','只有通过真实 CLI 与浏览器对照的例子提供 Playground 入口。','MVP 暂停入口不发布运行快照；提案语法、Go 机制探针和文档标题不能单独作为实现证据。']
    return out

def write_index(data):
    lines=['# 网站 RFC 特性状态索引','',f"编译器基线：`{data['compilerCommit']}`；证据复核日期：{data['reviewedAt']}。",'',
           '由 `website/catalog-source.json` 生成。状态是限定范围的人工判断；构建检查引用、源码版本、示例输出与拒绝例。此索引保留私有证据，公开构建仅导出摘要。','',
           '| 特性 | RFC | 状态 | 当前入口 | 实现/测试/设计证据（基线或注明的开发快照） |','| --- | --- | --- | --- | --- |']
    for c in data['items']:
        evidence=[]
        for entry in c['evidence']:
            digest=hashlib.sha256(evidence_bytes(data,entry)).hexdigest()[:12]
            evidence.append(f'`{entry}` ({digest})')
        lines.append('| '+' | '.join([c['title'],', '.join(c['rfcs']),data['statusLabels'][c['status']],c['support'],'; '.join(evidence)])+' |')
    lines += ['', '开发快照：'+json.dumps(data['evidenceRefs'],ensure_ascii=False), '', '管理建议：规范留在编译器仓库随版本评审；Issue 讨论/跟踪，PR 变更规范与实现；公共网站只发布审核后的摘要。未创建新仓库或 Issue。','']
    for entry, snapshot in data.get('evidenceSnapshots', {}).items():
        lines += [f"提案原文快照 `{entry}`：`{snapshot['path']}`；来源 `{snapshot['sourceCommit']}:{snapshot['sourcePath']}`，SHA-256 `{snapshot['sha256']}`，Git blob `{snapshot['gitBlob']}`。仅供仓内审核，不导出到公开清单。", '']
    (ROOT/'website/FEATURES.md').write_text('\n'.join(lines))

if __name__ == '__main__':
    data=load();write_index(data);print(f"catalog schema/evidence OK: {len(data['items'])} topics, RFC 0001–0026 covered")
