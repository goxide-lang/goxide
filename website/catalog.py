"""Version-bound public summaries; private evidence stays in this repository."""
import hashlib, json, pathlib, re, subprocess
ROOT = pathlib.Path(__file__).resolve().parent.parent

def load():
    data = json.loads((ROOT/'website/catalog-source.json').read_text())
    assert data['schemaVersion'] == 1
    assert re.fullmatch('[0-9a-f]{40}', data['compilerCommit'])
    ids = [e['id'] for e in data['examples']]
    assert len(ids) == len(set(ids))
    assert set(ids) == {p.stem for p in (ROOT/'website/examples').glob('*.hgo')}
    cards = data['items']; assert len({c['id'] for c in cards}) == len(cards)
    coverage = {r for c in cards for r in c['rfcs']}
    assert {f'{n:04}' for n in range(1,25)} <= coverage
    for c in cards:
        assert c['status'] in data['statusLabels'] and c['topic'] in data['topics']
        assert c['support'] in ['browser','cli','unavailable']
        assert c['summary'] and c['usage'] and c['limitation'] and c['evidence']
        assert set(c['examples']) <= set(ids)
        assert bool(c['examples']) == (c['support'] == 'browser')
        if c['status'] in ['proposal','accepted']: assert c['support'] == 'unavailable'
        if c['status'] == 'verified': assert c['examples'] and any('_test.go' in e for e in c['evidence'])
        for name in c['checks']: assert (ROOT/'website/checks/catalog'/f'{name}.hgo').is_file()
        for e in c['evidence']:
            evidence_bytes(data,e)
    return data

def evidence_bytes(data, entry):
    if entry.startswith('@'):
        key, path = entry[1:].split(':',1)
        return subprocess.check_output(['git','show',data['evidenceRefs'][key]+':'+path],cwd=ROOT)
    # Always bind evidence to the actual published compiler, not a mutable worktree.
    return subprocess.check_output(['git','show',data['compilerCommit']+':'+entry],cwd=ROOT)

def export(data, checks):
    out = {k:data[k] for k in ['schemaVersion','compilerCommit','reviewedAt','statusLabels','topics']}
    out['items'] = [{k:v for k,v in c.items() if k!='evidence'} for c in data['items']]
    out['checks'] = checks
    out['rules'] = ['状态针对卡片写明的范围，不代表整篇 RFC 已完成。','只有通过真实 CLI 与浏览器对照的单文件例子提供 Playground 入口。','开发分支或任务进展独立标注，不会升级当前线上编译器状态。','提案语法、Go 机制探针和文档标题都不能单独作为实现证据。']
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
    (ROOT/'website/FEATURES.md').write_text('\n'.join(lines))

if __name__ == '__main__':
    data=load();write_index(data);print(f"catalog schema/evidence OK: {len(data['items'])} topics, RFC 0001–0024 covered")
