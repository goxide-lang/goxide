package expand

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
	"github.com/hxsf/goxide/internal/syntax/printer"
)

func expandFile(t *testing.T, name, source string) *ast.File {
	t.Helper()
	f, diagnostics := parser.ParseFile(name, []byte("package p\n"+source))
	for _, d := range diagnostics {
		if d.Severity == parser.Error {
			t.Fatalf("parse %s: %v\n%s", name, d, source)
		}
	}
	return f
}

func expandFiles(t *testing.T, files ...*ast.File) *Result {
	t.Helper()
	r, err := Expand(files, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func expandValue(t *testing.T, f *ast.File, name string) ast.Expr {
	t.Helper()
	for _, item := range f.Items {
		if v, ok := item.(*ast.VarDecl); ok {
			for _, spec := range v.Specs {
				if spec.Names[0].Name == name {
					return spec.Values[0]
				}
			}
		}
	}
	t.Fatalf("missing variable %s", name)
	return nil
}

func expandIdents(n ast.Node, name string) []*ast.Ident {
	var ids []*ast.Ident
	ast.Walk(n, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && id.Name == name {
			ids = append(ids, id)
		}
		return true
	})
	return ids
}

func TestExpandCollectsPackageDefinitionsBeforeMatching(t *testing.T) {
	call := expandFile(t, "call.gox", "var result = choose!(short extra)")
	def := expandFile(t, "definition.gox", `
var earlier = choose!(short)
macro_rules! choose {
    (short) => { 1 };
    ($x:ident extra) => { 2 };
    ($x:ident $y:ident) => { 3 };
}`)
	r := expandFiles(t, call, def)
	if got := expandValue(t, r.Files[0], "result").(*ast.BasicLit).Value; got != "2" {
		t.Fatalf("first complete rule produced %s", got)
	}
	if got := expandValue(t, r.Files[1], "earlier").(*ast.BasicLit).Value; got != "1" {
		t.Fatalf("declaration-order-independent invocation produced %s", got)
	}
	for _, f := range r.Files {
		for _, item := range f.Items {
			if _, ok := item.(*ast.MacroRulesDecl); ok {
				t.Fatal("macro declaration remains in expanded declarations")
			}
		}
	}
	bad := expandFile(t, "bad.gox", "var x = choose!(short extra again)")
	if _, err := Expand([]*ast.File{bad, def}, nil); err == nil {
		t.Fatal("accepted prefix-only matches")
	}
}

func TestExpandNestedCallsCarryExpansionStack(t *testing.T) {
	def := expandFile(t, "definition.gox", `
macro_rules! outer { ($x:expr) => { inner!($x) }; }
macro_rules! inner { ($x:expr) => { $x + 1 }; }`)
	call := expandFile(t, "call.gox", "var x = outer!(5)")
	r := expandFiles(t, call, def)
	if len(r.Expansions) != 2 {
		t.Fatalf("expansions = %d", len(r.Expansions))
	}
	outer, inner := r.Expansions[1], r.Expansions[2]
	if outer.Parent != 0 || inner.Parent != 1 || !reflect.DeepEqual(inner.Stack, []ast.ExpnID{1, 2}) {
		t.Fatalf("bad expansion chain: %#v / %#v", outer, inner)
	}
	if outer.Call.File != call || inner.Call.File != def || inner.Definition.File != def {
		t.Fatal("nested call lost its original template location")
	}
	x := expandValue(t, r.Files[0], "x").(*ast.BinaryExpr)
	if x.Expn != 2 || r.Origins[x.X].File != call {
		t.Fatalf("nested result provenance = %d / %#v", x.Expn, r.Origins[x.X])
	}
	ast.Walk(x, func(n ast.Node) bool {
		if _, macro := n.(*ast.MacroExpr); macro {
			t.Fatal("nested invocation was not expanded")
		}
		if expn := nodeExpn(n); expn != 0 {
			if _, exists := r.Expansions[expn]; !exists {
				t.Errorf("node points to missing expansion %d", expn)
			}
		}
		return true
	})
}

func TestExpandHygieneKeepsSpellingContextsAndInputImmutable(t *testing.T) {
	const source = `
macro_rules! local { ($x:expr) => {
    func() int { saved := 1; return saved + $x }()
}; }
var first = local!(saved)
var second = local!(saved)`
	f := expandFile(t, "hygiene.gox", source)
	before := expandFile(t, "hygiene.gox", source)
	for _, input := range []*ast.File{f, before} {
		ast.Walk(input, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				id.Ctx = 41
			}
			return true
		})
	}
	r := expandFiles(t, f)
	if !reflect.DeepEqual(f, before) {
		t.Fatal("expansion mutated the input AST")
	}
	contexts := map[ast.SyntaxCtx]int{}
	for _, id := range expandIdents(r.Files[0], "saved") {
		contexts[id.Ctx]++
	}
	if len(contexts) != 3 || contexts[41] != 2 {
		t.Fatalf("capture/local identities = %v", contexts)
	}
	for ctx, count := range contexts {
		if count != 2 || ctx != 41 && ctx <= 41 {
			t.Errorf("identity %d has %d uses", ctx, count)
		}
	}
	if strings.Contains(printer.Source(r.Files[0]), "__goxide_") {
		t.Fatal("hygiene rewrote identifier text")
	}
	if len(r.Contexts) != 2 {
		t.Fatalf("fresh expansion contexts = %d", len(r.Contexts))
	}
}

