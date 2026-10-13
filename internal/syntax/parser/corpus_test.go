package parser_test

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/parser"
	"github.com/hxsf/goxide/internal/syntax/printer"
	"github.com/hxsf/goxide/internal/syntax/token"
)

// Only syntax rejected by the authoritative frontend specification belongs here.
var corpusSkips = map[string]string{
	"examples/propagate_places/effects_fallback.gox":                                                               "uses hard-reserved unsafe as an unescaped package/path identifier (§3.3)",
	"examples/reexports/impl/macros.gox":                                                                           "package impl uses a hard-reserved word without r# (§3.3)",
	"website/checks/catalog/use_path_proposal.gox":                                                                 "use requires an identifier path, not a quoted string (§5.1)",
	"docs/reviews/match-unary-composite-evidence/testdata/acceptance/flows.gox":                                    "uses import, which .gox rejects (§5.1), and legacy dotted static paths in patterns (§5.7)",
	"docs/reviews/match-unary-composite-evidence/testdata/review-core/publication-core/cases/scope/flow/probe.gox": "uses import, which .gox rejects (§5.1)",
	"examples/enum_trial/model/config/defaults.gox":                                                                "func default uses a Go reserved keyword as an unescaped identifier (§3.3)",
	"examples/manual_default/config/types.gox":                                                                     "func default uses a Go reserved keyword as an unescaped identifier (§3.3)",
	"examples/manual_default/defaults.gox":                                                                         "func default uses a Go reserved keyword as an unescaped identifier (§3.3)",
	"examples/option_serde/defaults.gox":                                                                           "func default uses a Go reserved keyword as an unescaped identifier (§3.3)",
	"examples/serde_defaults/defaults.gox":                                                                         "func default uses a Go reserved keyword as an unescaped identifier (§3.3)",
	"examples/t0_joint/config/defaults.gox":                                                                        "func default uses a Go reserved keyword as an unescaped identifier (§3.3)",
	"website/examples/defaults.gox":                                                                                "func default uses a Go reserved keyword as an unescaped identifier (§3.3)",
	"website/checks/error_only.gox":                                                                                "uses import, which .gox rejects (§5.1)",
	"website/checks/macro_a01_opaque.gox":                                                                          "uses import, which .gox rejects (§5.1)",
	"website/checks/macro_a01_roles.gox":                                                                           "uses import, which .gox rejects (§5.1)",
	"website/checks/macro_imported_helper.gox":                                                                     "uses import, which .gox rejects (§5.1)",
	"website/checks/match_a04_literals.gox":                                                                        "uses import, which .gox rejects (§5.1)",
	"website/checks/match_a04_question.gox":                                                                        "uses import, which .gox rejects (§5.1)",
	"website/checks/match_a04_short_circuit.gox":                                                                   "uses import, which .gox rejects (§5.1)",
	"website/checks/match_a04_unary.gox":                                                                           "uses import, which .gox rejects (§5.1)",
	"website/checks/reject_macro_allocation_definition_import.gox":                                                 "uses import, which .gox rejects (§5.1)",
	"website/checks/reject_macro_allocation_primitive_import.gox":                                                  "uses import, which .gox rejects (§5.1)",
	"website/checks/reject_macro_append_definition_import.gox":                                                     "uses import, which .gox rejects (§5.1)",
	"website/checks/reject_macro_copy_definition_import.gox":                                                       "uses import, which .gox rejects (§5.1)",
	"website/checks/reject_macro_prepared.gox":                                                                     "uses import, which .gox rejects (§5.1)",
	"website/checks/reject_macro_selector.gox":                                                                     "uses import, which .gox rejects (§5.1)",
	"website/checks/reject_wrong_type.gox":                                                                         "uses import, which .gox rejects (§5.1)",
}

