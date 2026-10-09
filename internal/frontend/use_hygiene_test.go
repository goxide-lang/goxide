package frontend

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestUseGenericImportHygiene(t *testing.T) {
	options := useTestOptions(map[string]string{"api": "example.org/hygiene/api", "facade": "example.org/hygiene/facade", "p": "example.org/hygiene/p", "q": "example.org/hygiene/q"})

	src := t.TempDir()
	useWrite(t, src, map[string]string{
		"go.mod": "module example.org/hygiene\n\ngo 1.27\n",
		"p/p.go": `package p
 type Box[goxideUseImport0 any] struct{Value goxideUseImport0}
 func(b Box[T])Get()T{return b.Value}
 type Later[goxideUseImport1 any] struct{Value goxideUseImport1}
 `,
		"q/q.go":     "package q\ntype Other[goxideUseImport2 any] struct{Value goxideUseImport2}\n",
		"api/pre.go": "package api\nvar goxideUseImport3=3\n",
		"api/api.gox": `package api
 use p;
 use q;
 pub use p::Box;
 pub use p::Later as Another;
 pub use q::Other;
 `,
		"facade/api.gox": `package facade
 use api as a;
 pub use a::*;
 `,
		"client/client.go": `package client
 import a "example.org/hygiene/facade"
 import p "example.org/hygiene/p"
 var X p.Box[int]=a.Box[int]{Value:42}
 var Y=a.Another[int]{Value:7}
 var Z=a.Other[string]{Value:"ok"}
 `,
		"client/client_test.go": `package client
 import "testing"
 func TestRun(t *testing.T){if X.Get()!=42||Y.Value!=7||Z.Value!="ok"{t.Fatal(X,Y,Z)}}
 `,
	})
	out := filepath.Join(t.TempDir(), "out")
	result, e := CompileModuleWithOptions(src, out, options)
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		file, decl, source string
		line               int
	}{
		{"api/goxide_use_aliases.go", "type Box", "api/api.gox", 4},
		{"api/goxide_use_aliases.go", "type Another", "api/api.gox", 5},
		{"facade/goxide_use_aliases.go", "type Box", "facade/api.gox", 3},
	} {
		r := result.Files[tc.file]
		off := strings.Index(string(r.Go), tc.decl)
		if off < 0 || r.Diagnostics == nil {
			t.Fatalf("missing alias/map: %s", tc.file)
		}
		seg := r.Diagnostics.Lookup(off)
		if seg == nil || seg.Origin == nil || seg.Origin.File != filepath.Join(src, tc.source) || seg.Origin.Line != tc.line || seg.Origin.Column != 2 {
			t.Fatalf("wrong use provenance for %s: %#v", tc.decl, seg)
		}
	}
	useGo(t, out, "test", "-race", "./...")
	useGo(t, out, "vet", "./...")
}

func TestUseMacroReservedAliasPosition(t *testing.T) {
	options := useTestOptions(map[string]string{"p": "example.org/position/p"})

	for _, alias := range []string{"print", "println", "go_result", "go_result_keep"} {
		t.Run(alias, func(t *testing.T) {
			src := t.TempDir()
			file := filepath.Join(src, "api.gox")
			useWrite(t, src, map[string]string{
				"go.mod":  "module example.org/position\n\ngo 1.27\n",
				"p/p.gox": "package p\npub macro_rules! answer { () => { 1 }; }\n",
				"api.gox": "package api\nuse p;\n\n  pub use p::answer as " + alias + ";\n",
			})
			_, e := CompileModuleWithOptions(src, filepath.Join(t.TempDir(), "out"), options)
			want := fmt.Sprintf("%s:4:3: macro use alias %s is a reserved builtin macro name", file, alias)
			if e == nil || e.Error() != want {
				t.Fatalf("want exact source position %q, got %v", want, e)
			}
		})
	}
}

func TestUseGenericConstraintPosition(t *testing.T) {
	options := useTestOptions(map[string]string{"p": "example.org/position/p"})

	src := t.TempDir()
	file := filepath.Join(src, "api.gox")
	useWrite(t, src, map[string]string{
		"go.mod":  "module example.org/position\n\ngo 1.27\n",
		"p/p.go":  "package p\ntype Box[T interface{hidden()}] struct{Value T}\n",
		"api.gox": "package api\nuse p;\n\n  pub use p::Box;\n",
	})
	_, e := CompileModuleWithOptions(src, filepath.Join(t.TempDir(), "out"), options)
	want := file + ":4:3: use Box: generic alias constraint has private methods"
	if e == nil || !strings.HasPrefix(e.Error(), want) {
		t.Fatalf("want source position %q, got %v", want, e)
	}
}
