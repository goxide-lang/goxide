package lower

import (
	goast "go/ast"
	gotoken "go/token"
	"go/types"
	"strconv"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func (e *emitter) stableArrayOperand(node syntax.Expr) (*constArray, bool) {
	typ := e.info.Types[node]
	if typ == nil {
		return nil, false
	}
	if pointer, ok := typ.Underlying().(*types.Pointer); ok {
		return e.stableArray(pointer.Elem()), true
	}
	return e.stableArray(typ), false
}

func (e *emitter) stableArraySlice(array *constArray, pointer goast.Expr) goast.Expr {
	return &goast.CallExpr{Fun: e.callbackReference(goast.NewIdent(array.sliceName)), Args: []goast.Expr{pointer}}
}

func (e *emitter) stableArrayAddress(node syntax.Expr) goast.Expr {
	for {
		paren, ok := node.(*syntax.ParenExpr)
		if !ok {
			break
		}
		node = paren.X
	}
	if literal, ok := node.(*syntax.CompositeLit); ok {
		return e.stableArrayLiteral(literal, true)
	}
	return nil
}

// Returning an element pointer preserves addressability, while function arguments
// evaluate the index before the projection can perform a nil or bounds check.
func (e *emitter) stableArrayIndex(node *syntax.IndexExpr) goast.Expr {
	array, pointer := e.stableArrayOperand(node.X)
	if array == nil || len(node.Indices) != 1 {
		return nil
	}
	base, index := e.fresh("Array"), e.fresh("Index")
	baseType := goast.Expr(goast.NewIdent(array.typeName))
	value := e.expr(node.X)
	if pointer || e.addressable(node.X) {
		baseType = &goast.StarExpr{X: baseType}
		if !pointer {
			value = &goast.UnaryExpr{Op: gotoken.AND, X: value}
		}
		pointer = true
	}
	var address goast.Expr = base
	if !pointer {
		address = &goast.UnaryExpr{Op: gotoken.AND, X: base}
	}
	indexType := e.goType(e.info.Types[node.Indices[0]])
	resultType := &goast.StarExpr{X: e.goType(array.elem)}
	function := &goast.FuncLit{Type: &goast.FuncType{
		Params:  &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{base}, Type: baseType}, {Names: []*goast.Ident{index}, Type: indexType}}},
		Results: &goast.FieldList{List: []*goast.Field{{Type: resultType}}},
	}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{
		&goast.UnaryExpr{Op: gotoken.AND, X: &goast.IndexExpr{X: e.stableArraySlice(array, address), Index: index}},
	}}}}}
	return &goast.StarExpr{X: &goast.CallExpr{Fun: function, Args: []goast.Expr{value, e.expr(node.Indices[0])}}}
}

func (e *emitter) stableArraySlicing(node *syntax.SliceExpr) goast.Expr {
	array, pointer := e.stableArrayOperand(node.X)
	if array == nil {
		return nil
	}
	base := e.fresh("Array")
	baseType := &goast.StarExpr{X: goast.NewIdent(array.typeName)}
	value := e.expr(node.X)
	if !pointer {
		value = &goast.UnaryExpr{Op: gotoken.AND, X: value}
	}
	parameters := []*goast.Field{{Names: []*goast.Ident{base}, Type: baseType}}
	arguments := []goast.Expr{value}
	bounds := make([]goast.Expr, 3)
	for i, bound := range []syntax.Expr{node.Low, node.High, node.Max} {
		if bound != nil {
			name := e.fresh("Bound")
			parameters = append(parameters, &goast.Field{Names: []*goast.Ident{name}, Type: e.goType(e.info.Types[bound])})
			arguments = append(arguments, e.expr(bound))
			bounds[i] = name
		}
	}
	slice := &goast.SliceExpr{X: e.stableArraySlice(array, base), Low: bounds[0], High: bounds[1], Max: bounds[2], Slice3: node.Slice3}
	return &goast.CallExpr{Fun: &goast.FuncLit{Type: &goast.FuncType{Params: &goast.FieldList{List: parameters}, Results: &goast.FieldList{List: []*goast.Field{{Type: &goast.ArrayType{Elt: e.goType(array.elem)}}}}}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{slice}}}}}, Args: arguments}
}

