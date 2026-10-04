"""Build-time CLI evidence only; no executable project service is shipped."""
import hashlib, pathlib, shutil, tempfile

def migration(ROOT, GO, run):
    with tempfile.TemporaryDirectory(prefix='hgo-web-import-') as tmp:
        old=pathlib.Path(tmp)/'old-project';new=pathlib.Path(tmp)/'new-project'
        shutil.copytree(ROOT/'website/cli-examples/import-go',old)
        def snapshot(): return {str(p.relative_to(old)):p.read_bytes() for p in old.rglob('*') if p.is_file()}
        before=snapshot();hgo=str(ROOT/'bin/hgo')
        plan=run(hgo,'import-go','--go',GO,'--plan','--bin','app=.',str(old)).decode()
        assert snapshot()==before and not new.exists()
        published_stdout=run(hgo,'import-go','--go',GO,'--out',str(new),'--bin','app=.','--offline',str(old)).decode()
        published_stdout=published_stdout.replace(str(new),'new-project').replace(str(old),'old-project')
        assert str(tmp) not in published_stdout
        assert snapshot()==before
        assert not any((new/n).exists() for n in ['go.mod','go.sum','hgo.lock','target','.hgo'])
        assert (new/'goxide.toml').read_text()==plan
        files=[dict(path='old-project/'+name,source=data.decode()) for name,data in sorted(before.items())]
        files += [dict(path='new-project/'+name,source=(new/name).read_text()) for name in ['goxide.toml','goxide.lock','main.go']]
        stdout=run(hgo,'run','--go',GO,'--bin','app','--offline','--locked',str(new)).decode()
        assert stdout=='same source, one manifest\n'
        assert snapshot()==before and not (new/'go.mod').exists()
        assert list((new/'target').rglob('go.mod')), 'generated module metadata missing'
        generated=sorted({p.read_text() for p in (new/'target').rglob('*.go') if 'same source, one manifest' in p.read_text()})
        assert generated
        files.extend(dict(path=f'target 生成 Go {i+1}（只读）',source=go) for i,go in enumerate(generated))
        return dict(id='import-go',title='普通 Go → TOML：保留原工程的真实迁入',note='下列输出来自真实 CLI；仅将本机工程绝对路径替换为 old-project / new-project。原文件未变、目标根无 Go 元数据等结论由构建断言独立核验。',files=files,runs=[
            dict(command='hgo import-go --plan --bin app=. old-project',stdout=plan),
            dict(command='hgo import-go --out new-project --bin app=. --offline old-project',stdout=published_stdout),
            dict(command='hgo run --bin app --offline --locked new-project',stdout=stdout)],
            verification=dict(originalFilesUnchanged=True,targetModuleMetadata=True,sourceDigests={name:hashlib.sha256(data).hexdigest() for name,data in before.items()}))