func TestExpandDefinitionAndExplicitCallerScopes(t *testing.T) {
	def := expandFile(t, "definition.gox", `
macro_rules! scoped { () => { helper($caller::helpers::run(), $caller::Local) }; }`)
	call := expandFile(t, "call.gox", "var value = scoped!()")
	r := expandFiles(t, call, def)
	x := expandValue(t, r.Files[0], "value")
	for name, file := range map[string]*ast.File{"helper": def, "helpers": call, "run": call, "Local": call} {
		ids := expandIdents(x, name)
		if len(ids) != 1 || r.Scopes[ids[0]].File != file {
			t.Errorf("%s resolution scope does not point to its source scope", name)
			continue
		}
		if (ids[0].Ctx == 0) != (file == call) {
			t.Errorf("%s context = %d", name, ids[0].Ctx)
		}
	}
}

func TestExpandTypeFragmentsAndExpressionBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, source string
	}{
		{"parameters-results-variables", `macro_rules! typed { ($T:type) => { func(v $T) $T { var x $T; return x }(0) }; }; var x = typed!(int)`},
		{"field", `macro_rules! typed { ($T:type) => { struct { value $T } }; }; type X = typed!(int)`},
		{"generic", `macro_rules! typed { ($T:type) => { Box[$T] }; }; type X = typed!(int)`},
		{"array-length-expression", `macro_rules! array { ($n:expr) => { [$n]int }; }; type X = array!(3)`},
		{"whole-expression", `macro_rules! identity { ($x:expr) => { len($x) }; }; var x = identity!([1]int{1})`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := expandFile(t, tt.name+".gox", tt.source)
			expandFiles(t, f)
		})
	}
	for _, tt := range []struct {
		name, source string
	}{
		{"expression-parameter", `macro_rules! bad { ($T:expr) => { func(v $T) {} }; }; var x = bad!(int)`},
		{"identifier-slice", `macro_rules! bad { ($T:ident) => { []$T }; }; type X = bad!(int)`},
		{"token-tree-field", `macro_rules! bad { ($T:tt) => { struct { value $T } }; }; type X = bad!(int)`},
		{"expression-generic", `macro_rules! bad { ($T:expr) => { pair[$T, int] }; }; var x = bad!(int)`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := expandFile(t, tt.name+".gox", tt.source)
			_, err := Expand([]*ast.File{f}, nil)
			var d *Diagnostic
			if !errors.As(err, &d) || d.Code != "category" || d.Definition.File != f || d.Call.File != f {
				t.Fatalf("expected two-location category diagnostic, got %v", err)
			}
		})
	}
}

func TestExpandItemsJoinCallerDeclarations(t *testing.T) {
	def := expandFile(t, "definition.gox", `
macro_rules! make_type { ($T:type) => { pub type Generated $T }; }
macro_rules! pass_item { ($i:item) => { $i }; }`)
	call := expandFile(t, "call.gox", `
make_type!(int)
pass_item!(pub func work() int { return 7 })`)
	r := expandFiles(t, call, def)
	if len(r.Files[0].Items) != 2 || r.Files[0].Package.Name != "p" {
		t.Fatalf("caller declarations = %#v", r.Files[0].Items)
	}
	decl := r.Files[0].Items[0].(*ast.TypeDecl)
	fn := r.Files[0].Items[1].(*ast.FuncDecl)
	for _, id := range []*ast.Ident{decl.Name, fn.Name} {
		if id.Ctx != 0 || r.Scopes[id].File != call {
			t.Errorf("generated item %s did not enter caller scope", id.Name)
		}
	}
	if decl.Vis.Kind != ast.VisPub || fn.Vis.Kind != ast.VisPub || decl.Name.Name != "Generated" || fn.Name.Name != "work" {
		t.Fatal("item visibility or spelling changed")
	}
	if r.Origins[fn.Body].File != call {
		t.Fatal("captured item lost caller provenance")
	}
}

