package frontend

import (
	"github.com/hxsf/goxide/internal/testdeps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func compileImportedFixture(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	src := t.TempDir()
	files["go.mod"] = "module example.org/qualified\ngo 1.27\n"
	for name, source := range files {
		path := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := testdeps.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	out := filepath.Join(t.TempDir(), "generated")
	_, err := CompileModule(src, out)
	return out, err
}

func TestQualifiedEnumsKeepFileAliasesAndValidation(t *testing.T) {
	out, err := compileImportedFixture(t, map[string]string{
		"a/a.hgo": "package unusual\npub enum State { Idle, Number(int), }\n",
		"b/b.hgo": "package unusual\npub enum State { Idle, Number(int), }\n",
		"first.hgo": `package app
import first "example.org/qualified/a"
import same "example.org/qualified/a"
var Effects int
func One(v first.State) int {
 first := 99; _ = first
 match v {
 first.State::Number(n) if n > 0 => { return n },
 _ => { Effects++; return -1 },
 }
 return 0
}
func Make() first.State { return first.State::Number(7) }
func Same(v first.State) int {
 match v { first.State::Idle => { return 0 }, same.State::Number(n) => { return n } }
 return -1
}
`,
		"second.hgo": `package app
import first "example.org/qualified/b"
import other "example.org/qualified/a"
func Two(v first.State) int {
 match v { first.State::Idle => { return 2 }, first.State::Number(n) => { return n } }
 return 0
}
func Again(v other.State) int { return One(v) }
`,
		"consumer_test.go": `package app
import("testing"; a "example.org/qualified/a"; b "example.org/qualified/b")
func TestValues(t *testing.T) {
 if Same(Make()) != 7 || One(Make()) != 7 || Again(Make()) != 7 || Two(b.NewStateIdle()) != 2 { t.Fatal("wrong identity") }
 type forged struct { a.HgoCase5_State_Number }
 var bad *a.HgoCase5_State_Number
 for _, v := range []a.State{nil,bad,&a.HgoCase5_State_Number{Value:1},forged{}} {
  func(){defer func(){if recover()==nil {t.Error("invalid enum accepted")}}(); One(v)}()
  if Effects != 0 { t.Fatal("wildcard ran before validation") }
 }
}
`,
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", "-race", "./...")
	cmd.Dir = out
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated module: %v\n%s", err, data)
	}
}

func TestQualifiedEnumsRejectPrivateAndMixedIdentity(t *testing.T) {
	for _, tc := range []struct{ name, consumer, want string }{
		{"private", `func f(){match 0 { p.secret::Idle => {} }}`, "unknown enum"},
		{"same-spelling", `func f(v p.State){match v { p.State::Idle => {}, q.State::Number(n) => { _=n } }}`, "mixed enum identities"},
		{"native", `func f(v p.State){match v { p.State::Idle => {} }}`, "unknown enum"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			producer := "package p\npub enum State { Idle, Number(int), }\nenum secret { Idle, }\n"
			extension := ".hgo"
			if tc.name == "native" {
				producer = "package p\ntype State int\n"
				extension = ".go"
			}
			imports := `import p "example.org/qualified/a"` + "\n"
			if tc.name == "same-spelling" {
				imports += `import q "example.org/qualified/b"` + "\n"
			}
			_, err := compileImportedFixture(t, map[string]string{"a/a" + extension: producer, "b/b.hgo": "package p\npub enum State { Idle, Number(int), }\n", "main.hgo": "package main\n" + imports + tc.consumer + "\n"})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "main.hgo:") {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestUserHgoPrefixDoesNotDeduplicate(t *testing.T) {
	_, err := CompilePackage("example.org/p", map[string][]byte{"a.hgo": []byte("package p\nfunc HgoBusiness() {}\n"), "b.hgo": []byte("package p\nfunc HgoBusiness() {}\n")}, nil)
	if err == nil || !strings.Contains(err.Error(), "redeclared") {
		t.Fatalf("duplicate user declaration: %v", err)
	}
}

func TestQualifiedMatchBodyDiagnosticOrigin(t *testing.T) {
	_, err := compileImportedFixture(t, map[string]string{
		"a/a.hgo": "package p\npub enum State { Idle, Number(int), }\n",
		"main.hgo": `package app
import p "example.org/qualified/a"
func f(v p.State) {
 match v {
  p.State::Idle => {},
  p.State::Number(n) => { var bad string = n; _ = bad },
 }
}
`,
	})
	if err == nil || !strings.Contains(err.Error(), "main.hgo:6:") || !strings.Contains(err.Error(), "cannot use") {
		t.Fatalf("missing consumer arm origin: %v", err)
	}
}
