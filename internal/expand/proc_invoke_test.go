package expand

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

func TestProceduralFunctionPositionsAndCardinality(t *testing.T) {
	for _, test := range []struct {
		name, source, output, wantError string
		position                        pm.Position
		count                           int
	}{
		{"expression", "var value = emit!()", "42", "", pm.ExpressionPosition, 1},
		{"type", "type Value emit!()", "[]int", "", pm.TypePosition, 1},
		{"statements empty", "func run() { emit!() }", "", "", pm.StatementsPosition, 0},
		{"statements many", "func run() { emit!() }", "first(); second()", "", pm.StatementsPosition, 2},
		{"statement", "func run() { label: emit!() }", "first()", "", pm.StatementPosition, 1},
		{"statement empty", "func run() { label: emit!() }", "", "exactly one statement", pm.StatementPosition, 0},
		{"statement many", "func run() { label: emit!() }", "first(); second()", "exactly one statement", pm.StatementPosition, 0},
		{"items empty", "emit!()", "", "", pm.ItemsPosition, 0},
		{"items many", "emit!()", "type First int; type Second int", "", pm.ItemsPosition, 2},
		{"trait members", "trait Value { emit!() }", "type Output; func value(self Self) Output", "", pm.TraitMembersPosition, 2},
		{"impl members", "type Value int; impl Value { emit!() }", "func first(self Self) {}; func second(self Self) {}", "", pm.ImplMembersPosition, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var positions []pm.Position
			resolver := newAttributeResolver(t, []pm.Entry{{Name: "emit", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
				return procText(ctx, test.output)
			}}}, nil)
			macro := resolver.macros["emit"]
			run := macro.Run
			macro.Run = func(request pm.Request) ([]byte, error) {
				positions = append(positions, request.Position())
				return run(request)
			}
			resolver.macros["emit"] = macro
			result, err := expandAttributes([]*ast.File{expandFile(t, "function_slot.gox", test.source)}, resolver)
			if !reflect.DeepEqual(positions, []pm.Position{test.position}) {
				t.Fatalf("request positions = %v, want %q", positions, test.position)
			}
			if test.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantError) || len(result.Files) != 0 {
					t.Fatalf("invalid output was admitted: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			items := result.Files[0].Items
			count := len(items)
			switch test.position {
			case pm.ExpressionPosition:
				if value := expandValue(t, result.Files[0], "value").(*ast.BasicLit).Value; value != "42" {
					t.Fatalf("expression = %s", value)
				}
			case pm.TypePosition:
				if _, ok := items[0].(*ast.TypeDecl).Type.(*ast.SliceType); !ok {
					t.Fatalf("type = %T", items[0].(*ast.TypeDecl).Type)
				}
			case pm.StatementsPosition, pm.StatementPosition:
				count = len(items[0].(*ast.FuncDecl).Body.List)
			case pm.TraitMembersPosition:
				count = len(items[0].(*ast.TraitDecl).Items)
			case pm.ImplMembersPosition:
				count = len(items[1].(*ast.ImplDecl).Items)
			}
			if count != test.count {
				t.Fatalf("output count = %d, want %d", count, test.count)
			}
		})
	}
}

func TestProceduralFunctionRejectsAttributeAndDeriveEntries(t *testing.T) {
	called := false
	resolver := newAttributeResolver(t, []pm.Entry{
		{Name: "attribute", Attribute: func(_ pm.Context, _, _ public.TokenStream) pm.Output { called = true; return pm.Success(public.New()) }},
		{Name: "Derived", Derive: func(_ pm.Context, _ public.TokenStream) pm.Output { called = true; return pm.Success(public.New()) }},
	}, nil)
	for _, source := range []string{"var value = attribute!()", "type Value Derived!()", "attribute!()", "Derived!()"} {
		_, err := expandAttributes([]*ast.File{expandFile(t, "kind.gox", source)}, resolver)
		if err == nil || !strings.Contains(err.Error(), "function-like procedural macro") || called {
			t.Fatalf("wrong entry kind was executed: %s: called=%v err=%v", source, called, err)
		}
	}
}

func TestProceduralInvocationKeepsDiagnosticOrderAndFirstErrorLocation(t *testing.T) {
	resolver := newAttributeResolver(t, []pm.Entry{{Name: "report", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
		output := procText(ctx, "unusable_name")
		output.Diagnostics = []pm.Diagnostic{
			{Severity: pm.Warning, Message: "first", Span: ctx.CallSpan()},
			{Severity: pm.Error, Message: "failure", Span: ctx.DefSpan()},
			{Severity: pm.Error, Message: "last", Span: ctx.CallSpan()},
		}
		return output
	}}}, nil)
	file := expandFile(t, "diagnostics.gox", "var value = report!()")
	result, err := expandAttributes([]*ast.File{file}, resolver)
	var diagnostic *Diagnostic
	if !errors.As(err, &diagnostic) || len(result.Files) != 0 || diagnostic.Location.File != resolver.macros["report"].File {
		t.Fatalf("failed output or primary error location = %v", err)
	}
	var messages []string
	for _, diagnostic := range result.ProcDiagnostics {
		messages = append(messages, diagnostic.Message)
	}
	if !reflect.DeepEqual(messages, []string{"first", "failure", "last"}) || result.ProcDiagnostics[0].Location.File != file || result.ProcDiagnostics[2].Location.File != file {
		t.Fatalf("diagnostic order/locations = %+v", result.ProcDiagnostics)
	}
}

func TestProceduralNestedCallUsesAssociatedDiagnosticLocation(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "explicit"}[explicit], func(t *testing.T) {
			resolver := newAttributeResolver(t, []pm.Entry{
				{Name: "outer", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
					output := pm.FromParse(public.ParseText("last!()", ctx.CallSite()))
					tokens := output.Tokens.Tokens()
					for i := range tokens {
						tokens[i] = tokens[i].WithSpan(ctx.CallSpan())
					}
					output.Tokens = public.New(tokens...)
					return output
				}},
				{Name: "last", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
					var span public.Span
					if explicit {
						span = ctx.CallSpan()
					}
					return pm.Failure("nested fail", span)
				}},
			}, nil)
			file := expandFile(t, "caller.gox", "const Result = outer!()")
			result, err := expandAttributes([]*ast.File{file}, resolver)
			var diagnostic *Diagnostic
			if !errors.As(err, &diagnostic) || diagnostic.Location.File != file {
				t.Fatalf("nested diagnostic lost the associated source location: %+v", diagnostic)
			}
			if diagnostic.Call.File != resolver.macros["outer"].File || len(result.Expansions) != 2 || result.Expansions[2].Parent != 1 {
				t.Fatal("diagnostic position changed semantic provenance or expansion ancestry")
			}
			if len(result.ProcDiagnostics) != 1 || result.ProcDiagnostics[0].Location != diagnostic.Location {
				t.Fatalf("nested output diagnostic = %+v", result.ProcDiagnostics)
			}
		})
	}
}