func TestExpandStatementTemplateSplicesMultipleStatements(t *testing.T) {
	f := expandFile(t, "statements.gox", `
macro_rules! pair { ($x:expr) => { first($x); second($x) }; }
func run() { pair!(7) }`)
	r := expandFiles(t, f)
	fn := r.Files[0].Items[0].(*ast.FuncDecl)
	if len(fn.Body.List) != 2 {
		t.Fatalf("expanded statement count = %d", len(fn.Body.List))
	}
	for i, name := range []string{"first", "second"} {
		if len(expandIdents(fn.Body.List[i], name)) != 1 {
			t.Errorf("statement %d did not call %s", i, name)
		}
	}
}

func TestExpandParallelAndNestedRepetitions(t *testing.T) {
	f := expandFile(t, "repetitions.gox", `
macro_rules! table { ($($k:expr => $v:expr),* $(,)?) => { pairs($(pair($k, $v)),*) }; }
macro_rules! rows { ($( [$($x:expr),*] );*) => { matrix($(row($($x),*)),*) }; }
var tableValue = table!(a => 1, b => 2,)
var rowsValue = rows!([1,2];[];[3])`)
	r := expandFiles(t, f)
	table := expandValue(t, r.Files[0], "tableValue").(*ast.CallExpr)
	if len(table.Args) != 2 {
		t.Fatalf("table pairs = %d", len(table.Args))
	}
	for i, name := range []string{"a", "b"} {
		pair := table.Args[i].(*ast.CallExpr)
		if len(pair.Args) != 2 || len(expandIdents(pair.Args[0], name)) != 1 || pair.Args[1].(*ast.BasicLit).Value != fmt.Sprint(i+1) {
			t.Fatalf("pair %d did not retain synchronized captures", i)
		}
	}
	rows := expandValue(t, r.Files[0], "rowsValue").(*ast.CallExpr)
	if len(rows.Args) != 3 {
		t.Fatalf("rows = %d", len(rows.Args))
	}
	for i, length := range []int{2, 0, 1} {
		if got := len(rows.Args[i].(*ast.CallExpr).Args); got != length {
			t.Errorf("row %d has %d captures, want %d", i, got, length)
		}
	}
}

type expandResolverFunc func(*ast.Path, Scope) (*ast.MacroRulesDecl, *ast.File, error)

func (f expandResolverFunc) LookupMacro(path *ast.Path, scope Scope) (*ast.MacroRulesDecl, *ast.File, error) {
	return f(path, scope)
}