func structural(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return structural(v.Elem())
	}
	if v.Type() == reflect.TypeFor[ast.Span]() {
		return nil
	}
	switch v.Kind() {
	case reflect.Struct:
		out := map[string]any{"$type": v.Type().String()}
		for i := 0; i < v.NumField(); i++ {
			name := v.Type().Field(i).Name
			if v.Type() == reflect.TypeFor[ast.Base]() && (name == "NodeID" || name == "Loc") {
				continue
			}
			if v.Type() == reflect.TypeFor[ast.File]() && name == "LineStarts" {
				continue
			}
			if v.Type() == reflect.TypeFor[token.Token]() && (name == "Start" || name == "End") {
				continue
			}
			out[name] = structural(v.Field(i))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return nil
		}
		a := make([]any, v.Len())
		for i := range a {
			a[i] = structural(v.Index(i))
		}
		return a
	default:
		return v.Interface()
	}
}
func treeJSON(n ast.Node) string {
	b, e := json.Marshal(structural(reflect.ValueOf(n)))
	if e != nil {
		panic(e)
	}
	return string(b)
}
func badNodes(n ast.Node) []string {
	var bad []string
	ast.Walk(n, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.BadItem, *ast.BadStmt, *ast.BadExpr, *ast.BadType, *ast.BadPat:
			bad = append(bad, fmt.Sprintf("%T at %v", n, n.Span()))
		}
		return true
	})
	return bad
}
func errorsOnly(ds []parser.Diagnostic) []parser.Diagnostic {
	var out []parser.Diagnostic
	for _, d := range ds {
		if d.Severity == parser.Error {
			out = append(out, d)
		}
	}
	return out
}
func diagnostics(src []byte, ds []parser.Diagnostic) string {
	var b strings.Builder
	for i, d := range ds {
		if i == 10 {
			fmt.Fprintf(&b, "... %d more diagnostics\n", len(ds)-i)
			break
		}
		off := min(int(d.Span.Start), len(src))
		fmt.Fprintf(&b, "line %d: %s\n", strings.Count(string(src[:off]), "\n")+1, d.Message)
	}
	return b.String()
}
func assertRoundTrip(t *testing.T, f *ast.File) {
	t.Helper()
	printed := printer.Source(f)
	again, ds := parser.ParseFile("printed.gox", []byte(printed))
	if e := errorsOnly(ds); len(e) > 0 {
		t.Fatalf("reparse errors:\n%s\nprinted:\n%s", diagnostics([]byte(printed), e), printed)
	}
	if b := badNodes(again); len(b) > 0 {
		t.Fatalf("reparse bad nodes: %v", b)
	}
	a, b := treeJSON(f), treeJSON(again)
	if a != b {
		i := 0
		for i < len(a) && i < len(b) && a[i] == b[i] {
			i++
		}
		t.Fatalf("structural roundtrip differs at %d\noriginal: %s\nreparsed: %s\nprinted:\n%s", i, a[max(0, i-160):min(len(a), i+260)], b[max(0, i-160):min(len(b), i+260)], printed)
	}
}

func TestRepositoryCorpus(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	seen := map[string]bool{}
	passed, skipped, total := 0, 0, 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "website/node_modules" || rel == "website/dist" || d.Name() == "target" || d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".gox" {
			return nil
		}
		total++
		seen[rel] = true
		t.Run(rel, func(t *testing.T) {
			src, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			f, ds := parser.ParseFile(rel, src)
			errs := errorsOnly(ds)
			bad := badNodes(f)
			if reason, ok := corpusSkips[rel]; ok {
				if len(errs) == 0 && len(bad) == 0 {
					t.Fatalf("obsolete skip: %s", reason)
				}
				skipped++
				t.Skip(reason)
			}
			if len(errs) > 0 || len(bad) > 0 {
				t.Fatalf("parse errors:\n%sbad nodes: %v", diagnostics(src, errs), bad)
			}
			assertRoundTrip(t, f)
			passed++
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for path := range corpusSkips {
		if !seen[path] {
			t.Errorf("skip file no longer exists: %s", path)
		}
	}
	if total == 0 {
		t.Fatal("no .gox corpus files found")
	}
	t.Logf("corpus: total=%d passed=%d skipped=%d", total, passed, skipped)
}
