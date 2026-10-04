package macros

import (
	"strings"
	"sync"
	"testing"
)

func registry(t *testing.T, path string, sources map[string][]byte, deps map[string]*Package) *Package {
	t.Helper()
	p, err := CollectPackage(path, sources, deps)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestPackageDefinitionIdentityAndOrigins(t *testing.T) {
	dep := registry(t, "example/log", map[string][]byte{
		"definitions.gox": []byte(`package log
macro_rules! inner { ($x:expr) => { Emit($x) }; }
pub macro_rules! log { ($x:expr) => { inner!($x) }; }
`),
		"runtime.gox": []byte("package log\nfunc Emit(x int) int{return x+1}\n"),
	}, nil)
	src := []byte(`package client
import renamed "example/log"
macro_rules! inner { ($x:expr) => { missing($x) }; }
func Run() int { Emit := 999; renamed := 123; _=Emit; _=renamed; return renamed.log!(41) }
`)
	p := registry(t, "example/client", map[string][]byte{"caller.gox": src}, map[string]*Package{"example/log": dep})
	r, err := ExpandPackage("caller.gox", src, nil, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.Source), "= renamed.Emit") || strings.Contains(string(r.Source), "missing(") {
		t.Fatalf("wrong binding:\n%s", r.Source)
	}
	found := false
	for _, o := range r.Origins {
		if len(o.Stack) == 2 {
			found = true
			if o.Stack[0].Definition.File != "definitions.gox" || o.Stack[0].Callsite.File != "caller.gox" || o.Stack[1].Callsite.File != "definitions.gox" {
				t.Fatalf("wrong stack: %+v", o.Stack)
			}
			if !o.Stack[0].DefinitionOriginal || o.Stack[0].CallsiteOriginal {
				t.Fatalf("wrong source stages: %+v", o.Stack)
			}
		}
	}
	if !found {
		t.Fatal("missing nested expansion origins")
	}
}
func TestPackagePrivacyAndNoCallerFallback(t *testing.T) {
	for _, tt := range []struct{ body, call, want string }{
		{`macro_rules! secret {()=>{1};}`, `d.secret!()`, "private"},
		{`pub macro_rules! run {()=>{hidden()};}\nfunc hidden() int{return 1}`, `d.run!()`, "private runtime helper"},
		{`pub macro_rules! run {()=>{caller()};}`, `d.run!()`, "unsupported free template name"},
	} {
		t.Run(tt.want, func(t *testing.T) {
			dep := registry(t, "example/dep", map[string][]byte{"dep.gox": []byte("package dep\n" + strings.ReplaceAll(tt.body, `\n`, "\n"))}, nil)
			src := []byte("package main\nimport d \"example/dep\"\nfunc caller()int{return 99}\nfunc run()int{return " + tt.call + "}\n")
			p := registry(t, "example/main", map[string][]byte{"main.gox": src}, map[string]*Package{"example/dep": dep})
			_, err := ExpandPackage("main.gox", src, nil, p, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want %q", err, tt.want)
			}
		})
	}
}
func TestPackageRegistryConcurrentExpansion(t *testing.T) {
	src := []byte("package p\nmacro_rules! twice {($x:expr)=>{func()int{x:=$x;return x+x}()};}\nfunc Run()int{return twice!(21)}\n")
	p := registry(t, "example/p", map[string][]byte{"p.gox": src}, nil)
	want, err := ExpandPackage("p.gox", src, nil, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := ExpandPackage("p.gox", src, nil, p, nil)
			if err != nil || string(got.Source) != string(want.Source) {
				t.Errorf("non-deterministic expansion: %v", err)
			}
		}()
	}
	wg.Wait()
}
func TestPackageBudget(t *testing.T) {
	var b Budget
	if err := b.Add(Result{ExpansionCount: maxExpansions}); err != nil {
		t.Fatal(err)
	}
	if err := b.Add(Result{ExpansionCount: 1}); err == nil {
		t.Fatal("cross-file expansion budget not enforced")
	}
	b = Budget{}
	if err := b.Add(Result{EmittedTokens: maxTokens}); err != nil {
		t.Fatal(err)
	}
	if err := b.Add(Result{EmittedTokens: 1}); err == nil {
		t.Fatal("cross-file output budget not enforced")
	}
}
func TestPackageDirectImportAndMacroOnlyDependency(t *testing.T) {
	dep := registry(t, "example/wrap", map[string][]byte{"dep.gox": []byte(`package wrap
import "fmt"
pub macro_rules! show {($x:expr)=>{fmt.Sprint($x)};}
`)}, nil)
	src := []byte("package p\nimport w \"example/wrap\"\nfunc Run()string{return w.show!(1)}\n")
	p := registry(t, "example/p", map[string][]byte{"p.gox": src}, map[string]*Package{"example/wrap": dep})
	r, err := ExpandPackage("p.gox", src, nil, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`import _ "example/wrap"`, `"fmt"`, `.Sprint`} {
		if !strings.Contains(string(r.Source), want) {
			t.Fatalf("missing %s:\n%s", want, r.Source)
		}
	}
	dr, err := ExpandPackage("dep.gox", []byte(`package wrap
import "fmt"
pub macro_rules! show {($x:expr)=>{fmt.Sprint($x)};}
`), nil, dep, nil)
	if err != nil || !strings.Contains(string(dr.Source), `import _ "fmt"`) {
		t.Fatalf("definition dependency: %s, %v", dr.Source, err)
	}
}

func TestSingleFileCannotDiscardQualifier(t *testing.T) {
	_, err := Expand("p.gox", []byte("package p\nmacro_rules! value {()=>{1};}\nfunc Run()int{return missing.value!()}\n"))
	if err == nil || !strings.Contains(err.Error(), "directory compilation") {
		t.Fatalf("qualified local macro bypass: %v", err)
	}
}
func TestMacroOnlyImportShadowSelector(t *testing.T) {
	dep := registry(t, "example/d", map[string][]byte{"d.gox": []byte("package d\npub macro_rules! literal {()=>{1};}\n")}, nil)
	src := []byte("package p\nimport d \"example/d\"\nfunc Run()int{d:=struct{V int}{1};return d.V+d.literal!()}\n")
	p := registry(t, "example/p", map[string][]byte{"p.gox": src}, map[string]*Package{"example/d": dep})
	r, err := ExpandPackage("p.gox", src, nil, p, nil)
	if err != nil || !strings.Contains(string(r.Source), `import _ "example/d"`) {
		t.Fatalf("shadow selector mistaken for runtime import use: %v\n%s", err, r.Source)
	}
}
