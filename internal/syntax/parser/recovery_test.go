package parser_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/source"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

func TestRecoveryRetainsIncompleteSelectors(t *testing.T) {
	for _, prefix := range []string{"value.", "Value::"} {
		t.Run(prefix, func(t *testing.T) {
			source := "package p\nfunc broken() { " + prefix + "\n}\nfunc healthy() {}\n"
			file, diagnostics := parser.ParseFile("selector.gox", []byte(source))
			if len(errorsOnly(diagnostics)) != 1 {
				t.Fatalf("want one missing identifier error, got %v", diagnostics)
			}
			if len(file.Items) != 2 {
				t.Fatalf("want both functions, got %d items", len(file.Items))
			}
			broken, ok := file.Items[0].(*ast.FuncDecl)
			if !ok || broken.Name.Name != "broken" {
				t.Fatalf("lost broken function: %T", file.Items[0])
			}
			healthy, ok := file.Items[1].(*ast.FuncDecl)
			if !ok || healthy.Name.Name != "healthy" {
				t.Fatalf("lost healthy function: %T", file.Items[1])
			}
			statement, ok := broken.Body.List[0].(*ast.ExprStmt)
			if !ok {
				t.Fatalf("lost incomplete expression: %T", broken.Body.List[0])
			}
			var receiver, missing *ast.Ident
			switch expression := statement.X.(type) {
			case *ast.SelectorExpr:
				receiver, missing = expression.X.(*ast.Ident), expression.Sel
			case *ast.PathExpr:
				receiver, missing = expression.Path.Segments[0].Name, expression.Path.Segments[1].Name
			default:
				t.Fatalf("lost selector structure: %T", expression)
			}
			start := uint32(strings.Index(source, prefix))
			if receiver.Missing || receiver.Span() != (ast.Span{Start: start, End: start + 5}) {
				t.Fatalf("receiver changed: %#v", receiver)
			}
			offset := uint32(strings.Index(source, "}"))
			span := ast.Span{Start: offset, End: offset}
			if !missing.Missing || missing.Name != "" || missing.Span() != span || diagnostics[0].Span != span {
				t.Fatalf("missing identifier = %#v, diagnostics = %v; want %v", missing, diagnostics, span)
			}
			if statement.Span().End != offset || statement.X.Span().End != offset {
				t.Fatalf("selector does not contain its missing identifier: %v / %v", statement.Span(), statement.X.Span())
			}
			if broken.Span().End != offset+1 || healthy.Span().Start != uint32(strings.Index(source, "func healthy")) {
				t.Fatalf("function boundaries changed: %v / %v", broken.Span(), healthy.Span())
			}
		})
	}
}

func TestRecoveryRetainsFunctionWithoutClosingBrace(t *testing.T) {
	for _, suffix := range []string{"", "func healthy() {}\n"} {
		t.Run(fmt.Sprintf("following_function_%t", suffix != ""), func(t *testing.T) {
			const prefix = "package p\nfunc broken[T any](value T) T {\nreturn value\n"
			source := prefix + suffix
			file, diagnostics := parser.ParseFile("block.gox", []byte(source))
			if len(errorsOnly(diagnostics)) != 1 {
				t.Fatalf("want one missing brace error, got %v", diagnostics)
			}
			broken, ok := file.Items[0].(*ast.FuncDecl)
			if !ok {
				t.Fatalf("lost function signature: %T", file.Items[0])
			}
			if broken.Name.Name != "broken" || broken.Generics.Params[0].Names[0].Name != "T" ||
				broken.Params[0].Names[0].Name != "value" || len(broken.Results) != 1 {
				t.Fatalf("function signature changed: %#v", broken)
			}
			if _, ok := broken.Body.List[0].(*ast.ReturnStmt); !ok {
				t.Fatalf("lost valid body statement: %T", broken.Body.List[0])
			}
			last := broken.Body.List[len(broken.Body.List)-1]
			span := ast.Span{Start: uint32(len(prefix)), End: uint32(len(prefix))}
			if _, ok := last.(*ast.BadStmt); !ok || last.Span() != span || diagnostics[0].Span != span {
				t.Fatalf("closing brace recovery = %T %v, diagnostics = %v; want %v", last, last.Span(), diagnostics, span)
			}
			if broken.Span().End != span.End || broken.Body.Span().End != span.End {
				t.Fatalf("function does not contain its missing brace: %v / %v", broken.Span(), broken.Body.Span())
			}
			if suffix != "" {
				if len(file.Items) != 2 {
					t.Fatalf("lost following declaration: %d items", len(file.Items))
				}
				if healthy, ok := file.Items[1].(*ast.FuncDecl); !ok || healthy.Name.Name != "healthy" {
					t.Fatalf("lost healthy function: %T", file.Items[1])
				}
			}
		})
	}
}

func TestRecoveryDistinguishesBlankIdentifier(t *testing.T) {
	expression, diagnostics := parser.ParseExpr([]byte("value._"))
	if len(errorsOnly(diagnostics)) != 0 {
		t.Fatal(diagnostics)
	}
	identifier := expression.(*ast.SelectorExpr).Sel
	if identifier.Missing || identifier.Name != "_" || identifier.Span() != (ast.Span{Start: 6, End: 7}) {
		t.Fatalf("blank identifier became recovery: %#v", identifier)
	}
}