func TestExpandResolverKeepsSameNamedPackagesDistinct(t *testing.T) {
	local := expandFile(t, "local.gox", `
macro_rules! helper { () => { 1 }; }
var value = external::outer!()`)
	external := expandFile(t, "external.gox", `
macro_rules! outer { () => { helper!() }; }
macro_rules! helper { () => { 2 }; }`)
	outer := external.Items[0].(*ast.MacroRulesDecl)
	helper := external.Items[1].(*ast.MacroRulesDecl)
	lookedUpHelper := false
	resolver := expandResolverFunc(func(path *ast.Path, scope Scope) (*ast.MacroRulesDecl, *ast.File, error) {
		if len(path.Segments) == 2 && path.Segments[0].Name.Name == "external" && scope.File == local {
			return outer, external, nil
		}
		if len(path.Segments) == 1 && path.Segments[0].Name.Name == "helper" && scope.File == external {
			lookedUpHelper = true
			return helper, external, nil
		}
		return nil, nil, errors.New("missing macro source in requested scope")
	})
	r, err := Expand([]*ast.File{local}, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if value := expandValue(t, r.Files[0], "value").(*ast.BasicLit).Value; value != "2" || !lookedUpHelper {
		t.Fatalf("external helper resolved to %s; resolver consulted = %v", value, lookedUpHelper)
	}
}

func TestExpandMissingSourceAndDuplicateDefinitions(t *testing.T) {
	t.Run("missing-source", func(t *testing.T) {
		f := expandFile(t, "missing.gox", "var x = absent::outer!()")
		resolver := expandResolverFunc(func(*ast.Path, Scope) (*ast.MacroRulesDecl, *ast.File, error) {
			return nil, nil, errors.New("missing source module absent")
		})
		_, err := Expand([]*ast.File{f}, resolver)
		var d *Diagnostic
		if !errors.As(err, &d) || d.Code != "macro" || !strings.Contains(d.Message, "source module absent") || d.Call.File != f {
			t.Fatalf("missing-source diagnosis = %v", err)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		a := expandFile(t, "a.gox", "macro_rules! same { () => { 1 }; }")
		b := expandFile(t, "b.gox", "macro_rules! same { () => { 2 }; }")
		_, err := Expand([]*ast.File{a, b}, nil)
		var d *Diagnostic
		if !errors.As(err, &d) || d.Code != "definition" || d.Definition.File != a || d.Call.File != b {
			t.Fatalf("duplicate-definition diagnosis = %v", err)
		}
		b.Items[0].(*ast.MacroRulesDecl).Name.Ctx = 9
		call := expandFile(t, "call.gox", "var x = same!()")
		x := expandValue(t, call, "x").(*ast.MacroExpr)
		x.Path.Segments[0].Name.Ctx = 9
		r := expandFiles(t, call, a, b)
		if got := expandValue(t, r.Files[0], "x").(*ast.BasicLit).Value; got != "2" {
			t.Fatalf("distinct Ctx macro resolved to %s", got)
		}
	})
}

func TestExpandRawIdentifiersPreserveSpellingAndContext(t *testing.T) {
	f := expandFile(t, "raw.gox", `
macro_rules! raw { ($x:ident) => { func() int { r#type := 1; return r#type + $x }() }; }
var x = raw!(r#type)`)
	r := expandFiles(t, f)
	ids := expandIdents(expandValue(t, r.Files[0], "x"), "type")
	counts := map[ast.SyntaxCtx]int{}
	for _, id := range ids {
		if !id.Raw || id.Name != "type" {
			t.Fatalf("raw identifier changed: %#v", id)
		}
		counts[id.Ctx]++
	}
	if len(ids) != 3 || counts[0] != 1 || len(counts) != 2 {
		t.Fatalf("raw capture and local contexts = %v", counts)
	}
}

func TestExpandTTParseErrorUsesTemplateCaptureSpan(t *testing.T) {
	const definition = "macro_rules! m { ($x:tt) => { $x }; }"
	def := expandFile(t, "definition.gox", definition)
	call := expandFile(t, "call.gox", "var padding_to_separate_template_and_call_offsets = 0; var x = m!(if)")
	_, err := Expand([]*ast.File{call, def}, nil)
	var d *Diagnostic
	want := uint32(len("package p\n") + strings.LastIndex(definition, "$x"))
	if !errors.As(err, &d) || d.Definition.File != def || d.Definition.Span != (ast.Span{Start: want, End: want + 2}) || d.Call.File != call {
		t.Fatalf("template capture diagnosis = %v, expected span %d:%d", err, want, want+2)
	}
}

func TestExpandFailuresHaveDefinitionCallAndNestedStack(t *testing.T) {
	for _, tt := range []struct {
		name, defs, call, code string
		depth                  int
	}{
		{"category", `macro_rules! need { ($x:ident) => { $x }; }`, "need!(123)", "category", 1},
		{"unconsumed", `macro_rules! need { (ok) => { 1 }; }`, "need!(ok extra)", "unconsumed", 1},
		{"repeat-length", `macro_rules! need { (($($k:ident),*);($($v:expr),*)) => { tuple($(pair($k,$v)),*) }; }`, "need!((a,b);(1))", "repetition-length", 1},
		{"nested-category", `macro_rules! need { ($x:expr) => { inner!($x) }; }; macro_rules! inner { ($x:ident) => { $x }; }`, "need!(123)", "category", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			def := expandFile(t, "definition.gox", tt.defs)
			call := expandFile(t, "call.gox", "var value = "+tt.call)
			r, err := Expand([]*ast.File{call, def}, nil)
			var d *Diagnostic
			if !errors.As(err, &d) || d.Code != tt.code {
				t.Fatalf("expected %s diagnostic, got %v", tt.code, err)
			}
			if r.Files != nil || d.Definition.File != def || d.Definition.Span.End <= d.Definition.Span.Start || d.Call.Span.End <= d.Call.Span.Start {
				t.Fatalf("missing failure spans or published partial AST: %#v", d)
			}
			stack := r.Expansions[d.Expn].Stack
			if len(stack) != tt.depth || r.Expansions[stack[0]].Call.File != call {
				t.Fatalf("failure stack = %v", stack)
			}
		})
	}
}

func TestExpandOutputAndRecursionBudgets(t *testing.T) {
	t.Run("output", func(t *testing.T) {
		input := "1"
		for range 16 {
			input = "double!(" + input + ")"
		}
		f := expandFile(t, "output.gox", "macro_rules! double { ($x:expr) => { $x + $x }; }; var value = "+input)
		r, err := Expand([]*ast.File{f}, nil)
		var d *Diagnostic
		if !errors.As(err, &d) || d.Code != "budget" || !strings.Contains(d.Message, "output") {
			t.Fatalf("expected output budget diagnostic, got %v", err)
		}
		if r.Files != nil {
			t.Fatal("budget failure published partial output")
		}
	})
	t.Run("recursive", func(t *testing.T) {
		f := expandFile(t, "recursive.gox", "macro_rules! again { () => { again!() }; }; var value = again!()")
		r, err := Expand([]*ast.File{f}, nil)
		var d *Diagnostic
		if !errors.As(err, &d) || d.Code != "budget" || len(r.Expansions[d.Expn].Stack) != MaxDepth+1 {
			t.Fatalf("expected bounded recursive expansion, got %v", err)
		}
	})
}
