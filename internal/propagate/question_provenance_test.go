package propagate

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/sourcemap"
)

// Exercise the three consumers of the common question flow directly. Choosing
// a private-place route is the assignment planner's separate responsibility.
func TestQuestionFlowProvenanceConsumers(t *testing.T) {
	for _, consumer := range []string{"expr", "effect", "frame"} {
		t.Run(consumer, func(t *testing.T) {
			source := []byte(`package p
 type Option interface{ option() }
 type Some struct{ Value int };func(Some) option(){}
 type None struct{};func(None) option(){}
 func none() Option{return None{}}
 func validate(Option){}
 func operand() Option{return Some{1}}
 func question(Option) int{return 0}
 type guardType struct{};func(guardType) Fail(Option){}
 func f() Option {guard:=guardType{};_=guard;_=question(operand());return none()}
`)
			fs := token.NewFileSet()
			file, err := parser.ParseFile(fs, "flow.go", source, 0)
			if err != nil {
				t.Fatal(err)
			}
			declaration := enums.Decl{Name: "Option", Family: "Option", TypeArgs: []string{"int"}, Validator: "validate", Variants: []enums.Variant{{CaseName: "Some"}, {CaseName: "None", Constructor: "none"}}}
			m := sourcemap.Identity("flow.go", source)
			questionPos := strings.LastIndex(string(source), "question(")
			operandPos := strings.LastIndex(string(source), "operand(")
			questionStack := []sourcemap.Expansion{{Macro: "question_owner", Definition: sourcemap.Span{File: "definition.gox", Start: 1, End: 10, Line: 1, Column: 2}, Callsite: sourcemap.Span{File: "caller.gox", Start: 30, End: 40, Line: 2, Column: 1}}}
			operandStack := []sourcemap.Expansion{{Macro: "operand_owner", Definition: sourcemap.Span{File: "other.gox", Start: 2, End: 12, Line: 1, Column: 3}, Callsite: sourcemap.Span{File: "caller.gox", Start: 50, End: 60, Line: 3, Column: 1}}}
			operandOrigin := sourcemap.Position("flow.go", source, operandPos, operandPos+len("operand"))
			m.Attach(questionPos, questionPos+len("question"), nil, questionStack, "generated/unmapped")
			m.Attach(operandPos, operandPos+len("operand"), &operandOrigin, operandStack, "source")
			l := &lowering{fs: fs, file: file, cfg: Config{Families: []enums.Decl{declaration}}, questions: map[string]enums.Decl{"question": declaration}, conversions: map[string]string{}, evidence: m.CaptureAST(fs, file)}
			l.check()
			l.captureExpressionFacts()
			function := file.Decls[len(file.Decls)-1].(*ast.FuncDecl)
			assignment := function.Body.List[2].(*ast.AssignStmt)
			input := assignment.Rhs[0]
			var pre []ast.Stmt
			var value ast.Expr
			switch consumer {
			case "expr":
				pre, value, err = l.expr(input, function.Type)
			case "effect":
				pre, value, err = l.effectExpr(input, function.Type)
			case "frame":
				value, err = l.frameExpr(input, function.Type, "guard")
			}
			if err != nil {
				t.Fatal(err)
			}
			assignment.Rhs[0] = value
			body := append([]ast.Stmt(nil), function.Body.List[:2]...)
			body = append(body, pre...)
			body = append(body, function.Body.List[2:]...)
			function.Body.List = body
			var printed bytes.Buffer
			if err := format.Node(&printed, fs, file); err != nil {
				t.Fatal(err)
			}
			mapped := l.evidence.Print(file, printed.Bytes(), "test.question")
			payloads, originalOperands := 0, 0
			for _, segment := range mapped.Segments {
				if segment.Origin != nil && *segment.Origin == operandOrigin {
					originalOperands++
					if len(segment.Stack) != 1 || segment.Stack[0].Macro != "operand_owner" {
						t.Fatalf("operand relabeled: %+v", segment)
					}
				}
				if strings.HasPrefix(string(mapped.Source[segment.Start:segment.End]), "__goxide_qtmp_") && len(segment.Stack) > 0 {
					payloads++
					if segment.Origin != nil || segment.Kind != "generated/unmapped" || len(segment.Stack) != 1 || segment.Stack[0].Macro != "question_owner" {
						t.Fatalf("generated payload has wrong ownership: %+v", segment)
					}
				}
			}
			if payloads < 3 || originalOperands != 1 {
				t.Fatalf("lost flow/operand identity: payloads=%d operands=%d\n%s", payloads, originalOperands, printed.Bytes())
			}
		})
	}
}
