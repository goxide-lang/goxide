package check

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// ConstParam has source identity independently of Go type parameters.
type ConstParam struct {
	Decl   *ast.GenericParam
	Name   *ast.Ident
	Symbol *resolve.Symbol
}

// ConstValue is either a nonnegative integer or an unchanged source parameter.
type ConstValue struct {
	Param *ConstParam
	Value int64
}

type ArrayPattern struct {
	Elem   types.Type
	Length ConstValue
}

// ArrayOf recognizes a source array before consulting its Go representation.
func (i *Info) ArrayOf(t types.Type) (*ArrayPattern, bool) {
	if t == nil {
		return nil, false
	}
	t = types.Unalias(t)
	if p, ok := t.(*types.TypeParam); ok {
		if i == nil {
			return nil, false
		}
		array := i.ArrayPatterns[p]
		return array, array != nil
	}
	if array, ok := t.Underlying().(*types.Array); ok {
		return &ArrayPattern{Elem: array.Elem(), Length: ConstValue{Value: array.Len()}}, true
	}
	return nil, false
}

func (i *Info) arrayShape(t types.Type) (*ArrayPattern, bool) {
	t = types.Unalias(t)
	switch t.(type) {
	case *types.TypeParam, *types.Array:
		return i.ArrayOf(t)
	}
	return nil, false
}

func (i *Info) containsType(t, needle types.Type) bool {
	if t == needle {
		return true
	}
	if array, ok := i.arrayShape(t); ok {
		return i.containsType(array.Elem, needle)
	}
	return typeChildren(t, func(child types.Type) bool { return i.containsType(child, needle) })
}

func (i *Info) containsConst(t types.Type, needle *ConstParam) bool {
	if array, ok := i.arrayShape(t); ok {
		return array.Length.Param == needle || i.containsConst(array.Elem, needle)
	}
	return typeChildren(t, func(child types.Type) bool { return i.containsConst(child, needle) })
}

type implPatternContext struct {
	info       *Info
	parameters map[*ConstParam]bool
	bindings   map[*ConstParam]ConstValue
}

func newImplPatternContext(info *Info, groups ...[]*ConstParam) *implPatternContext {
	context := &implPatternContext{info: info, parameters: map[*ConstParam]bool{}, bindings: map[*ConstParam]ConstValue{}}
	for _, group := range groups {
		for _, parameter := range group {
			context.parameters[parameter] = true
		}
	}
	return context
}

func (c *implPatternContext) bind(pattern, target ConstValue) bool {
	if pattern.Param != nil && c.parameters[pattern.Param] {
		if previous, exists := c.bindings[pattern.Param]; exists {
			return previous == target
		}
		c.bindings[pattern.Param] = target
		return true
	}
	return pattern == target
}

func (c *implPatternContext) unify(left, right ConstValue) bool {
	left, right = resolveConst(left, c.bindings), resolveConst(right, c.bindings)
	if left == right {
		return true
	}
	if left.Param != nil && c.parameters[left.Param] {
		c.bindings[left.Param] = right
		return true
	}
	if right.Param != nil && c.parameters[right.Param] {
		c.bindings[right.Param] = left
		return true
	}
	return false
}

func (c *checker) implTargetsOverlap(left, right *Impl) bool {
	return implTargetsOverlap(left.Target, right.Target, left.TypeParams, right.TypeParams, newImplPatternContext(c.out, left.ConstParams, right.ConstParams))
}

