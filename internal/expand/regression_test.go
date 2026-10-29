package expand

import (
	"errors"
	"fmt"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestExpandNestedCallerUsesLexicalInvocationScope(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprintf("explicit_callee_%t", explicit), func(t *testing.T) {
			localSource := `macro_rules! helper { () => { 1 }; }
var value = ext::outer!()`
			externalSource := `macro_rules! helper { () => { 2 }; }
macro_rules! inner { () => { $caller::helper!() }; }
macro_rules! outer { () => { inner!() }; }`
			if explicit {
				localSource = `macro_rules! inner { () => { $caller::helper!() }; }
` + localSource
				externalSource = `macro_rules! helper { () => { 2 }; }
macro_rules! outer { () => { $caller::inner!() }; }`
			}
			local := expandFile(t, "local.gox", localSource)
			external := expandFile(t, "external.gox", externalSource)
			result, err := Expand([]*ast.File{local}, regressionSourceResolver(external))
			if err != nil {
				t.Fatal(err)
			}
			if got := expandValue(t, result.Files[0], "value").(*ast.BasicLit).Value; got != "2" {
				t.Fatalf("inner caller resolved in the outermost package: %s", got)
			}
			inner := result.Expansions[2]
			context := result.Contexts[inner.Ctx]
			if context.Caller.File != external || context.Caller.Ctx != result.Expansions[1].Ctx {
				t.Fatalf("inner lexical caller = %#v", context.Caller)
			}
			wantDefinition := external
			if explicit {
				wantDefinition = local
			}
			if inner.Definition.File != wantDefinition {
				t.Fatal("callee lookup scope was confused with invocation scope")
			}
		})
	}
}

func TestExpandGoDeferRequireCallAfterSubstitution(t *testing.T) {
	for _, keyword := range []string{"go", "defer"} {
		t.Run(keyword, func(t *testing.T) {
			for _, input := range []string{"work()", "nested!()", "1"} {
				source := fmt.Sprintf(`macro_rules! nested { () => { work() }; }
macro_rules! launch { ($x:expr) => { %s $x }; }
func run() { launch!(%s) }`, keyword, input)
				file := expandFile(t, "calls.gox", source)
				result, err := Expand([]*ast.File{file}, nil)
				if input == "1" {
					var diagnostic *Diagnostic
					if !errors.As(err, &diagnostic) || diagnostic.Code != "category" {
						t.Fatalf("non-call %s expression: %v", keyword, err)
					}
					if diagnostic.Definition.File != file || diagnostic.Call.File != file || diagnostic.Expn == 0 {
						t.Fatalf("non-call diagnostic lost expansion origins: %#v", diagnostic)
					}
					continue
				}
				if err != nil {
					t.Fatalf("%s %s: %v", keyword, input, err)
				}
				stmt := result.Files[0].Items[0].(*ast.FuncDecl).Body.List[0]
				var call ast.Expr
				switch n := stmt.(type) {
				case *ast.GoStmt:
					call = n.Call
				case *ast.DeferStmt:
					call = n.Call
				default:
					t.Fatalf("unexpected statement %T", stmt)
				}
				if _, ok := call.(*ast.CallExpr); !ok {
					t.Fatalf("%s retained a non-call %T", keyword, call)
				}
			}
		})
	}
}

func TestExpandGeneratedItemKeepsCallerAndTemplateKeys(t *testing.T) {
	for _, nested := range []bool{false, true} {
		t.Run(fmt.Sprintf("nested_%t", nested), func(t *testing.T) {
			externalSource := `macro_rules! make { () => { func Created() int { return Created() } }; }`
			invocation := "make"
			if nested {
				externalSource += "\nmacro_rules! outer { () => { make!() }; }"
				invocation = "outer"
			}
			local := expandFile(t, "local.gox", "ext::"+invocation+"!()")
			call := local.Items[0].(*ast.MacroItem)
			for _, segment := range call.Path.Segments {
				segment.Name.Ctx = 41
			}
			external := expandFile(t, "external.gox", externalSource)
			result, err := Expand([]*ast.File{local}, regressionSourceResolver(external))
			if err != nil {
				t.Fatal(err)
			}
			decl := result.Files[0].Items[0].(*ast.FuncDecl)
			reference := decl.Body.List[0].(*ast.ReturnStmt).Results[0].(*ast.CallExpr).Fun.(*ast.Ident)
			if decl.Name.Ctx != 41 || result.Scopes[decl.Name].File != local {
				t.Fatalf("generated declaration lost the source caller identity: %#v", decl.Name)
			}
			alias, ok := result.Introductions[decl.Name]
			if !ok || alias != (Key{reference.Name, reference.Ctx}) || alias.Ctx <= 41 {
				t.Fatalf("recursive reference has no matching generated binding: alias=%#v reference=%#v", alias, reference)
			}
			if result.Scopes[reference].File != external {
				t.Fatal("generated binding changed the default scope of template references")
			}
		})
	}
}

func regressionSourceResolver(external *ast.File) Resolver {
	return expandResolverFunc(func(path *ast.Path, scope Scope) (*ast.MacroRulesDecl, *ast.File, error) {
		file := scope.File
		if len(path.Segments) > 1 {
			file = external
		}
		name := path.Segments[len(path.Segments)-1].Name.Name
		for _, item := range file.Items {
			if def, ok := item.(*ast.MacroRulesDecl); ok && def.Name.Name == name {
				return def, file, nil
			}
		}
		return nil, nil, fmt.Errorf("macro %s has no source definition", name)
	})
}
