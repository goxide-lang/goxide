package frontend

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestProjectCfg(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		source := []byte("//hgo:cfg feature(\"extra\")\npackage p\nimport \"missing.example/never\"\nfunc F()Unknown{return missing()}\n")
		out, selected, e := ConfigureSource("p.hgo", source, map[string]bool{"extra": enabled})
		if e != nil || selected != enabled || !bytes.Equal(out, source) {
			t.Fatalf("selection: %s %v %v", out, selected, e)
		}
	}
	for _, pred := range []string{"feature(\"extra\")", "any()", "all()", "not(feature(\"extra\"))"} {
		src := []byte("package p\nvar V = cfg!(" + pred + ") && true\n")
		out, _, e := ConfigureSource("p.hgo", src, map[string]bool{"extra": true})
		if e != nil {
			t.Fatal(e)
		}
		for i, c := range src {
			if (c == '\n') != (out[i] == '\n') {
				t.Fatal("newline mapping changed")
			}
		}
		if len(out) != len(src) {
			t.Fatal("offset changed")
		}
		if _, e = parser.ParseFile(token.NewFileSet(), "p.go", out, 0); e != nil {
			t.Fatalf("%s: %v", out, e)
		}
	}
	for _, src := range []string{
		"//hgo:cfg feature(\"off\")\npackage p\nfunc F(){ x := }\n",
		"//hgo:cfg feature(\"missing\")\npackage p\n",
		"package p\nvar v=cfg!(not())\n",
		"package p\nvar v=cfg!(\nall()\n)\n",
		"package p\nmacro_rules! cfg {() => {true};}\n",
		"package p\nuse p::other as cfg;\n",
		"package p\npub use p::cfg;\n",
	} {
		if _, _, e := ConfigureSource("p.hgo", []byte(src), map[string]bool{"off": false}); e == nil {
			t.Fatalf("accepted %s", src)
		}
	}
	src := "package p\npub macro_rules! m {($x:expr) => { $x };}\n"
	if _, _, e := ConfigureSource("p.hgo", []byte(src), map[string]bool{}); e != nil {
		t.Fatal(e)
	}
	if _, _, e := ConfigureSource("p.hgo", []byte("//hgo:cfg feature(\"off\")\n"+src), map[string]bool{"off": false}); e == nil || !strings.Contains(e.Error(), "Go-compatible syntax") {
		t.Fatal(e)
	}
}