func (c *checker) explicitIndexArguments(sig *types.Signature, node *ast.IndexExpr) types.Type {
	groups := c.out.FunctionGenerics[sig]
	want := 0
	if groups != nil {
		for _, group := range groups.Params {
			want += len(group.Names)
		}
	}
	if len(node.Indices) != want {
		c.diag(node, GenericConstraint, fmt.Sprintf("got %d generic arguments but want %d", len(node.Indices), want))
		return types.Typ[types.Invalid]
	}
	var actual []types.Type
	var constants []ConstValue
	index := 0
	for _, group := range groups.Params {
		for range group.Names {
			argument := node.Indices[index]
			index++
			if group.Const {
				value, ok := c.constExpression(argument)
				if !ok {
					return types.Typ[types.Invalid]
				}
				constants = append(constants, value)
			} else {
				actual = append(actual, c.exprType(argument))
			}
		}
	}
	return c.instantiateFunctionType(sig, actual, constants, node)
}

// ArrayType never uses a fictitious array length. An unresolved array has an
// opaque Go-safe representative; the length remains separate source data.
func (i *Info) ArrayType(elem types.Type, length ConstValue) types.Type {
	if length.Param == nil {
		return types.NewArray(elem, length.Value)
	}
	if i.ArrayPatterns == nil {
		i.ArrayPatterns = map[*types.TypeParam]*ArrayPattern{}
	}
	for rep, array := range i.ArrayPatterns {
		if array.Length == length && types.Identical(array.Elem, elem) {
			return rep
		}
	}
	constraint := types.Type(anyType)
	if types.Comparable(elem) {
		constraint = types.Universe.Lookup("comparable").Type()
	}
	rep := types.NewTypeParam(types.NewTypeName(0, nil, "["+length.Param.Name.Name+"]"+types.TypeString(elem, nil), nil), constraint)
	i.ArrayPatterns[rep] = &ArrayPattern{Elem: elem, Length: length}
	return rep
}

// Substitute resolves source array lengths together with ordinary type values.
func (i *Info) Substitute(t types.Type, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue) types.Type {
	return newBodyTypes(i, sub, constants).typ(t)
}

func resolveConst(value ConstValue, sub map[*ConstParam]ConstValue) ConstValue {
	seen := map[*ConstParam]bool{}
	for value.Param != nil && !seen[value.Param] {
		seen[value.Param] = true
		next, ok := sub[value.Param]
		if !ok || next == value {
			break
		}
		value = next
	}
	return value
}

func openConstants(values []ConstValue) bool {
	for _, value := range values {
		if value.Param != nil {
			return true
		}
	}
	return false
}

func identicalConstants(left, right []ConstValue) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func (c *checker) constParameters(g *ast.GenericParams) []*ConstParam {
	var result []*ConstParam
	if g == nil {
		return result
	}
	if c.out.ConstParams == nil {
		c.out.ConstParams = map[*resolve.Symbol]*ConstParam{}
	}
	for _, group := range g.Params {
		if !group.Const {
			continue
		}
		if group.Constraint == nil || !types.Identical(c.typ(group.Constraint), types.Typ[types.Int]) {
			c.diag(group, GenericConstraint, "const generic parameters require type int")
		}
		for _, name := range group.Names {
			symbol := c.symbol(name)
			param := c.out.ConstParams[symbol]
			if param == nil {
				param = &ConstParam{Decl: group, Name: name, Symbol: symbol}
				c.out.ConstParams[symbol] = param
				c.out.Symbols[symbol] = types.Typ[types.Int]
			}
			result = append(result, param)
		}
	}
	return result
}

func (c *checker) constExpression(node ast.Expr) (ConstValue, bool) {
	if node == nil {
		c.diag(c.file.AST, GenericConstraint, "an integer const argument is required")
		return ConstValue{}, false
	}
	if paren, ok := node.(*ast.ParenExpr); ok {
		return c.constExpression(paren.X)
	}
	if parameter := c.out.ConstParams[c.symbol(node)]; parameter != nil {
		return ConstValue{Param: parameter}, true
	}
	if symbol := c.symbol(node); symbol != nil && symbol.Kind == resolve.Constant && !c.integerConstantType(symbol) {
		c.diag(node, GenericConstraint, "const arguments require an integer constant")
		return ConstValue{}, false
	}

	value := c.integerConstant(node)
	if value.Kind() == constant.Int {
		if number, ok := constant.Int64Val(value); ok && number >= 0 {
			return ConstValue{Value: number}, true
		}
	}
	c.diag(node, GenericConstraint, "array lengths and const arguments require a nonnegative integer constant or an unchanged const parameter")
	return ConstValue{}, false
}

