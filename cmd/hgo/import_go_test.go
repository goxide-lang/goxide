package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func importCall(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	var b bytes.Buffer
	argv := append([]string{"import-go", "--go", filepath.Join(runtime.GOROOT(), "bin", "go")}, args...)
	argv = append(argv, root)
	e := run(argv, &b, &b)
	return b.String(), e
}

func TestImportGoPlanPublishMixedAndLocked(t *testing.T) {
	t.Setenv("GOWORK", "off")
	base := t.TempDir()
	old := filepath.Join(base, "old")
	dep := filepath.Join(base, "dep")
	out := filepath.Join(base, "new")
	projectWrite(t, dep, map[string]string{"go.mod": "module example.org/dep\n\ngo 1.27\n", "dep.go": "package dep\nfunc Value()string{return \"dep\"}\n"})
	projectWrite(t, old, map[string]string{"go.mod": "module example.org/app\n\ngo 1.27\ntoolchain go1.27.1\nrequire example.org/dep v1.0.0 // indirect\nreplace example.org/dep => ../dep\nexclude example.org/dep v1.0.1\n", "main.go": "package main\nimport (\"fmt\";_ \"embed\";\"example.org/dep\")\n//go:embed data.txt\nvar data string\nfunc main(){fmt.Println(dep.Value(),data,Helper())}\n", "helper.go": "package main\nfunc Helper()string{return \"mixed\"}\n", "data.txt": "embedded"})
	before := sourceHash(t, old)
	b, e := importCall(t, old, "--plan", "--bin", "app=.")
	if e != nil || !strings.Contains(b, "format = 2") || !strings.Contains(b, "indirect = true") {
		t.Fatalf("%s %v", b, e)
	}
	if sourceHash(t, old) != before {
		t.Fatal("plan changed source")
	}
	if b, e = importCall(t, old, "--out", out, "--bin", "app=.", "--offline"); e != nil {
		t.Fatalf("%s %v", b, e)
	}
	if sourceHash(t, old) != before {
		t.Fatal("import changed source")
	}
	for _, name := range []string{"go.mod", "go.sum", "target", ".hgo"} {
		if _, e := os.Lstat(filepath.Join(out, name)); !os.IsNotExist(e) {
			t.Fatal("published staging artifact", name, e)
		}
	}
	lock, e := os.ReadFile(filepath.Join(out, "goxide.lock"))
	if e != nil || bytes.Contains(lock, []byte(".goxide-import-")) {
		t.Fatalf("lock %s %v", lock, e)
	}
	for i := 0; i < 2; i++ {
		if b, e = projectRun(t, out, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "dep embedded mixed" {
			t.Fatalf("%s %v", b, e)
		}
		if i == 0 {
			if e = os.Rename(filepath.Join(out, "helper.go"), filepath.Join(out, "helper.hgo")); e != nil {
				t.Fatal(e)
			}
		}
	}
	if b, e = importCall(t, old, "--out", out, "--bin", "app=."); e == nil {
		t.Fatal("overwrote existing project", b)
	}
}

func TestImportGoFailuresLeaveOriginalAndOutput(t *testing.T) {
	for _, kind := range []string{"unknown", "deprecated", "retract", "godebug", "tool", "workspace", "parent-module", "bad-marker", "compile", "existing-config"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("GOWORK", "off")
			base := t.TempDir()
			root := filepath.Join(base, "root")
			out := filepath.Join(base, "new")
			mod := "module example.org/app\n\ngo 1.27\n"
			switch kind {
			case "unknown":
				mod += "future_directive 1\n"
			case "deprecated":
				mod = "// Deprecated: use example.org/new instead.\n" + mod
			case "retract":
				mod += "retract v1.0.0\n"
			case "godebug":
				mod += "godebug default=go1.26\n"
			case "tool":
				mod += "tool example.org/tool\n"
			}
			projectWrite(t, root, map[string]string{"go.mod": mod, "main.go": "package main\nfunc main(){}\n"})
			switch kind {
			case "workspace":
				projectWrite(t, base, map[string]string{"go.work": "go 1.27\nuse ./root\n"})
			case "parent-module":
				os.Rename(filepath.Join(root, "go.mod"), filepath.Join(base, "go.mod"))
			case "bad-marker":
				projectWrite(t, root, map[string]string{"keep/.hgo-layout.json": "garbage", "keep/notes.txt": "preserve"})
			case "compile":
				projectWrite(t, root, map[string]string{"main.go": "package main\nfunc main(){missing()}\n"})
			case "existing-config":
				os.WriteFile(filepath.Join(root, "goxide.toml"), []byte("unknown"), 0644)
			}
			before := sourceHash(t, root)
			if b, e := importCall(t, root, "--out", out, "--bin", "app=.", "--offline"); e == nil {
				t.Fatalf("accepted %s %s", kind, b)
			}
			if sourceHash(t, root) != before {
				t.Fatal("source changed")
			}
			if _, e := os.Lstat(out); !os.IsNotExist(e) {
				t.Fatal("partial publication", e)
			}
			stages, _ := filepath.Glob(filepath.Join(base, ".goxide-import-*"))
			if len(stages) != 0 {
				t.Fatal("staging leak", stages)
			}
		})
	}
}

