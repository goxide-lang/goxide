package lower_test

import (
	"bytes"
	goast "go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	syntaxparser "github.com/goxide-lang/goxide/internal/syntax/parser"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestCoreEnumMatch(t *testing.T) {
	runCoreFixture(t, "enum_match", "idle zero positive:3 number:-2 pair:4:x record:7 text:y\n7 1 [1 2 3] 9\n", 5, 0)
}

func TestCoreGenericEnumInstances(t *testing.T) {
	runCoreFixture(t, "generic_enum", "42 hello 3 world true 7 mapped 8 9\n", 5, 3)
}

func TestCoreSumMatch(t *testing.T) {
	runCoreFixture(t, "sum_match", "-1 0 6 -2 0 8 bad true\n", 0, 3)
}

func TestCoreNilBeforeWildcard(t *testing.T) {
	runCoreFixture(t, "nil_match", "true false\n", 1, 0)
	t.Run("nested-dispatch", func(t *testing.T) {
		const name = "nestedfixture/main.gox"
		input := map[string]string{name: `package fixture
pub enum Inner[T] { A, Item(T), Record { Value T } }
pub enum Outer[T] { Wrap(Inner[T]), Pair(int, Inner[T]), Box(Option[Inner[T]]), Empty }
func mark(touched *int) bool { *touched++; return true }
func nested(value Outer[int], touched *int) int {
    match value {
        Outer::Wrap(Inner::Item(n)) if mark(touched) => { return n },
        Outer::Wrap(Inner::Record { Value: n }) => { return n },
        Outer::Wrap(p) => { _ = p; return 0 },
        _ => { return -1 },
    }
}
func short(value Outer[int]) int {
    match value {
        Outer::Pair(0, Inner::A) => { return 1 },
        _ => { return 0 },
    }
}
func boxed(value Outer[int], touched *int) int {
    true := false
    _ = true
    match value {
        Outer::Box(Option::Some(Inner::Item(n))) if mark(touched) => { return n },
        _ => { return 0 },
    }
}
func bind(value Outer[int], touched *int) Inner[int] {
    match value {
        Outer::Wrap(p) => { *touched++; return p },
        _ => { return Inner::A },
    }
}
func rematch(value Inner[int], touched *int) {
    match value { _ => { *touched++ } }
}
`}
		output, err := lowerDeriveSources(input)
		if err != nil {
			t.Fatal(err)
		}
		again, err := lowerDeriveSources(input)
		if err != nil || !reflect.DeepEqual(output, again) {
			t.Fatalf("repeated nested generation differs: %v", err)
		}
		const consumer = `package fixture
import (
    "fmt"
    "strings"
    "testing"
    "github.com/goxide-lang/std/sum"
)
func TestDispatch(t *testing.T) {
    rejects := func(run func(), touched *int) {
        t.Helper()
        message := ""
        func() { defer func() { message = fmt.Sprint(recover()) }(); run() }()
        if !strings.Contains(message, "match on nil Inner") || *touched != 0 {
            t.Fatalf("nested panic=%q guard=%d", message, *touched)
        }
    }
    for _, value := range []Inner[int]{nil, (*A[int])(nil), (*Item[int])(nil), (*Record[int])(nil)} {
        touched := 0
        outer := &Wrap[int]{Value: value}
        rejects(func() { nested(outer, &touched) }, &touched)
        if result := bind(outer, &touched); result != value || touched != 1 {
            t.Fatalf("binding changed payload or ran dispatch: %v, %d", result, touched)
        }
        touched = 0
        rejects(func() { rematch(value, &touched) }, &touched)
        boxedValue := &Box[int]{Value: sum.NewSome[Inner[int]](value)}
        rejects(func() { boxed(boxedValue, &touched) }, &touched)
        if got := short(&Pair[int]{V0: 1, V1: value}); got != 0 {
            t.Fatalf("unreached nested pattern returned %d", got)
        }
    }
    touched := 0
    if got := nested(&Wrap[int]{Value: &Item[int]{Value: 7}}, &touched); got != 7 || touched != 1 { t.Fatal(got, touched) }
    if got := nested(&Wrap[int]{Value: &Record[int]{Value: 9}}, &touched); got != 9 || touched != 1 { t.Fatal(got, touched) }
    if got := nested(&Wrap[int]{Value: &A[int]{}}, &touched); got != 0 || touched != 1 { t.Fatal(got, touched) }
    if got := short(&Pair[int]{V1: &A[int]{}}); got != 1 { t.Fatal(got) }
    if got := boxed(&Box[int]{Value: sum.NewSome[Inner[int]](&Item[int]{Value: 11})}, &touched); got != 11 || touched != 2 { t.Fatal(got, touched) }
    if got := boxed(&Box[int]{Value: sum.None[Inner[int]]()}, &touched); got != 0 || touched != 2 { t.Fatal(got, touched) }
}
`
		module := t.TempDir()
		if err := testdeps.WriteFile(filepath.Join(module, "go.mod"), []byte("module nestedfixture\n\ngo 1.27\n"), 0600); err != nil {
			t.Fatal(err)
		}
		for file, contents := range map[string][]byte{"generated.go": output[name], "generated_test.go": []byte(consumer)} {
			if err := os.WriteFile(filepath.Join(module, file), contents, 0600); err != nil {
				t.Fatal(err)
			}
		}
		runGo(t, module, "build", "./...")
		runGo(t, module, "test", "-race", "-count=1", "./...")
	})
}