func (c *checker) integerConstantType(symbol *resolve.Symbol) bool {
	typ := c.out.Symbols[symbol]
	if source, exists := c.sourceTypes[symbol]; exists {
		old := c.file
		c.file = source.file
		typ = c.typ(source.node)
		c.file = old
	}
	if typ == nil {
		return true
	}
	basic, ok := types.Unalias(typ).Underlying().(*types.Basic)
	return !ok || basic.Info()&types.IsInteger != 0
}

func (c *checker) integerConstant(node ast.Expr) (result constant.Value) {
	result = constant.MakeUnknown()
	defer func() {
		if recover() != nil || result.Kind() == constant.Int && constant.BitLen(result) > 512 {
			result = constant.MakeUnknown()
		}
	}()
	if symbol := c.symbol(node); symbol != nil && symbol.Kind == resolve.Constant {
		if !c.integerConstantType(symbol) {
			return result
		}
		if value := c.constants[symbol]; value != nil && value.Kind() != constant.Unknown {
			return value
		}
		if source, exists := c.sourceValues[symbol]; exists {
			if c.constantStack == nil {
				c.constantStack = map[*resolve.Symbol]bool{}
			}
			if c.constantStack[symbol] {
				return result
			}
			c.constantStack[symbol] = true
			old := c.file
			c.file = source.file
			defer func() { c.file = old; delete(c.constantStack, symbol) }()
			return c.integerConstant(source.node)
		}
	}
	if value := c.importedIntegerConstant(node); value != nil {
		return value
	}
	if paren, ok := node.(*ast.ParenExpr); ok {
		return c.integerConstant(paren.X)
	}
	if unary, ok := node.(*ast.UnaryExpr); ok {
		value := c.integerConstant(unary.X)
		if value.Kind() != constant.Int {
			return result
		}
		for _, op := range []gotoken.Token{gotoken.ADD, gotoken.SUB, gotoken.XOR} {
			if op.String() == unary.Op.String() {
				return constant.UnaryOp(op, value, 0)
			}
		}
		return result
	}
	if binary, ok := node.(*ast.BinaryExpr); ok {
		left, right := c.integerConstant(binary.X), c.integerConstant(binary.Y)
		if left.Kind() != constant.Int || right.Kind() != constant.Int {
			return result
		}
		if binary.Op.String() == "<<" || binary.Op.String() == ">>" {
			count, ok := constant.Uint64Val(right)
			// Match Go constant-shift bounds while avoiding unbounded allocations.
			if !ok || count > 1074 {
				return result
			}
			op := gotoken.SHL
			if binary.Op.String() == ">>" {
				op = gotoken.SHR
			}
			return constant.Shift(left, op, uint(count))
		}
		for _, op := range []gotoken.Token{gotoken.ADD, gotoken.SUB, gotoken.MUL, gotoken.QUO, gotoken.REM, gotoken.AND, gotoken.OR, gotoken.XOR, gotoken.AND_NOT} {
			if op.String() == binary.Op.String() {
				if op == gotoken.QUO {
					op = gotoken.QUO_ASSIGN
				}
				return constant.BinaryOp(left, op, right)
			}
		}
		return result
	}
	return c.constant(node)
}

func (c *checker) recordConstExpression(node ast.Expr) bool {
	parameter := c.out.ConstParams[c.symbol(node)]
	if parameter == nil {
		return false
	}
	if c.out.ConstValues == nil {
		c.out.ConstValues = map[ast.Expr]ConstValue{}
	}
	c.out.ConstValues[node] = ConstValue{Param: parameter}
	return true
}

