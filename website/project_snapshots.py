"""Build-time CLI evidence only; no executable project service is shipped."""
import hashlib, json, pathlib, shutil, tempfile, tomllib

def migration(ROOT, GO, run):
    with tempfile.TemporaryDirectory(prefix='goxide-web-import-') as tmp:
        old=pathlib.Path(tmp)/'old-project';new=pathlib.Path(tmp)/'new-project'
        shutil.copytree(ROOT/'website/cli-examples/import-go',old)
        def snapshot(): return {str(p.relative_to(old)):p.read_bytes() for p in old.rglob('*') if p.is_file()}
        before=snapshot();goxide=str(ROOT/'bin/goxide')
        plan=run(goxide,'import-go','--go',GO,'--plan','--bin','app=.',str(old)).decode()
        assert snapshot()==before and not new.exists()
        published_stdout=run(goxide,'import-go','--go',GO,'--out',str(new),'--bin','app=.','--offline',str(old)).decode()
        published_stdout=published_stdout.replace(str(new),'new-project').replace(str(old),'old-project')
        assert str(tmp) not in published_stdout
        assert snapshot()==before
        assert not any((new/n).exists() for n in ['go.mod','go.sum','target','.goxide'])
        assert (new/'goxide.lock').is_file()
        assert tomllib.loads((new/'goxide.toml').read_text())==tomllib.loads(plan)
        files=[dict(path='old-project/'+name,source=data.decode()) for name,data in sorted(before.items())]
        files += [dict(path='new-project/'+name,source=(new/name).read_text()) for name in ['goxide.toml','goxide.lock','main.go']]
        stdout=run(goxide,'run','--go',GO,'--bin','app','--offline','--locked',str(new)).decode()
        assert stdout=='same source, one manifest\n'
        assert snapshot()==before and not (new/'go.mod').exists()
        assert list((new/'target').rglob('go.mod')), 'generated module metadata missing'
        generated=sorted({p.read_text() for p in (new/'target').rglob('*.go') if 'same source, one manifest' in p.read_text()})
        assert generated
        files.extend(dict(path=f'target 生成 Go {i+1}（只读）',source=go) for i,go in enumerate(generated))
        return dict(id='import-go',title='普通 Go → TOML：保留原工程的真实迁入',note='下列输出来自真实 CLI；仅将本机工程绝对路径替换为 old-project / new-project。原文件未变、目标根无 Go 元数据等结论由构建断言独立核验。',files=files,runs=[
            dict(command='goxide import-go --plan --bin app=. old-project',stdout=plan),
            dict(command='goxide import-go --out new-project --bin app=. --offline old-project',stdout=published_stdout),
            dict(command='goxide run --bin app --offline --locked new-project',stdout=stdout)],
            verification=dict(originalFilesUnchanged=True,targetModuleMetadata=True,sourceDigests={name:hashlib.sha256(data).hexdigest() for name,data in before.items()}))