func TestRecoveryDoesNotSplitValidBlockSyntax(t *testing.T) {
	const text = "package p\nfunc healthy() { callback := func() {}; callback(); macro_rules!() }\n"
	file, diagnostics := parser.ParseFile("complete.gox", []byte(text))
	if len(errorsOnly(diagnostics)) != 0 || len(file.Items) != 1 {
		t.Fatalf("valid block was treated as a declaration boundary: %v / %d", diagnostics, len(file.Items))
	}
	function := file.Items[0].(*ast.FuncDecl)
	if len(function.Body.List) != 3 {
		t.Fatalf("valid statements were lost: %#v", function.Body.List)
	}
	if _, ok := function.Body.List[2].(*ast.ExprStmt).X.(*ast.MacroExpr); !ok {
		t.Fatal("contextual macro name lost its invocation")
	}
}

func TestRecoveryFragmentsKeepMissingNames(t *testing.T) {
	for _, text := range []string{"value.", "Value::"} {
		expression, diagnostics := parser.ParseExpr([]byte(text))
		if len(errorsOnly(diagnostics)) != 1 {
			t.Fatalf("%q: want one missing name error, got %v", text, diagnostics)
		}
		var missing *ast.Ident
		ast.Walk(expression, func(node ast.Node) bool {
			if name, ok := node.(*ast.Ident); ok && name.Missing {
				missing = name
			}
			return true
		})
		span := ast.Span{Start: uint32(len(text)), End: uint32(len(text))}
		if missing == nil || missing.Name != "" || missing.Span() != span || diagnostics[0].Span != span {
			t.Fatalf("%q: missing name or EOF location lost: %#v / %v", text, missing, diagnostics)
		}
	}
}

func TestRecoveryDiagnosticsMapAcrossCRLF(t *testing.T) {
	const text = "package p\r\nfunc broken() { call(value\r\n}\r\nfunc healthy() {}\r\n"
	_, diagnostics := parser.ParseFile("crlf.gox", []byte(text))
	index := source.New(text)
	want := uint32(strings.Index(text, "value\r\n") + len("value"))
	found := false
	for _, diagnostic := range diagnostics {
		span := diagnostic.Span
		if _, err := index.Range(int(span.Start), int(span.End), source.UTF16); err != nil {
			t.Fatalf("diagnostic %v has an unrepresentable source range: %v", diagnostic, err)
		}
		if strings.HasPrefix(diagnostic.Message, "expected )") && span == (ast.Span{Start: want, End: want}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing closing parenthesis must point before CRLF: %v", diagnostics)
	}
}

func TestRecoveryRetainsIncompleteBodyAndSibling(t *testing.T) {
	for _, body := range []string{"call(", "items[", "var value", "missing!", "missing!("} {
		t.Run(body, func(t *testing.T) {
			source := "package p\nfunc broken(value int) int { " + body + "\n}\nfunc healthy() {}\n"
			file, diagnostics := parser.ParseFile("body.gox", []byte(source))
			if len(errorsOnly(diagnostics)) == 0 {
				t.Fatal("incomplete body has no syntax error")
			}
			if len(file.Items) != 2 {
				t.Fatalf("want both functions, got %d items", len(file.Items))
			}
			broken, ok := file.Items[0].(*ast.FuncDecl)
			if !ok || broken.Name.Name != "broken" || broken.Params[0].Names[0].Name != "value" || len(broken.Results) != 1 {
				t.Fatalf("lost function signature: %#v", file.Items[0])
			}
			if healthy, ok := file.Items[1].(*ast.FuncDecl); !ok || healthy.Name.Name != "healthy" {
				t.Fatalf("lost healthy function: %#v", file.Items[1])
			}
			if strings.HasPrefix(body, "missing!") {
				statement, ok := broken.Body.List[0].(*ast.ExprStmt)
				if !ok {
					t.Fatalf("lost macro invocation: %T", broken.Body.List[0])
				}
				macro, ok := statement.X.(*ast.MacroExpr)
				if !ok || macro.Path.Segments[0].Name.Name != "missing" || !macro.Args.Missing {
					t.Fatalf("lost missing macro arguments: %#v", statement.X)
				}
			}
		})
	}
}

func TestRecoveryRetainsIncompleteGenericDeclaration(t *testing.T) {
	const source = "package p\nfunc broken[T any\nfunc healthy() {}\n"
	file, diagnostics := parser.ParseFile("generic.gox", []byte(source))
	if len(errorsOnly(diagnostics)) == 0 || len(file.Items) != 2 {
		t.Fatalf("incomplete generics lost declarations or diagnostics: %d / %v", len(file.Items), diagnostics)
	}
	broken, ok := file.Items[0].(*ast.FuncDecl)
	if !ok || broken.Name.Name != "broken" || broken.Generics.Params[0].Names[0].Name != "T" {
		t.Fatalf("lost generic declaration: %#v", file.Items[0])
	}
	if healthy, ok := file.Items[1].(*ast.FuncDecl); !ok || healthy.Name.Name != "healthy" {
		t.Fatalf("lost healthy function: %#v", file.Items[1])
	}
}

func BenchmarkParseFile(b *testing.B) {
	var source strings.Builder
	source.WriteString("package p\n")
	for i := range 100 {
		fmt.Fprintf(&source, "func function%d(x int) int { y := x + 42; if y > 0 { return y }; return 0 }\n", i)
	}
	text := []byte(source.String())
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	for b.Loop() {
		parser.ParseFile("benchmark.gox", text)
	}
}
