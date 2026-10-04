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
		"//hgo:cfg feature(\"off\")\npackage p\nfunc F(){ x := ( }\n",
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
	if _, _, e := ConfigureSource("p.hgo", []byte("//hgo:cfg feature(\"off\")\n"+src), map[string]bool{"off": false}); e != nil {
		t.Fatal(e)
	}
}

func TestProjectCfgDisabledHgo(t *testing.T) {
	sources := map[string]string{
		"enum-match":        "package p\nenum State { Empty, Count(Unknown), }\nfunc F(s State)int{return match s {State::Empty=>0,State::Count(n)=>missing(n),}}\n",
		"derive":            "package p\n#[derive(Debug, Clone)]\ntype Item struct { Value Unknown }\n",
		"macro":             "package p\nmacro_rules! m {($x:expr)=>{cfg!(feature($x))};}\nfunc F(){missing_macro!(unresolved)}\n",
		"reserved-disabled": "package p\nmacro_rules! cfg {()=>{false};}\nuse absent::other as cfg;\n",
	}
	for name, src := range sources {
		t.Run(name, func(t *testing.T) {
			input := []byte("//hgo:cfg feature(\"off\")\n" + src)
			out, on, e := ConfigureSource(name+".hgo", input, map[string]bool{"off": false})
			if e != nil || on || !bytes.Equal(input, out) {
				t.Fatalf("disabled hgo: %s %v %v", out, on, e)
			}
		})
	}
}

func TestProjectCfgLexicalBoundaries(t *testing.T) {
	for _, src := range []string{
		"package p\nfunc F(){(]}\n",
		"package p\nvar S=\"unterminated\n",
		"package p\n/* unterminated",
		"package p\nvar X=@\n",
		"package p\nvar X=\x00\n",
		"package p\nvar X=\xff\n",
		"package 123\n",
		"package p\nimport 123\n",
	} {
		if _, _, e := ConfigureSource("bad.hgo", []byte("//hgo:cfg feature(\"off\")\n"+src), map[string]bool{"off": false}); e == nil {
			t.Fatalf("accepted invalid lexical/prefix syntax: %q", src)
		}
	}
	// Deliberately defer full expression grammar to the selected compilation.
	src := []byte("//hgo:cfg feature(\"off\")\npackage p\nfunc F(){x:=}\n")
	if _, on, e := ConfigureSource("deferred.hgo", src, map[string]bool{"off": false}); e != nil || on {
		t.Fatalf("%v %v", on, e)
	}
	// Only real line-comment directives count, never text inside strings/comments.
	src = []byte("package p\n/*\n//hgo:cfg feature(\"unknown\")\n*/\nvar S=`\n//hgo:cfg feature(\"unknown\")\ncfg!(feature(\"unknown\"))\n`\n")
	out, on, e := ConfigureSource("literal.hgo", src, map[string]bool{})
	if e != nil || !on || !bytes.Equal(src, out) {
		t.Fatalf("literal text interpreted: %s %v", out, e)
	}
}
func TestDeriveImportsAfterLeadingComments(t *testing.T) {
	for _, header := range []string{"//hgo:cfg all()\n", "//go:build linux\n\n// package documentation\n", "/* leading block\nsecond line */\n"} {
		for _, clause := range []string{"package p\n", "package p // trailing package comment\n", "package 日本語\n"} {
			src := header + clause + "#[derive(Debug)]\ntype Item struct { Value int }\nfunc Original() int { return 71 }\n"
			r, e := Compile("leading.hgo", []byte(src))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = parser.ParseFile(token.NewFileSet(), "out.go", r.Go, parser.AllErrors); e != nil {
				t.Fatal(e)
			}
			for _, word := range []string{"Original", "Value"} {
				at := strings.Index(string(r.Go), word)
				seg := r.Diagnostics.Lookup(at)
				want := strings.Index(src, word)
				if seg == nil || seg.Origin == nil || seg.Origin.Start != want {
					t.Fatalf("%s wrong origin: %#v expected %d output %s", word, seg, want, r.Go)
				}
			}
		}
	}
}