func TestCoreMatchLoopAndDefer(t *testing.T) {
	runCoreFixture(t, "match_control", "22 4 4\n", 2, 0)
}

func TestCorePropagationEvaluationOrder(t *testing.T) {
	runCoreFixture(t, "propagation_order", "failed [callee first load cleanup]\n6 [callee first load last body late cleanup]\n", 0, 1)
}

func TestCorePropagationFunctionBoundaries(t *testing.T) {
	runCoreFixture(t, "propagation_contexts", "false []\nfailed [load]\nouter [load inner-cleanup outer]\nfailed [load cleanup]\n1 [load deferred cleanup]\n3 -1\nfailed [load guard-cleanup]\nguard [load guard-cleanup]\n", 1, 2)
}

func TestCorePropagationPreservesValues(t *testing.T) {
	runCoreFixture(t, "propagation_values", "3 true true true true\n", 0, 2)
}

func TestCorePropagationSingleStepConversion(t *testing.T) {
	runCoreFixture(t, "propagation_conversion", "1 0 true true false\n", 0, 1)
}

func TestCorePropagationLoopsSwitchesAndAssignments(t *testing.T) {
	runCoreFixture(t, "propagation_flow", "2 [cond0 body0 post0 cond1 body1 post1 cond2 body2 post2 cond3]\nstop [cond0 body0 post0 cond1 body1 post1]\nstop [tag case1 case2]\n20 [tag case1 case2 selected]\nstop [10] 100 [lhs rhs]\n7 [9] 7 [lhs rhs]\n9223372036854775815 [wide]\nstop [bare]\n1 [bare]\n7 []\n7 [goto]\n1 [post0 post1]\n", 0, 2)
}

func TestCorePropagationAggregateStorageAndOrder(t *testing.T) {
	runCoreFixture(t, "propagation_aggregate", "99 [index]\nstop [index]\n[9 9] [low]\nstop [low]\nstop [k1 v1 k2]\nstop [k1 v1 k2 v2]\n3 [k1 v1 k2 v2 k3 v3]\nstop [rhs]\npanic [rhs]\n6\n", 0, 4)
}

func TestCorePropagationTypeSwitchAndSelect(t *testing.T) {
	runCoreFixture(t, "propagation_select", "14\n-1 [channel value load] 0\n7 [channel value load] 0\n-1 0\n9 0\n", 1, 1)
}

