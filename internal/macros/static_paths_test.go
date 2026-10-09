package macros

import (
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestStaticTemplateImportKeepsDefinitionOwner(t *testing.T) {
	dep := registry(t, "example/wrap", map[string][]byte{"def.gox": []byte(`package wrap
import s "strings"
pub macro_rules! repeat {($x:expr)=>{s::Repeat($x, 2)};}
`)}, nil)
	src := []byte(`package caller
import w "example/wrap"
import s "strconv"
func Run() string { s := 1; _ = s; return w::repeat!("x") }
`)
	p := registry(t, "example/caller", map[string][]byte{"call.gox": src}, map[string]*Package{"example/wrap": dep})
	r, err := ExpandPackage("call.gox", src, nil, p, nil, ExpansionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.Source), `"strings"`) || !strings.Contains(string(r.Source), ".Repeat") || strings.Contains(string(r.Source), "s.Repeat") {
		t.Fatalf("definition import rebound to caller:\n%s", r.Source)
	}
	run(t, `package main
import "fmt"
import s "strings"
macro_rules! repeat {($x:expr)=>{s::Repeat($x, 2)};}
func main() { s := 1; _ = s; fmt.Println(repeat!("x")) }
`, "xx\n")
}

func TestStaticMacroPathsRejectDotAndUnresolvedPrefix(t *testing.T) {
	for _, call := range []string{"d.literal!()", "a::d::literal!()"} {
		t.Run(call, func(t *testing.T) {
			dep := registry(t, "example/d", map[string][]byte{"d.gox": []byte("package d\npub macro_rules! literal {()=>{1};}\n")}, nil)
			src := []byte("package p\nimport d \"example/d\"\nmacro_rules! literal {()=>{99};}\nfunc Run()int{return " + call + "}\n")
			p := registry(t, "example/p", map[string][]byte{"p.gox": src}, map[string]*Package{"example/d": dep})
			_, err := ExpandPackage("p.gox", src, nil, p, nil, ExpansionOptions{})
			if err == nil || !strings.Contains(err.Error(), "macro") {
				t.Fatalf("discarded qualifier %s: %v", call, err)
			}
		})
	}
}

func TestStaticTemplatePathsRejectWrongCategory(t *testing.T) {
	for _, tt := range []struct{ body, want string }{
		{`s.Repeat("x",2)`, "package template paths require ::"},
		{`func() int { s:=[]int{1}; return s::Len() }()`, "static template paths require a definition-site package function"},
		{`#[derive(s::Serialize)] type Item struct {}`, "unsupported template syntax"},
	} {
		_, err := Expand("bad.gox", []byte("package p\nimport s \"strings\"\nmacro_rules! m {()=>{"+tt.body+"};}\nfunc Run(){m!()}\n"))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: got %v, want %s", tt.body, err, tt.want)
		}
	}
}

func TestStaticCapturedPathsKeepSpellingAndOrigin(t *testing.T) {
	for _, fragment := range []string{"expr", "tt"} {
		src := []byte("package p\nmacro_rules! identity {($x:" + fragment + ")=>{$x};}\nfunc Run()int{return identity!((s::Value()))}\n")
		r, err := ExpandWithMap("capture.gox", src)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(r.Source), "s::Value()") {
			t.Fatalf("%s capture lost static spelling:\n%s", fragment, r.Source)
		}
		found := false
		for _, origin := range r.Origins {
			if string(r.Source[origin.OutputStart:origin.OutputEnd]) == "Value" && origin.File == "capture.gox" && origin.Line == 3 {
				found = true
			}
		}
		if !found {
			t.Fatalf("%s capture lost source origin", fragment)
		}
	}
}

func TestStaticTemplateDiagnosticKeepsTriviaOffsets(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		src := strings.ReplaceAll("package p\nimport s \"strings\"\nmacro_rules! m {()=>{s /* owner */ :: Missing()};}\nfunc Run(){m!()}\n", "\n", newline)
		_, err := Expand("trivia.gox", []byte(src))
		if err == nil || !strings.Contains(err.Error(), "trivia.gox:3:39:") || !strings.Contains(err.Error(), "must name a function") {
			t.Fatalf("wrong static selector location: %v", err)
		}
	}
}

