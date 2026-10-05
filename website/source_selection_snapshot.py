"""Execute source selection against a deterministic local Go module proxy."""
import pathlib,tempfile,subprocess,os,json,zipfile,pty,select,time

def build(ROOT,GO):
 with tempfile.TemporaryDirectory(prefix='goxide-site-source-') as tmp:
  base=pathlib.Path(tmp);old=base/'old-project';old.mkdir();proxy=base/'proxy/example.org/d/@v';proxy.mkdir(parents=True)
  version='v1.2.0';mod='module example.org/d\ngo 1.27\n';remote='package d\nconst Value="remote"\n'
  (proxy/(version+'.mod')).write_text(mod);(proxy/(version+'.info')).write_text(json.dumps(dict(Version=version,Time='2026-10-05T00:00:00Z')));(proxy/'list').write_text(version+'\n')
  with zipfile.ZipFile(proxy/(version+'.zip'),'w') as z:
   for name,data in [('go.mod',mod),('d.go',remote),('LICENSE','Demonstration fixture.\n')]:z.writestr('example.org/d@'+version+'/'+name,data)
  (old/'go.mod').write_text('module example.org/source-demo\ngo 1.27\nrequire example.org/d v1.2.0\n')
  (old/'main.go').write_text('package main\nimport("fmt";"example.org/d")\nfunc main(){fmt.Println(d.Value)}\n')
  env=dict(os.environ,GOTOOLCHAIN='local',GOWORK='off',GOPROXY=(base/'proxy').as_uri(),GOSUMDB='off',GOMODCACHE=str(base/'warm-cache'))
  for args in [['mod','tidy'],['mod','vendor']]:subprocess.run([GO,*args],cwd=old,env=env,check=True,capture_output=True)
  supplied='package d\nconst Value="supplied modification"\n';(old/'vendor/example.org/d/d.go').write_text(supplied)
  env['GOPROXY']='off'
  before={str(p.relative_to(old)):(p.read_bytes(),p.stat().st_mode&0o777) for p in old.rglob('*') if p.is_file()}
  def clean(s):return s.replace(str(base)+'/', '').replace('\r\n','\n')
  def call(args,fail=False,env_override=None):
   p=subprocess.run([str(ROOT/'bin/goxide'),args[0],'--go',GO,*args[1:]],env=env_override or env,capture_output=True,text=True,input='example.org/d=remote\n')
   assert (p.returncode!=0)==fail,p.stderr
   return clean(p.stderr if fail else p.stdout)
  flags=['import-go','--bin','app=.','--offline'];runs=[]
  local=base/'local-project';out=call([*flags,'--out',str(local),str(old)])
  runs.append(dict(command='非TTY，管道含remote选择仍不读取：goxide import-go --bin app=. --offline --out local-project old-project',stdout=out))
  out=call(['run','--bin','app','--offline','--locked',str(local)]);assert out=='supplied modification\n';runs.append(dict(command='goxide run --bin app --offline --locked local-project',stdout=out))
  selected=base/'remote-project';args=[*flags,'--source','example.org/d=remote','--out',str(selected),str(old)]
  rejected=call(args,fail=True);assert '--accept-source-diff' in rejected and not selected.exists();runs.append(dict(command='goxide import-go --bin app=. --offline --source example.org/d=remote --out remote-project old-project（差异未确认，预期失败）',stdout=rejected))
  out=call([*flags,'--source','example.org/d=remote','--accept-source-diff','example.org/d','--out',str(selected),str(old)]);runs.append(dict(command='goxide import-go --bin app=. --offline --source example.org/d=remote --accept-source-diff example.org/d --out remote-project old-project',stdout=out))
  out=call(['run','--bin','app','--offline','--locked',str(selected)]);assert out=='remote\n';runs.append(dict(command='goxide run --bin app --offline --locked remote-project',stdout=out))
  audit=json.loads((selected/'goxide-import.json').read_text());source=audit['source_choices']['example.org/d'];assert source['differences_accepted'] and source['status']=='modified/unknown' and source['differences']
  assert (selected/audit['local_modules']['example.org/d']/'d.go').read_text()==supplied
  cold=base/'cold-project';error=call([*flags,'--source','example.org/d=remote','--out',str(cold),str(old)],fail=True,env_override=dict(env,GOMODCACHE=str(base/'empty-cache')));assert 'offline' in error and not cold.exists();runs.append(dict(command='空module cache下重复offline remote迁入（预期失败）',stdout=error))
  # Real PTY, using the same binary and cached module fixture.
  terminal=base/'tty-project';master,slave=pty.openpty();p=subprocess.Popen([str(ROOT/'bin/goxide'),'import-go','--go',GO,'--bin','app=.','--offline','--out',str(terminal),str(old)],stdin=slave,stdout=slave,stderr=slave,env=env);os.close(slave);data=b'';sent=False;deadline=time.monotonic()+90
  try:
   while time.monotonic()<deadline:
    if select.select([master],[],[],.1)[0]:
     try:chunk=os.read(master,65536)
     except OSError:break
     if not chunk:break
     data+=chunk
     if not sent and b'cancel aborts:' in data:os.write(master,b'example.org/d=remote\nremote example.org/d\n');sent=True
    if p.poll() is not None:break
   assert p.wait(timeout=5)==0 and sent,data.decode(errors='replace')
  finally:
   if p.poll() is None:p.kill();p.wait()
   os.close(master)
  assert terminal.exists();transcript=clean(data.decode());assert 'example.org/d' in transcript and 'remote' in transcript
  tty_choice=json.loads((terminal/'goxide-import.json').read_text())['source_choices']['example.org/d']
  assert tty_choice['source']=='remote' and tty_choice['differences_accepted']
  tty_result=call(['run','--bin','app','--offline','--locked',str(terminal)]);assert tty_result=='remote\n'
  runs.append(dict(command='真实TTY：选择 example.org/d=remote，再逐模块确认 remote example.org/d',stdout=transcript))
  runs.append(dict(command='goxide run --bin app --offline --locked tty-project',stdout=tty_result))
  assert {str(p.relative_to(old)):(p.read_bytes(),p.stat().st_mode&0o777) for p in old.rglob('*') if p.is_file()}==before
  files=[dict(path='old-project/'+n,source=b.decode()) for n,(b,mode) in sorted(before.items())]
  files += [dict(path='remote-project/'+n,source=clean((selected/n).read_text())) for n in ['goxide.toml','goxide-import.json']]
  files.append(dict(path='保留的原供应 d.go',source=(selected/audit['local_modules']['example.org/d']/'d.go').read_text()))
  return dict(id='source-selection',title='来源选择：默认local、差异确认与真实TTY',note='仅本地CLI实测；example.org/d v1.2.0由临时file proxy制造并预热，随后GOPROXY=off。不是公共安装地址或浏览器网络服务。stdout/拒绝诊断与PTY合并输出据实记录，仅去临时根前缀并规范CRLF。remote只验证当前供应文件集合，原修改副本始终保留；空缓存offline失败。',files=files,runs=runs,verification=dict(nonTTYDoesNotConsumeInput=True,missingConfirmationRejected=True,explicitDifferenceConfirmed=True,suppliedBytesPreserved=True,coldOfflineRejected=True,realPTY=True,originalUnchanged=True))