func (e *emitter) stableArrayLiteral(node *syntax.CompositeLit, pointer bool) goast.Expr {
	typ := e.info.Types[node]
	if p, ok := typ.(*types.Pointer); ok {
		typ, pointer = p.Elem(), true
	}
	array := e.stableArray(typ)
	if array == nil {
		return nil
	}
	value := e.fresh("Array")
	arrayType := goast.NewIdent(array.typeName)
	body := []goast.Stmt{varStmt(value, arrayType, nil)}
	var next goast.Expr = integer(0)
	for _, element := range node.Elts {
		index, source := next, element
		if keyed, ok := element.(*syntax.KeyValue); ok {
			index, source = e.expr(keyed.Key), keyed.Value
		}
		target := &goast.IndexExpr{X: e.stableArraySlice(array, &goast.UnaryExpr{Op: gotoken.AND, X: value}), Index: index}
		body = append(body, &goast.AssignStmt{Lhs: []goast.Expr{target}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{e.expr(source)}})
		next = &goast.BinaryExpr{X: index, Op: gotoken.ADD, Y: integer(1)}
	}
	var result, resultType goast.Expr = value, arrayType
	if pointer {
		result = &goast.UnaryExpr{Op: gotoken.AND, X: value}
		resultType = &goast.StarExpr{X: arrayType}
	}
	body = append(body, &goast.ReturnStmt{Results: []goast.Expr{result}})
	return &goast.CallExpr{Fun: &goast.FuncLit{Type: &goast.FuncType{Params: &goast.FieldList{}, Results: &goast.FieldList{List: []*goast.Field{{Type: resultType}}}}, Body: &goast.BlockStmt{List: body}}}
}

func (e *emitter) stableArrayShape(node syntax.Expr) (*check.ArrayPattern, bool) {
	if e.constTemplate == nil {
		return nil, false
	}
	typ := e.info.Types[node]
	if typ == nil {
		return nil, false
	}
	pointer, ok := typ.Underlying().(*types.Pointer)
	if ok {
		typ = pointer.Elem()
	}
	array, _ := e.typeState.ArrayOf(e.info, typ)
	return array, ok
}

func (e *emitter) stableArrayLength(array *check.ArrayPattern) goast.Expr {
	if array.Length.Param != nil {
		return e.callbackReference(goast.NewIdent(e.constTemplate.lengths[array.Length.Param]))
	}
	return &goast.BasicLit{Kind: gotoken.INT, Value: strconv.FormatInt(array.Length.Value, 10)}
}

func (e *emitter) stableLengthCall(call *syntax.CallExpr) goast.Expr {
	if len(call.Args) != 1 {
		return nil
	}
	symbol := e.valueSymbol(call.Fun)
	if symbol == nil || symbol.Kind != resolve.Builtin || len(symbol.Target) != 0 || symbol.Name.Name != "len" && symbol.Name.Name != "cap" {
		return nil
	}
	array, _ := e.stableArrayShape(call.Args[0])
	if array == nil {
		return nil
	}
	length := e.stableArrayLength(array)
	if !e.stableArrayEvaluated(call.Args[0]) {
		e.retainStableArrayBindings(call.Args[0])
		return length
	}
	return &goast.CallExpr{Fun: &goast.FuncLit{Type: &goast.FuncType{
		Params:  &goast.FieldList{List: []*goast.Field{{Names: []*goast.Ident{goast.NewIdent("_")}, Type: e.goType(e.info.Types[call.Args[0]])}}},
		Results: &goast.FieldList{List: []*goast.Field{{Type: e.primitiveAlias("int")}}},
	}, Body: &goast.BlockStmt{List: []goast.Stmt{&goast.ReturnStmt{Results: []goast.Expr{length}}}}}, Args: []goast.Expr{e.expr(call.Args[0])}}
}

