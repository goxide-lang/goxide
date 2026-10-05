package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestImportSourceTerminalProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX PTY test")
	}
	python, e := exec.LookPath("python3")
	if e != nil {
		t.Skip("python3 PTY driver unavailable")
	}
	repo, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	binary := filepath.Join(t.TempDir(), "goxide")
	c := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "build", "-mod=readonly", "-o", binary, "./cmd/goxide")
	c.Dir = repo
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, b)
	}
	root, _, _ := sourceFixture(t, false, true)
	var script = `import os,pty,select,subprocess,sys,time
master,slave=pty.openpty()
p=subprocess.Popen(sys.argv[3:],stdin=slave,stdout=slave,stderr=slave)
os.close(slave)
data=b''; sent=False; deadline=time.monotonic()+90
try:
 while time.monotonic()<deadline:
  if select.select([master],[],[],.1)[0]:
   try: chunk=os.read(master,65536)
   except OSError: break
   if not chunk: break
   data+=chunk
   if not sent and b'cancel aborts:' in data:
    os.write(master,sys.argv[1].encode());sent=True
  if p.poll() is not None: break
 else: raise RuntimeError('PTY process timed out')
 code=p.wait(timeout=5)
 print(data.decode(errors='replace'))
 assert code==int(sys.argv[2]),(code,data)
 assert sent or b'requested source' in data or b'Plan:' in data, data
finally:
 if p.poll() is None: p.kill();p.wait()
 os.close(master)
`
	for _, tc := range []struct {
		name, input   string
		success, plan bool
	}{
		{name: "default local", input: "\n", success: true},
		{name: "cancel", input: "cancel\n"},
		{name: "EOF", input: "\x04"},
		{name: "remote reject", input: "example.org/d=remote\nno\n"},
		{name: "remote confirm", input: "example.org/d=remote\nremote example.org/d\n", success: true},
		{name: "plan never prompts", plan: true, success: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "new")
			code := "1"
			if tc.success {
				code = "0"
			}
			args := []string{"-c", script, tc.input, code, binary, "import-go", "--bin", "app=.", "--offline"}
			if tc.plan {
				args = append(args, "--plan")
			} else {
				args = append(args, "--out", out)
			}
			args = append(args, root)
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
			defer cancel()
			c := exec.CommandContext(ctx, python, args...)
			if b, e := c.CombinedOutput(); e != nil {
				t.Fatalf("%v %s", e, b)
			}
			_, e := os.Stat(out)
			if tc.success && !tc.plan {
				if e != nil {
					t.Fatal(e)
				}
			} else if !os.IsNotExist(e) {
				t.Fatal("published cancelled/plan output")
			}
		})
	}
	t.Run("pipe deterministic and missing value", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "new")
		c := exec.Command(binary, "import-go", "--out", out, "--bin", "app=.", "--offline", root)
		c.Stdin = strings.NewReader("example.org/d=remote\nremote example.org/d\n")
		b, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("%v %s", e, b)
		}
		if sourceAudit(t, out).Sources["example.org/d"].Source != "local" {
			t.Fatal("pipe triggered interactive selection")
		}
		c = exec.Command(binary, "import-go", "--source")
		b, e = c.CombinedOutput()
		if e == nil || !bytes.Contains(b, []byte("needs an argument")) {
			t.Fatalf("missing value %v %s", e, b)
		}
		c = exec.Command(binary, "import-go", "-source", "example.org/d=remote", root)
		b, e = c.CombinedOutput()
		if e == nil || !bytes.Contains(b, []byte("use --source")) {
			t.Fatalf("long flag %v %s", e, b)
		}
	})
}
