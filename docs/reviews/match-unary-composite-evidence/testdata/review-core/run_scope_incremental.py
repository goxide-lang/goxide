from pathlib import Path
import shutil,subprocess,os,json,hashlib,sys
p=Path('/tmp/goxide-a04-review-20261006');cand=p/sys.argv[1];dest=cand/'scope-incremental';dest.mkdir()
env=os.environ.copy();env.update(GOROOT='/opt/homebrew/Cellar/go/1.27.1/libexec',GOTOOLCHAIN='local',GOWORK='off',GOPROXY='off',GOCACHE='/private/tmp/goxide-a04-20261006-ukafjzho/environment/go-build',GOMODCACHE='/private/tmp/goxide-a04-20261006-ukafjzho/environment/go-mod',TMPDIR='/private/tmp/goxide-a04-20261006-ukafjzho/environment/tmp')
rs=[]
for key,src,assertion in [('closure-first',p/'typed-child/closure-first/source',p/'typed-child/v2/closure-first/generated/assertions_test.go'),('closure-last',p/'typed-child/closure-last/source',p/'typed-child/v2/closure-last/generated/assertions_test.go'),('flow',p/'flow-probe/source',p/'flow-probe/assertions_test.go')]:
 out=dest/key;out.mkdir();gen=out/'generated';argv=[str(cand/'goxide'),'transpile','-o',str(gen),str(src)];c=subprocess.run(argv,cwd=src,env=env,capture_output=True);(out/'transpile.stdout.log').write_bytes(c.stdout);(out/'transpile.stderr.log').write_bytes(c.stderr);r={'id':key,'sourceSha256':hashlib.sha256((src/'probe.gox').read_bytes()).hexdigest(),'transpile':{'argv':argv,'cwd':str(src),'exitCode':c.returncode}}
 if c.returncode==0:
  shutil.copy2(assertion,gen/'assertions_test.go');argv=['/opt/homebrew/bin/go','test','-race','-count=1','-v','.'];t=subprocess.run(argv,cwd=gen,env=env,capture_output=True);(out/'test.stdout.log').write_bytes(t.stdout);(out/'test.stderr.log').write_bytes(t.stderr);r['runtime']={'argv':argv,'cwd':str(gen),'exitCode':t.returncode};print(t.stdout.decode(),t.stderr.decode(),flush=True)
 (out/'result.json').write_text(json.dumps(r,indent=2)+'\n');rs.append(r);print(key,c.returncode,c.stderr.decode(),flush=True)
(dest/'results.json').write_text(json.dumps(rs,indent=2)+'\n')
