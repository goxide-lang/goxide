package resolve_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
)

func parseFiles(t *testing.T, sources map[string]string) map[string]*ast.File {
	t.Helper()
	files := make(map[string]*ast.File, len(sources))
	for name, source := range sources {
		file, diagnostics := parser.ParseFile(name, []byte(source))
		for _, d := range diagnostics {
			if d.Severity == parser.Error {
				t.Fatalf("parse %s: %v", name, diagnostics)
			}
		}
		files[name] = file
	}
	return files
}

func noDiagnostics(t *testing.T, result *resolve.Result) {
	t.Helper()
	if len(result.Diagnostics) != 0 {
		t.Fatalf("resolve diagnostics: %v", result.Diagnostics)
	}
}

func named(t *testing.T, file *ast.File, source, marker, name string) *ast.Ident {
	t.Helper()
	start := strings.Index(source, marker)
	rel := strings.Index(marker, name)
	if start < 0 || rel < 0 {
		t.Fatalf("missing test marker %q or identifier %q", marker, name)
	}
	var found *ast.Ident
	ast.Walk(file, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Span().Start == uint32(start+rel) {
			found = ident
		}
		return true
	})
	if found == nil {
		t.Fatalf("no identifier at %d (%q)", start+rel, marker)
	}
	return found
}

func diagnosticAt(t *testing.T, result *resolve.Result, file string, span ast.Span) resolve.Diagnostic {
	t.Helper()
	for _, d := range result.Diagnostics {
		if d.File == file && d.Span == span {
			if d.Message == "" || d.Code == "" || d.Error() == "" {
				t.Fatalf("unreadable diagnostic: %#v", d)
			}
			return d
		}
	}
	t.Fatalf("no diagnostic at %s:%v: %v", file, span, result.Diagnostics)
	return resolve.Diagnostic{}
}

func TestPackagesGroupByDirectoryAndClause(t *testing.T) {
	sources := map[string]string{
		"one/a.gox": "package p\nvar shared = 1\n",
		"one/b.gox": "package p\nfunc f() { _ = shared }\n",
		"one/c.gox": "package q\nvar shared = 2\n",
		"two/a.gox": "package p\nvar shared = 3\n",
	}
	files := parseFiles(t, sources)
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	if len(result.Packages) != 3 || len(result.Files) != 4 {
		t.Fatalf("package/file counts = %d/%d", len(result.Packages), len(result.Files))
	}
	decl := named(t, files["one/a.gox"], sources["one/a.gox"], "shared =", "shared")
	ref := named(t, files["one/b.gox"], sources["one/b.gox"], "_ = shared", "shared")
	if result.Files["one/a.gox"].Definitions[decl.ID()] != result.Files["one/b.gox"].References[ref.ID()].Symbol {
		t.Fatal("same-package reference did not resolve across files")
	}
}

func TestDuplicatePackageDeclaration(t *testing.T) {
	sources := map[string]string{"a.gox": "package p\nvar repeated = 1", "b.gox": "package p\nfunc repeated() {}"}
	files := parseFiles(t, sources)
	result := resolve.Resolve(files, nil)
	ident := named(t, files["b.gox"], sources["b.gox"], "repeated()", "repeated")
	diagnosticAt(t, result, "b.gox", ident.Span())
}

func TestPackageDeclarationConflictsWithPrelude(t *testing.T) {
	for _, name := range []string{"Option", "int", "len"} {
		t.Run(name, func(t *testing.T) {
			source := "package p\nvar " + name + " = 1"
			files := parseFiles(t, map[string]string{"p.gox": source})
			result := resolve.Resolve(files, nil)
			ident := named(t, files["p.gox"], source, name+" =", name)
			diagnosticAt(t, result, "p.gox", ident.Span())
		})
	}
}