// A constant len/cap or one-variable range still counts as a source use of its
// locals. Keep those bindings live without evaluating the skipped operand.
func (e *emitter) retainStableArrayBindings(node syntax.Expr) {
	defined := map[*resolve.Symbol]bool{}
	syntax.Walk(node, func(node syntax.Node) bool {
		if id, ok := node.(*syntax.Ident); ok {
			if symbol := e.file.Definitions[id.ID()]; symbol != nil {
				defined[symbol] = true
			}
		}
		return true
	})
	seen := map[*resolve.Symbol]bool{}
	syntax.Walk(node, func(node syntax.Node) bool {
		id, ok := node.(*syntax.Ident)
		if !ok {
			return true
		}
		symbol := e.file.References[id.ID()].Symbol
		if symbol != nil && symbol.Kind == resolve.Variable && !defined[symbol] && !seen[symbol] {
			seen[symbol] = true
			e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{goast.NewIdent("_")}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{&goast.UnaryExpr{Op: gotoken.AND, X: e.expr(id)}}})
		}
		return true
	})
}

func (e *emitter) stableArrayEvaluated(node syntax.Expr) bool {
	found := false
	syntax.Walk(node, func(node syntax.Node) bool {
		if found {
			return false
		}
		switch node := node.(type) {
		case *syntax.FuncLit:
			return false
		case *syntax.UnaryExpr:
			found = node.Op == token.ARROW
		case *syntax.TryExpr, *syntax.MatchExpr:
			found = true
		case *syntax.CallExpr:
			if _, conversion := node.Fun.(*syntax.TypeExpr); conversion {
				return true
			}
			symbol := e.valueSymbol(node.Fun)
			if symbol != nil && symbol.Kind == resolve.Type {
				return true
			}
			if e.stableConstantCall(node) {
				return false
			}
			found = true
		}
		return !found
	})
	return found
}

func (e *emitter) stableConstantCall(call *syntax.CallExpr) bool {
	symbol := e.valueSymbol(call.Fun)
	if symbol == nil || symbol.Kind != resolve.Builtin {
		return false
	}
	if len(symbol.Target) == 2 && symbol.Target[0] == "unsafe" && len(call.Args) == 1 {
		return (symbol.Target[1] == "Sizeof" || symbol.Target[1] == "Alignof") && check.FixedLayout(e.info.Types[call.Args[0]])
	}
	if len(symbol.Target) != 0 {
		return false
	}
	switch symbol.Name.Name {
	case "len", "cap":
		if len(call.Args) != 1 {
			return false
		}
		if array, _ := e.stableArrayShape(call.Args[0]); array != nil {
			return !e.stableArrayEvaluated(call.Args[0])
		}
		return symbol.Name.Name == "len" && e.constant(call.Args[0])
	case "complex", "real", "imag", "min", "max":
		for _, argument := range call.Args {
			if nested, ok := argument.(*syntax.CallExpr); ok {
				if !e.stableConstantCall(nested) {
					return false
				}
			} else if !e.constant(argument) {
				return false
			}
		}
		return true
	}
	return false
}