func runCoreFixture(t *testing.T, fixture, wantOutput string, wantTypeSwitches, wantTagSwitches int) {
	t.Helper()
	directory := filepath.Join("testdata", "core", fixture)
	source, err := os.ReadFile(filepath.Join(directory, "main.gox"))
	if err != nil {
		t.Fatal(err)
	}
	name := "corefixture/main.gox"
	if fixture == "propagation_conversion" {
		// This isolated lowering fixture declares its protocol in the canonical
		// source package. The module integration tests load the actual std source.
		name = "github.com/goxide-lang/std/convert/main.gox"
	}
	file, diagnostics := syntaxparser.ParseFile(name, source)
	if len(diagnostics) != 0 {
		t.Fatalf("parse: %v", diagnostics)
	}
	expanded, err := expand.Expand([]*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resolved := resolve.ResolveExpanded(map[string]*ast.File{name: file}, expanded, coreFixtureImporter{importer.Default()})
	files := map[string]*ast.File{name: expanded.Files[0]}
	if len(resolved.Diagnostics) != 0 {
		t.Fatalf("resolve: %v", resolved.Diagnostics)
	}
	info, checked := check.Check(files, resolved)
	if len(checked) != 0 {
		t.Fatalf("check: %v", checked)
	}
	output, err := lower.Lower(files, info)
	if err != nil {
		t.Fatal(err)
	}
	again, err := lower.Lower(files, info)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output, again) {
		t.Fatal("lowering identical checked inputs produced different bytes")
	}
	goldenName := filepath.Join(directory, "main.golden.go")
	if os.Getenv("GOXIDE_UPDATE_CORE_GOLDEN") == "1" {
		if err := os.WriteFile(goldenName, output[name], 0600); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(goldenName)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output[name], golden) {
		t.Fatalf("generated code differs from %s/main.golden.go:\n%s", directory, output[name])
	}
	locations := regexp.MustCompile(`\bat ([^"\s]+):([0-9]+)\b`).FindAllStringSubmatch(string(output[name]), -1)
	if fixture == "nil_match" && len(locations) == 0 {
		t.Fatal("nil match lost its panic source location")
	}
	lines := bytes.Count(source, []byte{'\n'})
	if len(source) > 0 && source[len(source)-1] != '\n' {
		lines++
	}
	for _, location := range locations {
		if location[1] != name {
			t.Errorf("panic source = %s, want %s", location[1], name)
		}
		line, err := strconv.Atoi(location[2])
		if err != nil || line < 1 || line > lines {
			t.Errorf("panic location %s:%s is outside source lines 1..%d", location[1], location[2], lines)
		}
	}
	generatedAST, err := parser.ParseFile(token.NewFileSet(), "generated.go", output[name], 0)
	if err != nil {
		t.Fatal(err)
	}
	typeSwitches, tagSwitches := 0, 0
	goast.Inspect(generatedAST, func(node goast.Node) bool {
		switch node := node.(type) {
		case *goast.TypeSwitchStmt:
			typeSwitches++
		case *goast.SwitchStmt:
			if node.Init == nil {
				break
			}
			tag := node.Tag
			if init, ok := node.Init.(*goast.AssignStmt); ok && len(init.Rhs) == 1 {
				tag = init.Rhs[0]
			}
			if call, ok := tag.(*goast.CallExpr); ok {
				if selector, ok := call.Fun.(*goast.SelectorExpr); ok && strings.HasPrefix(selector.Sel.Name, "Tag") {
					tagSwitches++
				}
			}
		}
		return true
	})
	if typeSwitches != wantTypeSwitches || tagSwitches != wantTagSwitches {
		t.Fatalf("dispatch count: type switches %d (want %d), tag switches %d (want %d)", typeSwitches, wantTypeSwitches, tagSwitches, wantTagSwitches)
	}
	module := []byte("module corefixture\n\ngo 1.27\n")
	generated, handwritten := t.TempDir(), t.TempDir()
	equivalent, err := os.ReadFile(filepath.Join(directory, "equivalent.go"))
	if err != nil {
		t.Fatal(err)
	}
	for target, contents := range map[string][]byte{generated: output[name], handwritten: equivalent} {
		if err := testdeps.WriteFile(filepath.Join(target, "go.mod"), module, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(target, "main.go"), contents, 0600); err != nil {
			t.Fatal(err)
		}
		runGo(t, target, "build", "./...")
	}
	got, hand := runGo(t, generated, "run", "."), runGo(t, handwritten, "run", ".")
	if got != hand || got != wantOutput {
		t.Fatalf("runtime output: lowered %q, handwritten %q, want %q", got, hand, wantOutput)
	}
}

type coreFixtureImporter struct{ importer types.Importer }

func (i coreFixtureImporter) Lookup(parts []string, name resolve.Key) []resolve.External {
	pkg, err := i.importer.Import(strings.Join(parts, "/"))
	if err != nil || name.Ctx != 0 {
		return nil
	}
	object := pkg.Scope().Lookup(name.Name)
	if object == nil || !object.Exported() {
		return nil
	}
	var kind resolve.Kind
	switch object.(type) {
	case *types.TypeName:
		kind = resolve.Type
	case *types.Func:
		kind = resolve.Function
	case *types.Var:
		kind = resolve.Variable
	case *types.Const:
		kind = resolve.Constant
	default:
		return nil
	}
	return []resolve.External{{Name: name, Kind: kind, GoName: object.Name(), Visibility: resolve.Public}}
}