func (c *checker) functionGeneric(sig *types.Signature) bool {
	return sig.TypeParams().Len() != 0 || len(c.out.FunctionConstParams[sig]) != 0
}

func (c *checker) bindConstArguments(sig *types.Signature, args []ConstValue, node ast.Node) (map[*ConstParam]ConstValue, bool) {
	params := c.out.FunctionConstParams[sig]
	if len(args) != len(params) {
		c.diag(node, GenericConstraint, fmt.Sprintf("got %d const arguments but want %d", len(args), len(params)))
		return nil, false
	}
	sub := make(map[*ConstParam]ConstValue, len(params))
	for index, parameter := range params {
		sub[parameter] = args[index]
	}
	return sub, true
}

func (c *checker) explicitFunctionArguments(sig *types.Signature, args *ast.GenericArgs) types.Type {
	groups := c.out.FunctionGenerics[sig]
	if groups == nil {
		actual := make([]types.Type, len(args.Args))
		for index, argument := range args.Args {
			actual[index] = c.genericArgType(argument)
		}
		return c.instantiateFunctionType(sig, actual, nil, args)
	}
	want := 0
	for _, group := range groups.Params {
		want += len(group.Names)
	}
	if len(args.Args) != want {
		c.diag(args, GenericConstraint, fmt.Sprintf("got %d generic arguments but want %d", len(args.Args), want))
		return types.Typ[types.Invalid]
	}
	var actual []types.Type
	var constants []ConstValue
	index := 0
	for _, group := range groups.Params {
		for range group.Names {
			argument := args.Args[index]
			index++
			if group.Const {
				expression := argument.Value
				if path, ok := argument.Type.(*ast.PathType); ok {
					expression = &ast.PathExpr{Base: path.Base, Path: path.Path}
				}
				if expression == nil {
					c.diag(argument, GenericConstraint, "an integer const argument is required")
					return types.Typ[types.Invalid]
				}
				value, ok := c.constExpression(expression)
				if !ok {
					return types.Typ[types.Invalid]
				}
				constants = append(constants, value)
			} else {
				actual = append(actual, c.genericArgType(argument))
			}
		}
	}
	return c.instantiateFunctionType(sig, actual, constants, args)
}

// inferArrayArguments binds a source length only when array shapes agree.
func (c *checker) inferArrayArguments(formal, actual types.Type, sub map[*types.TypeParam]types.Type, constants map[*ConstParam]ConstValue) bool {
	if formal == nil || actual == nil {
		return true
	}
	formal, actual = types.Unalias(formal), types.Unalias(actual)
	if array, ok := c.out.ArrayOf(formal); ok {
		other, ok := c.out.ArrayOf(actual)
		if !ok {
			return false
		}
		if parameter := array.Length.Param; parameter != nil {
			if previous, exists := constants[parameter]; exists && previous != other.Length {
				return false
			}
			constants[parameter] = other.Length
		} else if array.Length != other.Length {
			return false
		}
		return c.inferArrayArguments(array.Elem, other.Elem, sub, constants)
	}
	switch f := formal.(type) {
	case *types.TypeParam:
		if previous := sub[f]; previous != nil && !types.Identical(previous, actual) {
			return false
		}
		sub[f] = actual
	case *types.Slice:
		if a, ok := actual.(*types.Slice); ok {
			return c.inferArrayArguments(f.Elem(), a.Elem(), sub, constants)
		}
	case *types.Pointer:
		if a, ok := actual.(*types.Pointer); ok {
			return c.inferArrayArguments(f.Elem(), a.Elem(), sub, constants)
		}
	case *types.Map:
		if a, ok := actual.(*types.Map); ok {
			return c.inferArrayArguments(f.Key(), a.Key(), sub, constants) && c.inferArrayArguments(f.Elem(), a.Elem(), sub, constants)
		}
	case *types.Chan:
		if a, ok := actual.(*types.Chan); ok {
			return c.inferArrayArguments(f.Elem(), a.Elem(), sub, constants)
		}
	case *types.Signature:
		if a, ok := actual.Underlying().(*types.Signature); ok && f.Variadic() == a.Variadic() {
			return c.inferArrayArguments(f.Params(), a.Params(), sub, constants) && c.inferArrayArguments(f.Results(), a.Results(), sub, constants)
		}
	case *types.Tuple:
		if a, ok := actual.(*types.Tuple); ok && f.Len() == a.Len() {
			for index := 0; index < f.Len(); index++ {
				if !c.inferArrayArguments(f.At(index).Type(), a.At(index).Type(), sub, constants) {
					return false
				}
			}
		}
	case *types.Named:
		if a, ok := actual.(*types.Named); ok && a.Origin() == f.Origin() {
			for index := 0; index < f.TypeArgs().Len() && index < a.TypeArgs().Len(); index++ {
				if !c.inferArrayArguments(f.TypeArgs().At(index), a.TypeArgs().At(index), sub, constants) {
					return false
				}
			}
		}
	}
	return true
}

