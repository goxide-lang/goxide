"""Build-time examples for native workspace migration; no backend is published."""
import pathlib, tempfile, shutil, subprocess, os, json, tomllib, hashlib

def toml(data):
 lines=[]
 def emit(d, prefix=''):
  for k,v in d.items():
   if not isinstance(v,dict) and not (isinstance(v,list) and v and isinstance(v[0],dict)):lines.append(k+' = '+json.dumps(v,ensure_ascii=False))
  for k,v in d.items():
   key=prefix+'.'+k if prefix else k
   if isinstance(v,dict):lines.extend(['','['+key+']']);emit(v,key)
   elif isinstance(v,list) and v and isinstance(v[0],dict):
    for entry in v:lines.extend(['','[['+key+']]']);emit(entry,key)
 emit(data);return '\n'.join(lines)+'\n'

def build(ROOT, GO):
 shots=[]
 for vendored in [False,True]:
  with tempfile.TemporaryDirectory(prefix='goxide-site-work-import-') as tmp:
   base=pathlib.Path(tmp);old=base/'native';new=base/'migrated';(old/'app').mkdir(parents=True);(old/'lib').mkdir()
   def write(p,s):p.parent.mkdir(parents=True,exist_ok=True);p.write_text(s)
   write(old/'go.work','go 1.27\nuse (\n ./app\n ./lib\n)\n')
   for n in ['app','lib']:write(old/n/'go.mod',f'module example.org/{n}\ngo 1.27\n')
   write(old/'app/main.go','package main\nimport("fmt";"example.org/lib")\nfunc main(){fmt.Println(lib.Value())}\n')
   write(old/'lib/lib.go','package lib\nfunc Value() string{return "hello workspace"}\n')
   env=dict(os.environ,GOTOOLCHAIN='local',GOWORK='off',GOPROXY='off',GOSUMDB='off')
   def clean(s):return s.replace(str(base)+'/', '').replace(str(base),'fixture')
   def call(args,fail=False):
    p=subprocess.run([str(ROOT/'bin/goxide'),args[0],'--go',GO,*args[1:]],env=env,capture_output=True,text=True)
    assert (p.returncode!=0)==fail,p.stderr
    return clean(p.stderr if fail else p.stdout)
   if vendored:
    write(old/'dep/go.mod','module example.org/dep\ngo 1.27\n');write(old/'dep/dep.go','package dep\nfunc Value() string{return "original"}\n');write(old/'dep/LICENSE','Demonstration fixture: freely reusable.\n')
    write(old/'lib/go.mod','module example.org/lib\ngo 1.27\nrequire example.org/dep v1.0.0\nreplace example.org/dep => ../dep\n')
    write(old/'lib/lib.go','package lib\nimport "example.org/dep"\nfunc Value()string{return dep.Value()}\n')
    subprocess.run([GO,'work','vendor'],cwd=old,env=dict(env,GOWORK=str(old/'go.work')),check=True,capture_output=True)
    write(old/'vendor/example.org/dep/dep.go','package dep\nfunc Value()string{return "modified vendor"}\n');shutil.rmtree(old/'dep');env['GOMODCACHE']=str(base/'empty-module-cache')
   def original():return {str(p.relative_to(old)):(p.read_bytes(),p.stat().st_mode&0o777) for p in old.rglob('*') if p.is_file()}
   before=original();files=[dict(path='native/'+n,source=b.decode()) for n,(b,mode) in sorted(before.items())]
   plan=call(['import-go','--workspace','--plan','--offline',str(old)]);assert 'workspace=true' in plan or 'workspace = true' in plan
   imported=call(['import-go','--workspace','--out',str(new),'--offline',str(old)]);assert original()==before
   lock=json.loads((new/'goxide.lock').read_text());assert lock['version']==4
   assert not (new/'go.work').exists()
   app=new/'members/m0';lib=new/'members/m1';assert tomllib.loads((app/'goxide.toml').read_text())['go_mod']['module']=='example.org/app'
   files += [dict(path='migrated/'+n,source=clean((new/n).read_text())) for n in ['goxide.toml','members/m0/goxide.toml','members/m1/goxide.toml']]
   runs=[dict(command='goxide import-go --workspace --plan --offline native',stdout=plan),dict(command='goxide import-go --workspace --out migrated --offline native',stdout=imported)]
   out=call(['run','--package','example.org/app','--locked','--offline',str(new)]);assert out==('modified vendor\n' if vendored else 'hello workspace\n');runs.append(dict(command='goxide run --package example.org/app --locked --offline migrated',stdout=out))
   (lib/'lib.go').unlink()
   if vendored:
    with (lib/'goxide.toml').open('a') as config:config.write('\n[dependencies.dep]\ngo="example.org/dep"\nversion="v1.0.0"\n')
    write(lib/'lib.gox','package lib\nuse dep;\nmacro_rules! suffix { () => { " enhanced" }; }\nfunc Value()string{return dep::Value()+suffix!()}\n')
    out=call(['run','--package','example.org/app','--offline',str(new)]);assert out=='modified vendor enhanced\n'
    runs.append(dict(command='替换 lib.go 为 lib.gox 并声明 dep 根后更新锁：goxide run --package example.org/app --offline migrated',stdout=out))
    out=call(['run','--package','example.org/app','--locked','--offline',str(new)]);assert out=='modified vendor enhanced\n'
    runs.append(dict(command='固定新依赖根重复运行：goxide run --package example.org/app --locked --offline migrated',stdout=out))
    audit=json.loads((new/'goxide-import.json').read_text())
    assert not (new/'vendor').exists()
    assert 'vendor' not in tomllib.loads((new/'goxide.toml').read_text())['workspace']
    files.append(dict(path='migrated/goxide-import.json',source=clean((new/'goxide-import.json').read_text())))
    for relative in audit['local_modules'].values():
     for name in ['go.mod','dep.go']:
      p=new/relative/name
      if p.exists():files.append(dict(path='migrated/'+str(pathlib.Path(relative)/name),source=clean(p.read_text())))
    for name,outputs in audit['files'].items():
     for output in outputs:
      p=new/output
      assert (p.read_bytes(),p.stat().st_mode&0o777)==before['vendor/'+name]
   else:
    write(lib/'lib.gox','package lib\nfunc Value()string{if cfg!(feature("fancy")){return "enhanced workspace"};return "plain workspace"}\n')
    with (lib/'goxide.toml').open('a') as f:f.write('\n[features]\nfancy=[]\n')
    with (app/'goxide.toml').open('a') as f:f.write('\n[features]\ndefault=["member1/fancy"]\n')
    for flags,want in [([], 'enhanced workspace\n'),(['--no-default-features','--locked'],'plain workspace\n'),(['--locked'],'enhanced workspace\n')]:
     out=call(['run','--package','example.org/app',*flags,'--offline',str(new)]);assert out==want
     runs.append(dict(command='goxide run --package example.org/app '+' '.join(flags+['--offline','migrated']),stdout=out))
    # Materialize explicit dependency inheritance before copying the root lock.
    split=base/'standalone';rootdata=tomllib.loads((new/'goxide.toml').read_text())
    for source,name in [(app,'app'),(lib,'lib')]:
     data=tomllib.loads((source/'goxide.toml').read_text())
     for alias,dep in data.get('dependencies',{}).items():
      if dep.pop('workspace',False):
       resolved=dict(rootdata['workspace']['dependencies'][alias]);resolved.update(dep);resolved['path']='../lib';data['dependencies'][alias]=resolved
     write(split/name/'goxide.toml',toml(data))
     for p in source.iterdir():
      if p.suffix in ['.go','.gox']:shutil.copyfile(p,split/name/p.name)
    locked=(new/'goxide.lock').read_bytes();(split/'app/goxide.lock').write_bytes(locked)
    out=call(['run','--locked','--offline',str(split/'app')]);assert out=='enhanced workspace\n';assert (split/'app/goxide.lock').read_bytes()==locked
    runs.append(dict(command='显式物化成员配置并复制根锁后：goxide run --locked --offline standalone/app',stdout=out))
    files += [dict(path='standalone/'+n,source=clean((split/n).read_text())) for n in ['app/goxide.toml','lib/goxide.toml']]
   out=call(['test','--package','example.org/app','--locked','--offline',str(new)]);runs.append(dict(command='goxide test --package example.org/app --locked --offline migrated',stdout=out))
   files += [dict(path='增强后 members/m1/lib.gox',source=(lib/'lib.gox').read_text()),dict(path='增强后 members/m0/goxide.toml',source=(app/'goxide.toml').read_text()),dict(path='增强后 members/m1/goxide.toml',source=(lib/'goxide.toml').read_text())]
   assert original()==before
   shots.append(dict(id='workspace-import-vendor' if vendored else 'workspace-import',title='供应源码本地模块：修改字节保留与增强宏' if vendored else 'go.work 迁入：渐进 feature 与成员拆出',note='真实本地 CLI，原文件字节/权限不变，只发布新目录；输出中的临时根路径前缀已移除。'+('go work vendor 仅用于制造输入fixture；迁入不重建vendor。删除原replace目录并使用空module cache、proxy/sumdb关闭，Go构建缓存可用。供应源码已物化普通本地模块；独立运行只需显式本地路径和复制根锁，无vendor转换。' if vendored else '先在原workspace普通构建更新修改清单的锁；再进行locked feature切换。拆出显式物化继承/path并复制schema4根锁，锁原字节不改。示例不覆盖全部平台和Go政策组合。'),files=files,runs=runs,verification=dict(originalFilesAndModesUnchanged=True,schema=4,coldModuleCache=vendored,copiedRootLock=not vendored)))
 return shots
