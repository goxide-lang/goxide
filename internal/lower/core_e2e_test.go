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
	"strconv"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/expand"
	"github.com/hxsf/goxide/internal/lower"
	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
	syntaxparser "github.com/hxsf/goxide/internal/syntax/parser"
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
	runCoreFixture(t, "propagation_conversion", "1 0 true true false\n", 0, 1, "FromWrapped", "FromAssignable")
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

func runCoreFixture(t *testing.T, fixture, wantOutput string, wantTypeSwitches, wantTagSwitches int, helpers ...string) {
	t.Helper()
	directory := filepath.Join("testdata", "core", fixture)
	source, err := os.ReadFile(filepath.Join(directory, "main.gox"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "corefixture/main.gox"
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
	var conversions []resolve.Ref
	for _, helper := range helpers {
		found := false
		for _, item := range expanded.Files[0].Items {
			if function, ok := item.(*ast.FuncDecl); ok && function.Name.Name == helper {
				conversions = append(conversions, resolve.Ref{File: name, ID: function.ID()})
				found = true
			}
		}
		if !found {
			t.Fatalf("missing conversion helper %s", helper)
		}
	}
	info, checked := check.CheckWithConversions(files, resolved, conversions)
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
	std, err := filepath.Abs(filepath.Join("..", "..", "..", "std"))
	if err != nil {
		t.Fatal(err)
	}
	module := []byte("module corefixture\n\ngo 1.27\n\nrequire github.com/goxide-lang/std v0.0.0\nreplace github.com/goxide-lang/std => " + strconv.Quote(std) + "\n")
	generated, handwritten := t.TempDir(), t.TempDir()
	equivalent, err := os.ReadFile(filepath.Join(directory, "equivalent.go"))
	if err != nil {
		t.Fatal(err)
	}
	for target, contents := range map[string][]byte{generated: output[name], handwritten: equivalent} {
		if err := os.WriteFile(filepath.Join(target, "go.mod"), module, 0600); err != nil {
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
