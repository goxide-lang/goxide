package lower

import (
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strconv"
	"strings"

	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type printPart struct {
	literal     string
	slot, debug bool
}

func printParts(format string) ([]printPart, error) {
	var parts []printPart
	var text strings.Builder
	flush := func() {
		if text.Len() != 0 {
			parts = append(parts, printPart{literal: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(format); {
		switch {
		case strings.HasPrefix(format[i:], "{{"), strings.HasPrefix(format[i:], "}}"):
			text.WriteByte(format[i])
			i += 2
		case strings.HasPrefix(format[i:], "{}"):
			flush()
			parts = append(parts, printPart{slot: true})
			i += 2
		case strings.HasPrefix(format[i:], "{:?}"):
			flush()
			parts = append(parts, printPart{slot: true, debug: true})
			i += 4
		case format[i] == '{' || format[i] == '}':
			return nil, fmt.Errorf("unsupported or unmatched format brace")
		default:
			text.WriteByte(format[i])
			i++
		}
	}
	flush()
	return parts, nil
}

func (e *emitter) validatePrintStatements() {
	allowed := map[*syntax.CallExpr]bool{}
	add := func(statements []syntax.Stmt) {
		for _, statement := range statements {
			if expression, ok := statement.(*syntax.ExprStmt); ok {
				if call, ok := expression.X.(*syntax.CallExpr); ok {
					allowed[call] = true
				}
			}
		}
	}
	syntax.Walk(e.tree, func(n syntax.Node) bool {
		switch n := n.(type) {
		case *syntax.Block:
			add(n.List)
		case *syntax.CaseClause:
			add(n.Body)
		case *syntax.CommClause:
			add(n.Body)
		}
		return true
	})
	syntax.Walk(e.tree, func(n syntax.Node) bool {
		if call, ok := n.(*syntax.CallExpr); ok {
			if _, print := e.result.Prints[call]; print && !allowed[call] {
				e.fail(call, "print macros require a direct statement, not an initializer or loop post")
			}
		}
		return true
	})
}

func (e *emitter) printStatement(n syntax.Stmt) ([]goast.Stmt, bool) {
	statement, ok := n.(*syntax.ExprStmt)
	if !ok {
		return nil, false
	}
	call, ok := statement.X.(*syntax.CallExpr)
	if !ok {
		return nil, false
	}
	newline, ok := e.result.Prints[call]
	if !ok {
		return nil, false
	}
	format, arguments := "", call.Args
	if len(arguments) == 0 {
		if !newline {
			e.fail(call, "print! requires a format string literal")
			return nil, true
		}
	} else {
		literal, ok := arguments[0].(*syntax.BasicLit)
		if !ok || literal.Kind != token.STRING {
			e.fail(arguments[0], "print format must be a string literal")
			return nil, true
		}
		var err error
		format, err = strconv.Unquote(literal.Value)
		if err != nil {
			e.fail(literal, err.Error())
			return nil, true
		}
		arguments = arguments[1:]
	}
	parts, err := printParts(format)
	if err != nil {
		e.fail(call, err.Error())
		return nil, true
	}
	slots := 0
	for _, part := range parts {
		if part.slot {
			slots++
		}
	}
	if slots != len(arguments) || call.Ellipsis {
		e.fail(call, fmt.Sprintf("format has %d placeholders but %d arguments", slots, len(arguments)))
		return nil, true
	}
	block := &goast.BlockStmt{}
	values := make([]*goast.Ident, len(arguments))
	for i, arg := range arguments {
		if typ := e.info.Types[arg]; typ == nil || typ == types.Typ[types.Invalid] {
			e.fail(arg, "cannot resolve formatting argument type")
			return nil, true
		}
		before, value := e.exprScoped(arg)
		block.List = append(block.List, before...)
		values[i] = e.fresh("PrintArg")
		block.List = append(block.List, &goast.DeclStmt{Decl: &goast.GenDecl{Tok: gotoken.VAR, Specs: []goast.Spec{
			&goast.ValueSpec{Names: []*goast.Ident{values[i]}, Type: e.goType(e.info.Types[arg]), Values: []goast.Expr{value}},
		}}})
	}
	var text goast.Expr = &goast.BasicLit{Kind: gotoken.STRING, Value: `""`}
	appendText := func(next goast.Expr) {
		text = &goast.BinaryExpr{X: text, Op: gotoken.ADD, Y: next}
	}
	index := 0
	for _, part := range parts {
		if !part.slot {
			appendText(&goast.BasicLit{Kind: gotoken.STRING, Value: strconv.Quote(part.literal)})
			continue
		}
		value, err := e.printFormat(e.info.Types[arguments[index]], values[index], part.debug)
		if err != nil {
			e.fail(arguments[index], err.Error())
			return nil, true
		}
		appendText(value)
		index++
	}
	if newline {
		appendText(&goast.BasicLit{Kind: gotoken.STRING, Value: `"\n"`})
	}
	block.List = append(block.List, &goast.ExprStmt{X: &goast.CallExpr{Fun: e.formattingMember("Write"), Args: []goast.Expr{text}}})
	return []goast.Stmt{block}, true
}

func (e *emitter) formattingMember(name string) goast.Expr {
	pkg := e.importName([]string{"github.com", "goxide-lang", "std", "formatting"}, "formatting", 0)
	return &goast.SelectorExpr{X: pkg, Sel: goast.NewIdent(name)}
}

func (e *emitter) printFormat(t types.Type, value goast.Expr, debug bool) (goast.Expr, error) {
	if t == nil {
		return nil, fmt.Errorf("cannot resolve formatting argument type")
	}
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		if d := e.deriveFor(named); d != nil && debug && d.Requests["Debug"] && e.samePackage(d.File) {
			return &goast.CallExpr{Fun: e.deriveHelper(d, "Debug"), Args: []goast.Expr{&goast.UnaryExpr{Op: gotoken.AND, X: value}}}, nil
		}
		return nil, fmt.Errorf("type %s has no explicit supported formatting protocol", t)
	}
	base, ok := types.Default(t).(*types.Basic)
	if !ok {
		return nil, fmt.Errorf("type %s has no explicit supported formatting protocol", t)
	}
	name := map[types.BasicKind]string{
		types.Bool: "Bool", types.Int: "Int", types.Int8: "Int8", types.Int16: "Int16", types.Int32: "Int32", types.Int64: "Int64",
		types.Uint: "Uint", types.Uint8: "Uint8", types.Uint16: "Uint16", types.Uint32: "Uint32", types.Uint64: "Uint64", types.Uintptr: "Uintptr",
		types.Float32: "Float32", types.Float64: "Float64", types.Complex64: "Complex64", types.Complex128: "Complex128",
	}[base.Kind()]
	if base.Kind() == types.String {
		name = "DisplayString"
		if debug {
			name = "DebugString"
		}
	}
	if name == "" {
		return nil, fmt.Errorf("type %s has no explicit supported formatting protocol", t)
	}
	return &goast.CallExpr{Fun: e.formattingMember(name), Args: []goast.Expr{value}}, nil
}
