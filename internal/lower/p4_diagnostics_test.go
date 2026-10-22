package lower_test

import (
	"errors"
	"go/importer"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestP4RejectsInvalidViewsAtSourceOperation(t *testing.T) {
	const declarations = `
type Data struct { N int; Items []int }
trait View { func value(self Self) int }
impl View for Data { func value(self Self) int { return self.N } }
trait Mut { func add(self &mut Self) }
impl Mut for Data { func add(self &mut Self) { self.N++ } }
`
	for _, test := range []struct{ name, source, code, message string }{
		{"ambiguous-method", `trait Other {func value(self Self)int};impl Other for Data {func value(self Self)int{return 2}};func f(value Data)int{return /*bad*/value.value/*end*/()}`, check.TraitInvalid, "ambiguous trait method"},
		{"ambiguous-static-method", `trait First{func make()Self};trait Second{func make()Self};impl First for Data{func make()Self{return Self{N:1}}};impl Second for Data{func make()Self{return Self{N:2}}};func f()Data{return /*bad*/Data::make/*end*/()}`, check.TraitInvalid, "ambiguous static member"},
		{"readonly-mutable-view", `func f(value &Data) {var view Mut = /*bad*/value/*end*/; _=view}`, check.ReferencePermission, ""},
		{"temporary-mutable-view", `func makeData()Data{return Data{}};func f(){var view Mut=/*bad*/makeData()/*end*/;_=view}`, check.ReferenceAddress, ""},
		{"map-mutable-view", `func f(values map[string]Data){var view Mut=/*bad*/values["item"]/*end*/;_=view}`, check.ReferenceAddress, ""},
		{"readonly-mixed-view", `trait Mixed{func write(self Self);func read(self &Self)int};impl Mixed for Data{func write(self Self){self.Items[0]=1};func read(self &Self)int{return self.N}};func f(value &Data){var view Mixed=/*bad*/value/*end*/;_=view}`, check.ReferencePermission, ""},
		{"readonly-receiver-body", `trait Read{func bad(self &Self)};impl Read for Data{func bad(self &Self){/*bad*/self.N/*end*/++}}`, check.ReferencePermission, ""},
		{"parameter-permission-mismatch", `trait Consume{func consume(self Self,value &Data)};impl Consume for Data{/*bad*/func consume(self Self,value &mut Data){value.N++}/*end*/}`, check.TraitInvalid, "permissions do not match"},
		{"view-cannot-select-another-trait", `trait Other{func value(self Self)int};impl Other for Data{func value(self Self)int{return 2}};func f(value View){var other Other=/*bad*/value/*end*/;_=other}`, check.TraitInvalid, "different trait"},
		{"new-defined-type-does-not-inherit", `type Twin Data;func f(value Twin){var view View=/*bad*/value/*end*/;_=view}`, check.TraitInvalid, "explicit implementation"},
		{"nonruntime-self-result", `trait Transform{func make()Self;func transform(self &Self)Self};impl Transform for Data{func make()Self{return Self{}};func transform(self &Self)Self{return Self{N:self.N}}};func f(){var view /*bad*/Transform/*end*/=Data{};_=view}`, check.TraitInvalid, "cannot form a runtime view"},
		{"nonruntime-nested-self-result", `trait Wrapped{func wrap(self Self)struct{Value Self}};func f(){var view /*bad*/Wrapped/*end*/;_=view}`, check.TraitInvalid, "cannot form a runtime view"},
		{"unbound-runtime-associated-type", `trait Source{type Item;func get(self Self)Self::Item};func f(){var view /*bad*/Source/*end*/;_=view}`, check.TraitInvalid, "requires associated type"},
		{"wrong-runtime-associated-type", `trait Source{type Item;func get(self Self)Self::Item};impl Source for Data{type Item=int;func get(self Self)Self::Item{return self.N}};func f(value Data){var view Source[Item=string]=/*bad*/value/*end*/;_=view}`, check.TraitInvalid, "associated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := "package model\n" + declarations + test.source + "\n"
			files, info, diagnostics := checkP4DiagnosticSource(t, source)
			assertP4SourceDiagnostic(t, source, diagnostics, test.code, test.message)
			if output, err := lower.Lower(files, info); output != nil || err == nil {
				t.Fatalf("rejected source emitted Go: output=%v error=%v", output, err)
			}
		})
	}
}

func TestP4GeneratedFactoryCollisionIdentifiesImpl(t *testing.T) {
	const source = `package model
trait View {func value(self Self)int}
type Data struct {N int}
func AsDataView(value Data) int {return value.N}
/*bad*/impl View for Data {func value(self Self)int{return self.N}}/*end*/
`
	files, info, diagnostics := checkP4DiagnosticSource(t, source)
	if len(diagnostics) != 0 {
		t.Fatalf("check: %v", diagnostics)
	}
	output, err := lower.Lower(files, info)
	var diagnostic *lower.Diagnostic
	if output != nil || !errors.As(err, &diagnostic) || !strings.Contains(diagnostic.Message, "AsDataView") {
		t.Fatalf("missing generated-name source diagnostic: output=%v error=%v", output, err)
	}
	start, end := markedP4Range(source)
	if diagnostic.File != p4DiagnosticFilename || diagnostic.Span.Start < start || diagnostic.Span.Start >= diagnostic.Span.End || diagnostic.Span.End > end || diagnostic.Origin.File != diagnostic.File || diagnostic.Origin.Span != diagnostic.Span {
		t.Fatalf("factory collision points outside impl [%d:%d]: %#v", start, end, diagnostic)
	}
}