def vendor(ROOT, GO, run):
    import os, subprocess
    with tempfile.TemporaryDirectory(prefix='goxide-web-vendor-') as tmp:
        base=pathlib.Path(tmp);old=base/'old-project';dep=base/'dep';new=base/'new-project'
        old.mkdir();dep.mkdir()
        (dep/'go.mod').write_text('module example.org/catalogdep\n\ngo 1.27\n')
        (dep/'dep.go').write_text('package catalogdep\nfunc Value() string { return "original" }\n')
        (dep/'LICENSE').write_text('Example fixture: freely reusable demonstration code.\n')
        (old/'go.mod').write_text('module example.org/vendor-demo\n\ngo 1.27\n\nrequire example.org/catalogdep v1.0.0\nreplace example.org/catalogdep => ../dep\n')
        (old/'main.go').write_text('package main\nimport ("fmt"; "example.org/catalogdep")\nfunc main() { fmt.Println(catalogdep.Value()) }\n')
        run(GO,'mod','vendor',cwd=old)
        (old/'vendor/example.org/catalogdep/dep.go').write_text('package catalogdep\nfunc Value() string { return "vendored bytes" }\n')
        shutil.rmtree(dep)
        before={str(p.relative_to(old)):p.read_bytes() for p in old.rglob('*') if p.is_file()}
        env=dict(os.environ,GOTOOLCHAIN='local',GOWORK='off',GOPROXY='off',GOSUMDB='off',GOMODCACHE=str(base/'empty-module-cache'))
        goxide=str(ROOT/'bin/goxide')
        def call(*args,fail=False):
            p=subprocess.run([goxide,*args],env=env,capture_output=True,text=True)
            assert (p.returncode!=0)==fail,p.stderr
            out=(p.stderr if fail else p.stdout).replace(str(base)+'/', '')
            assert str(base) not in out
            return out
        plan=call('import-go','--go',GO,'--bin','app=.',str(old))
        assert 'local-modules/' in plan and '[vendor]' not in plan
        imported=call('import-go','--go',GO,'--out',str(new),'--bin','app=.','--offline',str(old))
        stdout=call('run','--go',GO,'--bin','app','--offline','--locked',str(new))
        assert stdout=='vendored bytes\n'
        files=[dict(path='old-project/'+n,source=b.decode()) for n,b in sorted(before.items())]
        files += [dict(path='new-project/'+n,source=(new/n).read_text()) for n in ['goxide.toml','goxide.lock']]
        locked=json.loads((new/'goxide.lock').read_text())
        assert 'vendor' not in locked
        audit=json.loads((new/'goxide-import.json').read_text())
        assert not (new/'vendor').exists()
        for name in ['go.mod','dep.go']:
            relative=pathlib.Path(audit['local_modules']['example.org/catalogdep'])/name
            files.append(dict(path='new-project/'+str(relative),source=(new/relative).read_text()))
        files.append(dict(path='new-project/goxide-import.json',source=(new/'goxide-import.json').read_text().replace(str(base)+'/', '')))
        assert {str(p.relative_to(old)):p.read_bytes() for p in old.rglob('*') if p.is_file()}==before
        path=new/audit['local_modules']['example.org/catalogdep']/'dep.go';path.write_text(path.read_text().replace('vendored bytes','changed bytes'))
        edited=call('run','--go',GO,'--bin','app','--offline','--locked',str(new))
        assert edited=='changed bytes\n'
        return dict(id='vendor',title='供应源码：本地模块冷离线与正常编辑',note='在空 module cache、GOPROXY=off、GOSUMDB=off 下实测；原 replace 目录已移除，Go 构建缓存可用。本机绝对路径仅替换为示例目录名。修改的 vendor 与上游是否相同：未检查，不联网比较。',files=files,runs=[dict(command='goxide import-go --bin app=. old-project',stdout=plan),dict(command='goxide import-go --out new-project --bin app=. --offline old-project',stdout=imported),dict(command='goxide run --bin app --offline --locked new-project',stdout=stdout),dict(command='修改本地模块 dep.go 后重复 locked 运行',stdout=edited)],verification=dict(coldModuleCache=True,proxyOff=True,originalFilesUnchanged=True,localMutationRebuilt=True))

def cli(ROOT, GO, run):
    import os, subprocess
    with tempfile.TemporaryDirectory(prefix='goxide-web-cli-') as tmp:
        p=pathlib.Path(tmp)/'args.go';p.write_text('package main\nimport ("fmt"; "os")\nfunc main() { fmt.Printf("%q\\n", os.Args[1:]) }\n')
        args=['--name','value','-x','','--','$(literal)']
        out=run(str(ROOT/'bin/goxide'),'run','--go',GO,str(p),'--',*args).decode()
        assert out=='["--name" "value" "-x" "" "--" "$(literal)"]\n'
        runs=[dict(command='goxide run args.go -- --name value -x \'\' -- \'$(literal)\'',stdout=out)]
        for flag in ['-go','-offline']:
            result=subprocess.run([str(ROOT/'bin/goxide'),'run',flag,str(p)],capture_output=True,text=True)
            assert result.returncode!=0 and '--'+flag[1:] in result.stderr
            runs.append(dict(command='goxide run '+flag+' args.go（预期失败）',stdout=result.stderr.replace(str(p),'args.go')))
        return dict(id='cli-options',title='CLI：run 参数原样透传与旧选项拒绝',files=[dict(path='args.go',source=p.read_text())],runs=runs)