func TestUnifiedLocalReplaceAndPureGit(t *testing.T) {
	for _, kind := range []string{"replace", "git", "path"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			lib := filepath.Join(base, "lib")
			root := filepath.Join(base, "app")
			projectWrite(t, lib, map[string]string{"goxide.toml": "[package]\nformat=2\nmodule='example.org/lib'\ngo='1.27'\n[features]\ndefault=['on']\non=[]\n[lib]\nname='lib'\npath='.'\n", "lib.hgo": "package lib\nfunc Value()bool{return cfg!(feature(\"on\"))}\n"})
			dependency := "path='../lib'\n"
			extra := ""
			if kind == "replace" {
				dependency = "version='v1.0.0'\n"
				extra = "[[go.replace]]\nmodule='example.org/lib'\npath='../lib'\n"
			}
			if kind == "git" {
				migrationGit(t, lib, "init")
				migrationGit(t, lib, "add", ".")
				migrationGit(t, lib, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-m", "pure")
				dependency = "git='" + lib + "'\nrev='HEAD'\n"
			}
			projectWrite(t, root, map[string]string{"goxide.toml": "[package]\nformat=2\nmodule='example.org/app'\ngo='1.27'\n[[bin]]\nname='app'\npath='main.go'\n[dependencies.lib]\nmodule='example.org/lib'\n" + dependency + extra, "main.go": "package main\nimport (\"fmt\";\"example.org/lib\")\nfunc main(){fmt.Println(lib.Value())}\n"})
			before := sourceHash(t, lib)
			flags := []string{"run"}
			if kind != "git" {
				flags = append(flags, "--offline")
			}
			if b, e := projectRun(t, root, flags...); e != nil || strings.TrimSpace(b) != "true" {
				t.Fatalf("%s %v", b, e)
			}
			if b, e := projectRun(t, root, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "true" {
				t.Fatalf("%s %v", b, e)
			}
			if sourceHash(t, lib) != before {
				t.Fatal("dependency source modified")
			}
		})
	}
}

func TestImportGoChecksumsAndOffline(t *testing.T) {
	proxy, _ := versionProxy(t)
	publishVersion(t, proxy, "example.org/native", "v1.0.0", map[string]string{"go.mod": "module example.org/native\ngo 1.27\n", "native.go": "package native\nfunc Value()string{return \"native\"}\n"})
	base := t.TempDir()
	root := filepath.Join(base, "old")
	out := filepath.Join(base, "new")
	projectWrite(t, root, map[string]string{"go.mod": "module example.org/app\ngo 1.27\nrequire example.org/native v1.0.0\n", "main.go": "package main\nimport (\"fmt\";\"example.org/native\")\nfunc main(){fmt.Println(native.Value())}\n"})
	before := sourceHash(t, root)
	if b, e := importCall(t, root, "--out", out, "--bin", "app=.", "--offline"); e == nil {
		t.Fatal("cold offline accepted", b)
	}
	if sourceHash(t, root) != before {
		t.Fatal("cold failure mutated source")
	}
	c := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "mod", "download", "all")
	c.Dir = root
	if b, e := c.CombinedOutput(); e != nil {
		t.Fatalf("warm %s %v", b, e)
	}
	sums, e := os.ReadFile(filepath.Join(root, "go.sum"))
	if e != nil {
		t.Fatal(e)
	}
	before = sourceHash(t, root)
	if b, e := importCall(t, root, "--out", out, "--bin", "app=.", "--offline"); e != nil {
		t.Fatalf("warm offline %s %v", b, e)
	}
	if sourceHash(t, root) != before {
		t.Fatal("warm import mutated source")
	}
	if b, e := projectRun(t, out, "run", "--offline", "--locked"); e != nil || strings.TrimSpace(b) != "native" {
		t.Fatalf("locked %s %v", b, e)
	}
	// A known checksum is authoritative input, never overwritten by a new lock.
	lines := strings.Split(string(sums), "\n")
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) == 3 {
			f[2] = "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
			lines[i] = strings.Join(f, " ")
		}
	}
	if e := os.WriteFile(filepath.Join(root, "go.sum"), []byte(strings.Join(lines, "\n")), 0644); e != nil {
		t.Fatal(e)
	}
	before = sourceHash(t, root)
	if b, e := importCall(t, root, "--out", filepath.Join(base, "bad"), "--bin", "app=.", "--offline"); e == nil {
		t.Fatal("accepted checksum conflict", b)
	}
	if sourceHash(t, root) != before {
		t.Fatal("checksum failure mutated source")
	}
}
