package frontend

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
	"github.com/hxsf/goxide/internal/typecheck"
)

func TestGeneratedASTMacroDiagnostics(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		for _, expression := range []string{"len($x?)", "$x? + \"bad\"", "takes($x?)"} {
			t.Run(strings.ReplaceAll(expression, "$", "")+strings.ReplaceAll(newline, "\n", "LF"), func(t *testing.T) {
				source := strings.ReplaceAll("package p\nmacro_rules! inner {($x:expr)=>{"+expression+"};}\nmacro_rules! outer {($x:expr)=>{inner!($x)};}\nfunc takes(x string){}\nfunc value()Option[int]{return Option[int]::Some(1)}\nfunc f()Option[int]{_=outer!(value());return Option[int]::None}\n", "\n", newline)
				_, err := Compile("generated.gox", []byte(source))
				d := requireSyntheticMacroError(t, err, 2)
				for i, name := range []string{"outer", "inner"} {
					if d.Stack[i].Macro != name {
						t.Fatalf("wrong frame order: %+v", d.Stack)
					}
					checkMacroFrame(t, d.Stack[i].Definition, source, "macro_rules! "+name)
					checkMacroFrame(t, d.Stack[i].Callsite, source, name+"!(")
				}
			})
		}
	}
}

func TestGeneratedASTNativeContextStaysUnknown(t *testing.T) {
	source := []byte("package p\nfunc value()Option[int]{return Option[int]::Some(1)}\nfunc f()Option[int]{_=len(value()?);return Option[int]::None}\n")
	_, err := Compile("native.gox", source)
	var d *sourcemap.Error
	if !errors.As(err, &d) || d.Origin != nil || len(d.Stack) != 0 {
		t.Fatalf("native generated payload acquired a source owner: %v", err)
	}
}

func TestGeneratedASTSiblingContexts(t *testing.T) {
	source := "package p\nmacro_rules! take {($x:expr)=>{$x?};}\nfunc value()Option[int]{return Option[int]::Some(1)}\nfunc f()Option[int]{_=take!(value());_=take!(value());return Option[int]::None}\n"
	result, err := Compile("siblings.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	calls := map[int]bool{}
	for _, s := range result.Diagnostics.Segments {
		if s.Origin == nil && len(s.Stack) == 1 && s.Stack[0].Macro == "take" && strings.HasPrefix(string(result.Go[s.Start:s.End]), "__goxide_qtmp_") {
			checkMacroFrame(t, s.Stack[0].Callsite, source, "take!(")
			calls[s.Stack[0].Callsite.Start] = true
		}
	}
	first := strings.Index(source, "take!(value())")
	second := strings.LastIndex(source, "take!(value())")
	if len(calls) != 2 || !calls[first] || !calls[second] {
		t.Fatalf("generated sibling contexts crossed/lost: %v", calls)
	}
}

func TestGeneratedASTExactAndUnprovedControls(t *testing.T) {
	base := "package p\nmacro_rules! check {($x:expr)=>{EXPR};}\nfunc value()Option[int]{return Option[int]::Some(1)}\nfunc f()Option[int]{_=check!(value());return Option[int]::None}\n"
	source := strings.Replace(base, "EXPR", "($x?).Missing", 1)
	_, err := Compile("exact.gox", []byte(source))
	var d *sourcemap.Error
	if !errors.As(err, &d) || d.Origin == nil || len(d.Stack) != 1 || source[d.Origin.Start:d.Origin.End] != "Missing" {
		t.Fatalf("real selector token lost precision: %v", err)
	}
	// Index lowering additionally freezes a value in another temp factory. This
	// slice does not infer that temp's ownership from its question-bearing child.
	source = strings.Replace(base, "EXPR", "($x?)[0]", 1)
	_, err = Compile("unproved.gox", []byte(source))
	if !errors.As(err, &d) || d.Origin != nil || len(d.Stack) != 0 {
		t.Fatalf("unproved later temporary acquired context: %v", err)
	}
}

func TestGeneratedASTBuiltinRenamesPreserveOtherTokens(t *testing.T) {
	definitions := []byte("package p\nmacro_rules! size {($x:expr)=>{len($x)};}\n")
	caller := "package p\nuse strings as len;\nfunc f()int{_=len::TrimSpace(\" x \");len:=3;_=len;unaffected:=4;return size!([]int{unaffected})}\n"
	result, err := CompilePackageWithOptions("example/renames", map[string][]byte{"definitions.gox": definitions, "caller.gox": []byte(caller)}, PackageOptions{Importer: typecheck.NewImporter("caller.gox"), Roots: frontendFixtureRoots(nil)})
	if err != nil {
		t.Fatal(err)
	}
	file := result.Files["caller.gox"]
	fs := token.NewFileSet()
	parsed, err := parser.ParseFile(fs, "caller.go", file.Go, 0)
	if err != nil {
		t.Fatal(err)
	}
	exact := 0
	ast.Inspect(parsed, func(node ast.Node) bool {
		id, ok := node.(*ast.Ident)
		if !ok || id.Name != "unaffected" {
			return true
		}
		segment := file.Diagnostics.Lookup(fs.Position(id.Pos()).Offset)
		if segment == nil || segment.Origin == nil || segment.Origin.File != "caller.gox" {
			t.Fatalf("unrelated input token lost evidence: %+v", segment)
		}
		origin := segment.Origin
		if caller[origin.Start:origin.End] != "unaffected" {
			t.Fatalf("wrong source token: %+v", segment)
		}
		exact++
		return true
	})
	if exact != 2 {
		t.Fatalf("builtin/import/local renames erased unrelated exact tokens: %d\n%s", exact, file.Go)
	}
}
