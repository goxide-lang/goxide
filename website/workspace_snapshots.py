"""Real CLI workspace examples built locally; only reviewed snapshots are public."""
import pathlib, tempfile, shutil, subprocess, os, json, zipfile

def build(ROOT, GO):
 with tempfile.TemporaryDirectory(prefix='goxide-site-workspace-') as tmp:
  base=pathlib.Path(tmp);ws=base/'workspace';shutil.copytree(ROOT/'examples/workspace',ws,ignore=shutil.ignore_patterns('README.md'))
  proxy=base/'proxy';cache=base/'module-cache';versions=proxy/'example.org/version/@v';versions.mkdir(parents=True)
  for v in ['v1.0.0','v1.1.0']:
   mod='module example.org/version\n\ngo 1.27\n';code=f'package version\nfunc Value() string {{ return "{v}" }}\n'
   (versions/(v+'.mod')).write_text(mod);(versions/(v+'.info')).write_text(json.dumps(dict(Version=v,Time='2026-10-04T00:00:00Z')))
   with zipfile.ZipFile(versions/(v+'.zip'),'w') as z:
    z.writestr('example.org/version@'+v+'/go.mod',mod);z.writestr('example.org/version@'+v+'/version.go',code)
  (versions/'list').write_text('v1.0.0\nv1.1.0\n')
  for name,v in [('service','v1.0.0'),('worker','v1.1.0')]:
   p=ws/'apps'/name/'goxide.toml';p.write_text(p.read_text()+f'\n[dependencies.version]\nmodule = "example.org/version"\npackage = "example.org/version"\nversion = "{v}"\n')
   p=ws/'apps'/name/'web.gox';p.write_text('package main\nuse fmt;\nuse shared;\nuse version;\nfunc main(){fmt::Println(version::Value(), shared::Mode())}\n')
  env=dict(os.environ,GOTOOLCHAIN='local',GOWORK='off',GOPROXY=proxy.as_uri(),GOSUMDB='off',GOMODCACHE=str(cache))
  def clean(s):return s.replace(str(base)+'/', '').replace(str(base),'fixture')
  def call(*args):
   p=subprocess.run([str(ROOT/'bin/goxide'),args[0],'--go',GO,*args[1:]],env=env,text=True,capture_output=True)
   assert p.returncode==0,p.stderr
   return p.stdout
  files=[dict(path='workspace/'+str(p.relative_to(ws)),source=p.read_text()) for p in sorted(ws.rglob('*')) if p.is_file()]
  runs=[]
  for flags,expect in [(['run','--bin','web'],'v1.1.0 json\n'),(['run','--package','example.org/worker','--bin','web'],'v1.1.0 yaml\n'),(['run','--package','example.org/service','--example','hello'],'json\n')]:
   out=call(*flags,str(ws));assert out==expect,(flags,out)
   runs.append(dict(command='goxide '+' '.join(flags)+' workspace',stdout=out))
  output=call('build','--workspace','--offline','--locked',str(ws))
  binaries=output.splitlines();assert len(binaries)==3
  runs.append(dict(command='goxide build --workspace --offline --locked workspace',stdout=clean(output)))
  for b in binaries:
   out=subprocess.check_output([b],env=env,text=True);assert out in ['v1.1.0 yaml\n','yaml\n'],out
   runs.append(dict(command='运行 '+clean(b),stdout=out))
  out=call('run','--bin','web','--offline','--locked',str(ws));assert out=='v1.1.0 json\n'
  runs.append(dict(command='goxide run --bin web --offline --locked workspace',stdout=out))
  out=call('test','--workspace','--offline','--locked',str(ws));runs.append(dict(command='goxide test --workspace --offline --locked workspace',stdout=out))
  locks=list(ws.rglob('goxide.lock'));assert locks==[ws/'goxide.lock'];lock=json.loads(locks[0].read_text());assert lock['version']==3
  files.append(dict(path='workspace/goxide.lock（路径已归一化）',source=clean(locks[0].read_text())))
  selected=lock['unified']['modules'];assert 'v1.1.0' in json.dumps(selected)
  snapshot=dict(id='workspace',title='Workspace：统一版本、选中 feature 与多目标',note='真实本地 CLI；example.org/version 的两个版本由本地 file proxy fixture 提供，不是公共可安装模块。service 要求1.0.0、worker要求1.1.0：单独service仍锁1.1.0，但只启用json；批量构建合并yaml，之后单独运行恢复json。example只在显式选择时运行。锁和输出中的临时根目录前缀已移除；不是可直接复用的锁文件。',files=files,runs=runs,verification=dict(rootLockOnly=True,schema=3,unifiedVersion='v1.1.0',selectedFeatureRestored=True,defaultBatchBins=3))
  # A separate local Git fixture makes copied-lock behavior observable.
  repos=base/'repos';repos.mkdir();git_env=dict(env,GIT_CONFIG_GLOBAL='/dev/null',GIT_CONFIG_NOSYSTEM='1',GIT_AUTHOR_NAME='Fixture',GIT_AUTHOR_EMAIL='fixture@example.invalid',GIT_COMMITTER_NAME='Fixture',GIT_COMMITTER_EMAIL='fixture@example.invalid',GIT_AUTHOR_DATE='2026-10-04T00:00:00Z',GIT_COMMITTER_DATE='2026-10-04T00:00:00Z')
  def git(repo,*args):return subprocess.check_output(['git','-c','core.hooksPath=/dev/null',*args],cwd=repo,env=git_env,stderr=subprocess.DEVNULL,text=True).strip()
  for name in ['used','unused']:
   repo=repos/name;repo.mkdir();(repo/'go.mod').write_text(f'module example.org/{name}\n\ngo 1.27\n');(repo/'lib.go').write_text(f'package {name}\nfunc Value() string {{return "one"}}\n');git(repo,'init','-b','main');git(repo,'add','.');git(repo,'commit','-m','one')
  pinned=git(repos/'used','rev-parse','HEAD')
  root=base/'git-workspace';root.mkdir();(root/'goxide.toml').write_text('[workspace]\nformat=1\nmembers=["app","server"]\ndefault-members=["app"]\n')
  for name,dep in [('app','used'),('server','unused')]:
   p=root/name;p.mkdir();(p/'goxide.toml').write_text(f'[package]\nformat=2\nmodule="example.org/{name}"\ngo="1.27"\n[dependencies]\ndep={{module="example.org/{dep}",git="{repos/dep}",rev="main"}}\n[[bin]]\nname="app"\npath="main.go"\n');(p/'main.go').write_text(f'package main\nimport("fmt";d "example.org/{dep}")\nfunc main(){{fmt.Println(d.Value())}}\n')
  out=call('run','--package','example.org/app',str(root));assert out=='one\n'
  locked=(root/'goxide.lock').read_bytes();(repos/'used/lib.go').write_text('package used\nfunc Value() string {return "two"}\n');git(repos/'used','add','.');git(repos/'used','commit','-m','two');moved=git(repos/'used','rev-parse','HEAD');assert pinned!=moved
  shutil.rmtree(repos/'unused');split=base/'standalone';split.mkdir()
  for n in ['goxide.toml','main.go']:shutil.copyfile(root/'app'/n,split/n)
  (split/'goxide.lock').write_bytes(locked)
  assert (split/'goxide.lock').read_bytes()==locked
  splitruns=[]
  for flags in [['run','--locked'],['run','--offline','--locked']]:
   out=call(*flags,str(split));assert out=='one\n';splitruns.append(dict(command='goxide '+' '.join(flags)+' standalone',stdout=out))
  splitfiles=[dict(path='git-workspace/'+name,source=clean((root/name).read_text())) for name in ['goxide.toml','app/goxide.toml','app/main.go','server/goxide.toml','server/main.go']]
  splitfiles += [dict(path='standalone/'+n,source=clean((split/n).read_text())) for n in ['goxide.toml','main.go','goxide.lock']]
  splitshot=dict(id='workspace-split',title='复制根锁拆出：沿用 Git pin，忽略无关来源',note='两个本地 Git fixture 的main最初返回one；锁定后used/main改为two，删除无关unused仓库。复制根锁与已独立配置到standalone，仍输出one。这里清单已显式配置来源；workspace=true须先物化，path源码不会随锁复制。临时Git地址/锁路径已移除根目录前缀，不是公共安装地址；首次locked可读取已锁内容，随后offline复用缓存。',files=splitfiles,runs=splitruns,verification=dict(copiedRootLock=True,pinnedCommit=pinned,movedBranchCommit=moved,unreachableSourceRemoved=True))
  return [snapshot,splitshot]