func (i *Info) isArray(t types.Type) bool { _, ok := i.ArrayOf(t); return ok }

func (c *checker) typeArgumentPositions(signature *types.Signature, count int) []int {
	var positions []int
	if groups := c.out.FunctionGenerics[signature]; groups != nil {
		index := 0
		for _, group := range groups.Params {
			for range group.Names {
				if !group.Const && index < count {
					positions = append(positions, index)
				}
				index++
			}
		}
	} else {
		for index := 0; index < count; index++ {
			positions = append(positions, index)
		}
	}
	return positions
}

func (c *checker) constraintUsesConst(node ast.Type) bool {
	found := false
	ast.Walk(node, func(n ast.Node) bool {
		if found || n == nil {
			return false
		}
		if _, ok := n.(*ast.Ident); ok {
			found = c.out.ConstParams[c.symbol(n)] != nil
		}
		return !found
	})
	return found
}

// Go array literals infer their actual length from positional and keyed values.
func (c *checker) inferredArrayLiteral(array *ast.ArrayType, elements []ast.Expr) types.Type {
	var length, index int64
	for _, element := range elements {
		if keyed, ok := element.(*ast.KeyValue); ok {
			value, ok := c.constExpression(keyed.Key)
			if !ok || value.Param != nil {
				return types.Typ[types.Invalid]
			}
			index = value.Value
		}
		if index == int64(^uint64(0)>>1) {
			c.diag(element, GenericConstraint, "array literal length overflows int")
			return types.Typ[types.Invalid]
		}
		index++
		if index > length {
			length = index
		}
	}
	result := types.NewArray(c.typ(array.Elem), length)
	c.out.typeNodes[array] = result
	return result
}

// Imported Go constants already carry their exact value in go/types.
func (c *checker) importedIntegerConstant(node ast.Expr) constant.Value {
	var reference resolve.Resolution
	switch expression := node.(type) {
	case *ast.PathExpr:
		reference = c.file.References[expression.Path.ID()]
	case *ast.Ident:
		reference = c.file.References[expression.ID()]
	default:
		return nil
	}
	parts, name := reference.Path, ""
	if symbol := reference.Symbol; symbol != nil && symbol.Kind != resolve.Import && symbol.Kind != resolve.Module {
		if len(symbol.Target) > 0 {
			parts = symbol.Target
		}
		name = symbol.GoName
	}
	object, ok := c.importedObject(parts, name).(*types.Const)
	if !ok {
		return nil
	}
	basic, ok := types.Unalias(object.Type()).Underlying().(*types.Basic)
	if !ok || basic.Info()&types.IsInteger == 0 {
		return constant.MakeUnknown()
	}
	return object.Val()
}