func (e *emitter) stableArrayRange(node *syntax.RangeStmt) goast.Stmt {
	array, pointer := e.stableArrayShape(node.X)
	if array == nil {
		return nil
	}
	valueUsed := node.Value != nil
	if id, ok := node.Value.(*syntax.Ident); ok && id.Name == "_" {
		valueUsed = false
	}
	var operand goast.Expr
	if valueUsed {
		operand = e.fresh("Array")
		value := e.expr(node.X)
		e.pending = append(e.pending, varStmt(operand.(*goast.Ident), e.goType(e.info.Types[node.X]), value))
	} else if e.stableArrayEvaluated(node.X) {
		value := e.expr(node.X)
		e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{goast.NewIdent("_")}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{value}})
	} else {
		e.retainStableArrayBindings(node.X)
	}
	guard := e.assignmentGuard
	e.assignmentGuard = nil
	body := e.block(node.Body)
	e.assignmentGuard = guard
	statement := &goast.RangeStmt{For: e.pos(node), Key: e.expr(node.Key), Tok: tokenKind(node.Tok), X: e.stableArrayLength(array), Body: body}
	if !valueUsed {
		if statement.Key == nil {
			statement.Tok = gotoken.ILLEGAL
		}
		return statement
	}
	index := e.fresh("Index")
	statement.Key, statement.Tok = index, gotoken.DEFINE
	var element goast.Expr
	if stable, _ := e.stableArrayOperand(node.X); stable != nil {
		address := operand
		if !pointer {
			address = &goast.UnaryExpr{Op: gotoken.AND, X: operand}
		}
		element = &goast.IndexExpr{X: e.stableArraySlice(stable, address), Index: index}
	} else {
		element = &goast.IndexExpr{X: operand, Index: index}
	}
	if node.Tok == token.ASSIGN {
		pending := e.pending
		e.pending = nil
		left := []goast.Expr{e.lvalue(node.Key), e.lvalue(node.Value)}
		before := e.pending
		e.pending = pending
		value := e.fresh("Element")
		before = append(before, varStmt(value, e.goType(array.Elem), element))
		for i, right := range []goast.Expr{index, value} {
			before = append(before, &goast.AssignStmt{Lhs: []goast.Expr{left[i]}, Tok: gotoken.ASSIGN, Rhs: []goast.Expr{right}})
		}
		body.List = append(before, body.List...)
	} else {
		assignment := &goast.AssignStmt{Lhs: []goast.Expr{e.expr(node.Key), e.expr(node.Value)}, Tok: tokenKind(node.Tok), Rhs: []goast.Expr{index, element}}
		body.List = append([]goast.Stmt{assignment}, body.List...)
	}
	return statement
}

func (e *emitter) stableArrayTarget(node syntax.Expr) bool {
	if e.constTemplate == nil {
		return false
	}
	found := false
	syntax.Walk(node, func(node syntax.Node) bool {
		if index, ok := node.(*syntax.IndexExpr); ok {
			array, _ := e.stableArrayOperand(index.X)
			found = found || array != nil
		}
		return !found
	})
	return found
}

func (e *emitter) stableArrayStatement(node syntax.Stmt) bool {
	switch node := node.(type) {
	case *syntax.AssignStmt:
		for _, target := range node.Lhs {
			if e.stableArrayTarget(target) {
				return true
			}
		}
	case *syntax.IncDecStmt:
		return e.stableArrayTarget(node.X)
	}
	return false
}

func (e *emitter) stableArrayAssignment(node *syntax.AssignStmt) goast.Stmt {
	if node.Tok == token.DEFINE {
		return nil
	}
	needed := false
	for _, target := range node.Lhs {
		needed = needed || e.stableArrayTarget(target)
	}
	if !needed {
		return nil
	}
	left := make([]goast.Expr, len(node.Lhs))
	values := make([]goast.Expr, len(node.Lhs))
	for index, target := range node.Lhs {
		left[index] = e.lvalue(target)
	}
	right := e.exprs(node.Rhs)
	for index, target := range node.Lhs {
		if id, ok := target.(*syntax.Ident); ok && id.Name == "_" {
			values[index] = goast.NewIdent("_")
			continue
		}
		name := e.fresh("Value")
		typ := e.info.Types[target]
		if node.Tok == token.SHL_ASSIGN || node.Tok == token.SHR_ASSIGN {
			typ = e.info.Types[node.Rhs[index]]
		}
		if typ == nil && len(node.Rhs) == len(node.Lhs) {
			typ = e.info.Types[node.Rhs[index]]
		}
		values[index] = name
		e.pending = append(e.pending, varStmt(name, e.goType(typ), nil))
	}
	e.pending = append(e.pending, &goast.AssignStmt{Lhs: values, Tok: gotoken.ASSIGN, Rhs: right})
	for index, target := range left {
		if id, ok := target.(*goast.Ident); ok && id.Name == "_" {
			continue
		}
		e.pending = append(e.pending, &goast.AssignStmt{Lhs: []goast.Expr{target}, Tok: tokenKind(node.Tok), Rhs: []goast.Expr{values[index]}})
	}
	return &goast.EmptyStmt{Implicit: true}
}