func TestPreludeAndLocalShadowing(t *testing.T) {
	source := `package p
func f() {
 var a bool; var b byte; var c complex64; var d complex128; var e error
 var g float32; var h float64; var i int; var j int8; var k int16; var l int32; var m int64
 var n rune; var o string; var p uint; var q uint8; var r uint16; var s uint32; var t uint64; var u uintptr
 var v any; var w comparable
 var a8 i8; var a16 i16; var a32 i32; var a64 i64
 var b8 u8; var b16 u16; var b32 u32; var b64 u64
 var c32 f32; var c64 f64; var dsize isize; var esize usize
 var option Option[int]; var result Result[int, error]
 _, _, _, _, _, _, _, _ = true, false, nil, Some(1), None, Ok(1), Err(nil), iota
 _, _, _, _, _, _, _, _ = append, cap, clear, close, complex, copy, delete, imag
 _, _, _, _, _, _, _, _ = len, make, max, min, new, panic, print, println
 _, _ = real, recover
 Option := 1
 len := Option
 _ = len // local
}`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	decl := named(t, files["p.gox"], source, "len :=", "len")
	ref := named(t, files["p.gox"], source, "_ = len // local", "len")
	if result.Files["p.gox"].References[ref.ID()].Symbol != result.Files["p.gox"].Definitions[decl.ID()] {
		t.Fatal("local value did not shadow prelude")
	}
}

func TestPreludeStaticVariantsAndBasicAliases(t *testing.T) {
	source := "package p\nfunc f() { var x i32; _ = Option[int]::Some(x); _ = Result[int, error]::Ok(x) }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	file := result.Files["p.gox"]
	alias := named(t, files["p.gox"], source, "x i32", "i32")
	if file.References[alias.ID()].Symbol.GoName != "int32" {
		t.Fatal("i32 did not map to int32")
	}
	for _, name := range []string{"Some", "Ok"} {
		id := named(t, files["p.gox"], source, "::"+name, name)
		if symbol := file.References[id.ID()].Symbol; symbol == nil || symbol.Kind != resolve.Variant {
			t.Fatalf("prelude variant %s was not resolved", name)
		}
	}
}