func TestDeriveHelpersRequireCanonicalResolver(t *testing.T) {
	src := []byte("package p\n#[derive(wire::Serialize, Decode, Clone)]\ntype Item struct{}\n")
	var requests []string
	p, err := CollectPackage("example/p", map[string][]byte{"p.gox": src}, nil, func(file, path string) (string, error) {
		requests = append(requests, file+":"+path)
		switch path {
		case "wire::Serialize":
			return "Serialize", nil
		case "Decode":
			return "Deserialize", nil
		case "Clone":
			return "Clone", nil
		default:
			return "", fmt.Errorf("unbound derive %s", path)
		}
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, helper := range []string{"SerializeItem", "DeserializeItem", "CloneItem"} {
		if !p.funcs[helper] {
			t.Errorf("missing validated helper %s", helper)
		}
	}
	if got := strings.Join(requests, ","); got != "p.gox:wire::Serialize,p.gox:Decode,p.gox:Clone" {
		t.Fatalf("resolver lost complete def-site binding: %s", got)
	}
	for _, spelling := range []string{"Serialize", "serde::Serialize", "wrong::Serialize"} {
		p := registry(t, "example/p", map[string][]byte{"p.gox": []byte("package p\n#[derive(" + spelling + ")]\ntype Item struct{}\n")}, nil)
		if p.funcs["SerializeItem"] {
			t.Errorf("unvalidated spelling authorized helper: %s", spelling)
		}
	}
	_, err = CollectPackage("example/p", map[string][]byte{"wrong.gox": src}, nil, func(file, path string) (string, error) {
		return "", fmt.Errorf("wrong protocol owner: %s", path)
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "wrong.gox:2:10: wrong protocol owner: wire::Serialize") {
		t.Fatalf("wrong owner must fail at definition: %v", err)
	}
	var located *sourcemap.Error
	if !errors.As(err, &located) || located.Origin == nil || located.Origin.File != "wrong.gox" || located.Origin.Line != 2 || located.Origin.Column != 10 {
		t.Fatalf("derive binding failure lost structured source origin: %v", err)
	}
}

func TestValidatedDeriveHelperKeepsDefinitionOwner(t *testing.T) {
	definitions := []byte(`package protocol
#[derive(wire::Serialize)]
type Item struct{}
pub macro_rules! encode {($x:expr)=>{SerializeItem($x)};}
`)
	dep, err := CollectPackage("example/protocol", map[string][]byte{"def.gox": definitions}, nil, func(file, path string) (string, error) {
		if file != "def.gox" || path != "wire::Serialize" {
			return "", fmt.Errorf("unexpected definition binding %s:%s", file, path)
		}
		return "Serialize", nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	src := []byte(`package caller
import protocol "example/protocol"
func SerializeItem(x int) int { return 99 }
func Run() int { return protocol::encode!(1) }
`)
	p := registry(t, "example/caller", map[string][]byte{"call.gox": src}, map[string]*Package{"example/protocol": dep})
	r, err := ExpandPackage("call.gox", src, nil, p, nil, ExpansionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(r.Source), "= protocol.SerializeItem") {
		t.Fatalf("derived helper rebound to caller:\n%s", r.Source)
	}
}

func TestPrivateMacroUseIsFileScopedAndKeepsDefinitionScope(t *testing.T) {
	dep := registry(t, "example/dep", map[string][]byte{"dep.gox": []byte("package dep\nmacro_rules! inner {()=>{7};}\npub macro_rules! original {()=>{inner!()};}\n")}, nil)
	files := map[string][]byte{
		"one.gox": []byte("package caller\nmacro_rules! inner {()=>{99};}\nfunc One()int{return selected!()}\n"),
		"two.gox": []byte("package caller\nfunc Two()int{return selected!()}\n"),
	}
	p := registry(t, "example/caller", files, map[string]*Package{"example/dep": dep})
	if err := p.BindAlias("one.gox", "selected", dep, "original", false); err != nil {
		t.Fatal(err)
	}
	r, err := ExpandPackage("one.gox", files["one.gox"], nil, p, nil, ExpansionOptions{})
	if err != nil || !strings.Contains(string(r.Source), "return 7") {
		t.Fatalf("definition binding changed: %v\n%s", err, r.Source)
	}
	if _, err := ExpandPackage("two.gox", files["two.gox"], nil, p, nil, ExpansionOptions{}); err == nil || !strings.Contains(err.Error(), "unknown macro selected") {
		t.Fatalf("private use leaked across files: %v", err)
	}
	if _, _, ok := p.PublicBinding("selected"); ok {
		t.Fatal("private macro use exported")
	}
}

func TestFunctionUseIsFileScopedAndKeepsDefinitionOwner(t *testing.T) {
	files := map[string][]byte{
		"one.gox": []byte("package caller\nmacro_rules! repeat {()=>{selected(\"x\",2)};}\nfunc One()string{return repeat!()}\n"),
		"two.gox": []byte("package caller\nmacro_rules! number {()=>{selected(42)};}\nfunc Two()string{return number!()}\n"),
	}
	p := registry(t, "example/caller", files, nil)
	for _, binding := range []struct{ file, path, name string }{{"one.gox", "strings", "Repeat"}, {"two.gox", "strconv", "Itoa"}} {
		if err := p.BindFunctionAlias(binding.file, "selected", binding.path, binding.name); err != nil {
			t.Fatal(err)
		}
		r, err := ExpandPackage(binding.file, files[binding.file], nil, p, nil, ExpansionOptions{})
		if err != nil || !strings.Contains(string(r.Source), `"`+binding.path+`"`) || !strings.Contains(string(r.Source), "."+binding.name) {
			t.Fatalf("definition function use rebound: %v\n%s", err, r.Source)
		}
	}
}

func TestLexicalUseCannotMasqueradeAsPrimitive(t *testing.T) {
	src := []byte("package p\nmacro_rules! make_zero {()=>{int(0)};}\nfunc Run(){_=make_zero!()}\n")
	p := registry(t, "example/p", map[string][]byte{"p.gox": src}, nil)
	if err := p.BindUseName("p.gox", "int"); err != nil {
		t.Fatal(err)
	}
	if _, err := ExpandPackage("p.gox", src, nil, p, nil, ExpansionOptions{}); err == nil || !strings.Contains(err.Error(), "definition-site declaration") {
		t.Fatalf("lexical binding mistaken for predeclared int: %v", err)
	}
}

func TestPreparedPackageDefinitionUsesOriginalSpans(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		original := strings.ReplaceAll("package dep\nuse strings;\npub macro_rules! value {()=>{7};}\n", "\n", newline)
		insert := len("package dep")
		imports := newline + "import \"strings\"" + newline
		clean := strings.Replace(original, "use strings;", strings.Repeat(" ", len("use strings;")), 1)
		prepared := []byte(clean[:insert] + imports + clean[insert:])
		originalMap := sourcemap.Identity("def.gox", []byte(original))
		mapping := &sourcemap.Map{Source: prepared}
		mapping.Segments = append(mapping.Segments, originalMap.Copy(0, insert, 0)...)
		afterUse := strings.Index(original, "use strings;") + len("use strings;")
		mapping.Segments = append(mapping.Segments, originalMap.Copy(afterUse, len(original), afterUse+len(imports))...)
		dep, err := CollectPackage("example/dep", map[string][]byte{"def.gox": prepared}, nil, nil, map[string]*sourcemap.Map{"def.gox": mapping})
		if err != nil {
			t.Fatal(err)
		}
		caller := []byte("package p\nimport d \"example/dep\"\nfunc Run()int{return d::value!()}\n")
		pkg := registry(t, "example/p", map[string][]byte{"caller.gox": caller}, map[string]*Package{"example/dep": dep})
		r, err := ExpandPackage("caller.gox", caller, nil, pkg, nil, ExpansionOptions{})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, origin := range r.Origins {
			if string(r.Source[origin.OutputStart:origin.OutputEnd]) != "7" {
				continue
			}
			found = true
			if !origin.Original || origin.SourceStart != strings.Index(original, "7") || origin.Line != 3 || len(origin.Stack) != 1 {
				t.Fatalf("prepared offset claimed as original: %+v", origin)
			}
			frame := origin.Stack[0]
			if !frame.DefinitionOriginal || frame.Definition.Start != strings.Index(original, "macro_rules!") || frame.Definition.Line != 3 {
				t.Fatalf("definition frame moved with generated imports: %+v", frame)
			}
		}
		if !found {
			t.Fatal("missing expanded definition token")
		}
	}
}

func TestMacroImportInsertionAfterExplicitPackageSemicolon(t *testing.T) {
	dep := registry(t, "example/wrap", map[string][]byte{"wrap.gox": []byte("package wrap\nimport s \"strings\"\npub macro_rules! repeat {()=>{s::Repeat(\"x\",2)};}\n")}, nil)
	source := []byte("package client; import wrap \"example/wrap\"\nfunc Run()string{return wrap::repeat!()}\n")
	p := registry(t, "example/client", map[string][]byte{"client.gox": source}, map[string]*Package{"example/wrap": dep})
	r, err := ExpandPackage("client.gox", source, nil, p, nil, ExpansionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "client.go", r.Source, 0); err != nil {
		t.Fatalf("generated import split package semicolon: %v\n%s", err, r.Source)
	}
}

func TestExpandPackagePreservesImportsForLaterDSLPass(t *testing.T) {
	source := []byte("package p\nimport route \"strings\"\n")
	pkg := registry(t, "example/p", map[string][]byte{"p.gox": source}, nil)
	if err := pkg.BindFunctionAlias("p.gox", "Trim", "strings", "TrimSpace"); err != nil {
		t.Fatal(err)
	}
	for _, preserve := range []bool{false, true} {
		r, err := ExpandPackage("p.gox", source, nil, pkg, nil, ExpansionOptions{PreserveImports: preserve})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(string(r.Source), "import route"); got != preserve {
			t.Fatalf("preserve=%v: %s", preserve, r.Source)
		}
	}
}