func TestP4GeneratedEnumViewMethodRejectsPayloadFieldCollision(t *testing.T) {
	const source = `package model
pub trait View {func value(self Self)int}
pub enum Data { Item { AsView int } }
/*bad*/impl View for Data {func value(self Self)int{return 1}}/*end*/
`
	files, info, diagnostics := checkP4DiagnosticSource(t, source)
	if len(diagnostics) != 0 {
		t.Fatalf("check: %v", diagnostics)
	}
	output, err := lower.Lower(files, info)
	var diagnostic *lower.Diagnostic
	if output != nil || !errors.As(err, &diagnostic) || !strings.Contains(diagnostic.Message, "AsView") {
		t.Fatalf("missing enum view/payload collision: output=%v error=%v", output, err)
	}
	start, end := markedP4Range(source)
	if diagnostic.File != p4DiagnosticFilename || diagnostic.Span.Start < start || diagnostic.Span.Start >= diagnostic.Span.End || diagnostic.Span.End > end || diagnostic.Origin.File != diagnostic.File || diagnostic.Origin.Span != diagnostic.Span {
		t.Fatalf("enum view/payload collision points outside impl [%d:%d]: %#v", start, end, diagnostic)
	}
}

func TestP4PrivateInherentMemberRejectsAtConsumerSource(t *testing.T) {
	for _, expression := range []string{"value.hidden()", "model::Data::secret()"} {
		t.Run(expression, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.Mkdir(filepath.Join(directory, "model"), 0700); err != nil {
				t.Fatal(err)
			}
			source := "package main\nuse traitprivate::model;\nfunc main(){value:=model::Data{};_=value;_=/*bad*/" + expression + "/*end*/}\n"
			for name, content := range map[string]string{
				"main.gox": source,
				"model/model.gox": `package model
type Data struct{}
impl Data {
    func hidden(self Self)int{return 1}
    func secret()int{return 2}
}
`,
			} {
				if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			output, err := lower.PrepareModule([]string{directory}, "traitprivate", testConfig(t))
			if output != "" {
				t.Cleanup(func() { os.RemoveAll(output) })
				t.Fatalf("private member access emitted module %s", output)
			}
			var diagnostic check.Diagnostic
			if !errors.As(err, &diagnostic) || diagnostic.Code != check.TraitInvalid {
				t.Fatalf("missing source visibility diagnostic: %v", err)
			}
			start, end := markedP4Range(source)
			if diagnostic.File != "traitprivate/main.gox" || diagnostic.Span.Start < start || diagnostic.Span.Start >= diagnostic.Span.End || diagnostic.Span.End > end || diagnostic.Origin.File != diagnostic.File || diagnostic.Origin.Span != diagnostic.Span {
				t.Fatalf("visibility diagnostic points outside consumer expression [%d:%d]: %#v", start, end, diagnostic)
			}
		})
	}
}

const p4DiagnosticFilename = "traitdiagnostic/model.gox"

func checkP4DiagnosticSource(t *testing.T, source string) (map[string]*ast.File, *check.Info, []check.Diagnostic) {
	t.Helper()
	file, diagnostics := parser.ParseFile(p4DiagnosticFilename, []byte(source))
	if len(diagnostics) != 0 {
		t.Fatalf("parse: %v", diagnostics)
	}
	expanded, err := expand.Expand([]*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolve.ResolveExpanded(map[string]*ast.File{p4DiagnosticFilename: file}, expanded, coreFixtureImporter{importer.Default()})
	if len(resolved.Diagnostics) != 0 {
		t.Fatalf("resolve: %v", resolved.Diagnostics)
	}
	files := map[string]*ast.File{p4DiagnosticFilename: expanded.Files[0]}
	info, checked := check.Check(files, resolved)
	return files, info, checked
}

func assertP4SourceDiagnostic(t *testing.T, source string, diagnostics []check.Diagnostic, code, message string) {
	t.Helper()
	start, end := markedP4Range(source)
	for _, diagnostic := range diagnostics {
		if diagnostic.Code != code || !strings.Contains(diagnostic.Message, message) {
			continue
		}
		if diagnostic.File != p4DiagnosticFilename || diagnostic.Span.Start < start || diagnostic.Span.Start >= diagnostic.Span.End || diagnostic.Span.End > end || diagnostic.Origin.File != diagnostic.File || diagnostic.Origin.Span != diagnostic.Span {
			t.Fatalf("%s diagnostic points outside marked source [%d:%d]: %#v", code, start, end, diagnostic)
		}
		return
	}
	t.Fatalf("missing %s / %q diagnostic: %v", code, message, diagnostics)
}

func markedP4Range(source string) (uint32, uint32) {
	return uint32(strings.Index(source, "/*bad*/") + len("/*bad*/")), uint32(strings.Index(source, "/*end*/"))
}