func TestGoScopeShadowing(t *testing.T) {
	source := `package p
var x = 1
func f(x int) {
 _ = x // parameter
 { x := x; _ = x /* inner */ }
 x, y := x, 2
 _ = x // parameter reused
 _ = y
}
func g() { _ = x /* package */ }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	f := result.Files["p.gox"]
	def := func(marker string) *resolve.Symbol {
		return f.Definitions[named(t, files["p.gox"], source, marker, "x").ID()]
	}
	ref := func(marker string) *resolve.Symbol {
		return f.References[named(t, files["p.gox"], source, marker, "x").ID()].Symbol
	}
	param, inner, pkg := def("x int"), def("x := x"), def("x = 1")
	if param == nil || inner == nil || pkg == nil || param == inner || param == pkg {
		t.Fatal("distinct scopes did not define distinct symbols")
	}
	if ref("_ = x // parameter") != param || ref("x; _ =") != param || ref("_ = x /* inner */") != inner || ref("_ = x // parameter reused") != param || ref("_ = x /* package */") != pkg {
		t.Fatal("Go-style declaration scope or short-declaration reuse is incorrect")
	}
}

func TestShortDeclarationRequiresNewName(t *testing.T) {
	files := parseFiles(t, map[string]string{"p.gox": "package p\nfunc f(x int) { x := 1 }"})
	result := resolve.Resolve(files, nil)
	if len(result.Diagnostics) == 0 {
		t.Fatal("short declaration without a new variable accepted")
	}
}

func TestUnresolvedNameSpan(t *testing.T) {
	source := "package p\nfunc f() { _ = absent }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	ident := named(t, files["p.gox"], source, "absent", "absent")
	diagnosticAt(t, result, "p.gox", ident.Span())
}

func TestHygienicLookupAndImmutableAST(t *testing.T) {
	source := `package p
func f() {
 x := 1
 x := 2
 _ = x // caller
 _ = x // expansion
 _ = x // unmatched
}`
	files := parseFiles(t, map[string]string{"p.gox": source})
	f := files["p.gox"]
	caller := named(t, f, source, "x := 1", "x")
	expansion := named(t, f, source, "x := 2", "x")
	expansion.Ctx = 7
	callerRef := named(t, f, source, "x // caller", "x")
	expansionRef := named(t, f, source, "x // expansion", "x")
	expansionRef.Ctx = 7
	unmatched := named(t, f, source, "x // unmatched", "x")
	unmatched.Ctx = 8
	before, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	result := resolve.Resolve(files, nil)
	after, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("resolver modified AST")
	}
	resolved := result.Files["p.gox"]
	if resolved.References[callerRef.ID()].Symbol != resolved.Definitions[caller.ID()] || resolved.References[expansionRef.ID()].Symbol != resolved.Definitions[expansion.ID()] {
		t.Fatal("lookup ignored syntax context")
	}
	if resolved.Definitions[caller.ID()] == resolved.Definitions[expansion.ID()] {
		t.Fatal("hygienically distinct bindings merged")
	}
	diagnosticAt(t, result, "p.gox", unmatched.Span())
	if len(result.Diagnostics) != 1 {
		t.Fatalf("unexpected diagnostics: %v", result.Diagnostics)
	}
}

func TestEffectiveVisibilityAndGoNames(t *testing.T) {
	source := `package p
func Exported() {}
func hidden() {}
pub func publicName() {}
type GoName int
struct PrivateType { Upper int; pub lower int }
pub struct publicType {}
type GoStruct struct { Export int; hide int; pub explicit int }
enum PrivateEnum { Capital, small }
pub enum publicEnum { variant }
trait PrivateTrait { func Method(); type Assoc }
pub trait publicTrait { func read(); type Item }
struct Target {}
impl Target { func Internal() {}; pub func accessible() {} }
impl publicTrait for Target { func read() {}; type Item = int }
var GoValue int
pub var publicValue int
const GoConst = 1
pub const publicConst = 2
pub func élève() {}
mod PrivateModule;
pub mod publicModule;
macro_rules! PrivateMacro { () => {}; }
pub macro_rules! publicMacro { () => {}; }
`
	files := parseFiles(t, map[string]string{"p.gox": source})
	result := resolve.Resolve(files, nil)
	noDiagnostics(t, result)
	for _, tc := range []struct {
		marker, name, goName string
		visibility           resolve.Visibility
	}{
		{"Exported()", "Exported", "Exported", resolve.Public},
		{"hidden()", "hidden", "hidden", resolve.Private},
		{"publicName()", "publicName", "PublicName", resolve.Public},
		{"GoName int", "GoName", "GoName", resolve.Public},
		{"PrivateType {", "PrivateType", "_PrivateType", resolve.Private},
		{"Upper int", "Upper", "_Upper", resolve.Private},
		{"lower int", "lower", "Lower", resolve.Public},
		{"publicType {", "publicType", "PublicType", resolve.Public},
		{"Export int", "Export", "Export", resolve.Public},
		{"hide int", "hide", "hide", resolve.Private},
		{"explicit int", "explicit", "Explicit", resolve.Public},
		{"PrivateEnum {", "PrivateEnum", "_PrivateEnum", resolve.Private},
		{"Capital,", "Capital", "_Capital", resolve.Private},
		{"small }", "small", "small", resolve.Private},
		{"publicEnum {", "publicEnum", "PublicEnum", resolve.Public},
		{"variant }", "variant", "Variant", resolve.Public},
		{"PrivateTrait {", "PrivateTrait", "_PrivateTrait", resolve.Private},
		{"Method()", "Method", "_Method", resolve.Private},
		{"Assoc }", "Assoc", "_Assoc", resolve.Private},
		{"publicTrait {", "publicTrait", "PublicTrait", resolve.Public},
		{"read();", "read", "Read", resolve.Public},
		{"Item }", "Item", "Item", resolve.Public},
		{"Internal()", "Internal", "_Internal", resolve.Private},
		{"accessible()", "accessible", "Accessible", resolve.Public},
		{"read() {}", "read", "Read", resolve.Public},
		{"Item =", "Item", "Item", resolve.Public},
		{"GoValue int", "GoValue", "GoValue", resolve.Public},
		{"publicValue int", "publicValue", "PublicValue", resolve.Public},
		{"GoConst =", "GoConst", "GoConst", resolve.Public},
		{"publicConst =", "publicConst", "PublicConst", resolve.Public},
		{"élève()", "élève", "Élève", resolve.Public},
		{"PrivateModule;", "PrivateModule", "_PrivateModule", resolve.Private},
		{"publicModule;", "publicModule", "PublicModule", resolve.Public},
		{"PrivateMacro {", "PrivateMacro", "_PrivateMacro", resolve.Private},
		{"publicMacro {", "publicMacro", "PublicMacro", resolve.Public},
	} {
		t.Run(tc.marker, func(t *testing.T) {
			id := named(t, files["p.gox"], source, tc.marker, tc.name).ID()
			file := result.Files["p.gox"]
			visibility, ok := file.Visibility[id]
			if !ok || visibility != tc.visibility || file.GoNames[id] != tc.goName {
				t.Fatalf("visibility/name = %v/%q (present %v), want %v/%q", visibility, file.GoNames[id], ok, tc.visibility, tc.goName)
			}
		})
	}
}

func TestGoNameMappingRejectsConflictsAndUnexportableNames(t *testing.T) {
	for _, body := range []string{
		"pub func lower() {}\nfunc Lower() {}",
		"enum Mode {}\nvar _Mode int",
		"pub func _hidden() {}",
		"pub struct 世界 {}",
	} {
		t.Run(body, func(t *testing.T) {
			files := parseFiles(t, map[string]string{"p.gox": "package p\n" + body})
			result := resolve.Resolve(files, nil)
			if len(result.Diagnostics) == 0 {
				t.Fatal("invalid Go-name mapping accepted")
			}
		})
	}
}

func TestReservedIdentifiersRespectRawEscapes(t *testing.T) {
	source := "package p\nvar r#match = 1\nfunc f() { _ = r#match }"
	files := parseFiles(t, map[string]string{"p.gox": source})
	noDiagnostics(t, resolve.Resolve(files, nil))
	decl := named(t, files["p.gox"], source, "r#match =", "r#match")
	decl.Raw = false
	result := resolve.Resolve(files, nil)
	diagnosticAt(t, result, "p.gox", decl.Span())
}

func TestRecoveredSyntaxIsSafeToResolve(t *testing.T) {
	file, diagnostics := parser.ParseFile("p.gox", []byte("package p\nfunc bad() { x := ; }\nfunc good() {}"))
	if len(diagnostics) == 0 {
		t.Fatal("fixture did not exercise syntax recovery")
	}
	result := resolve.Resolve(map[string]*ast.File{"p.gox": file}, nil)
	for _, d := range result.Diagnostics {
		if d.Error() == "" {
			t.Fatal("empty diagnostic")
		}
	}
}

func TestExamplesSmoke(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "qualified_packages", "*.gox"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("example files: %v, %v", paths, err)
	}
	sources := make(map[string]string)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sources[path] = string(data)
	}
	files := parseFiles(t, sources)
	result := resolve.Resolve(files, nil)
	if len(result.Files) != len(files) {
		t.Fatalf("resolved %d of %d files", len(result.Files), len(files))
	}
	for _, d := range result.Diagnostics {
		source, exists := sources[d.File]
		if !exists || d.Message == "" || d.Error() == "" || d.Span.End < d.Span.Start || d.Span.End > uint32(len(source)) {
			t.Errorf("unreadable example diagnostic: %#v", d)
		}
	}
	if again := resolve.Resolve(files, nil); !reflect.DeepEqual(result.Diagnostics, again.Diagnostics) {
		t.Fatal("diagnostics depend on map iteration order")
	}
}
