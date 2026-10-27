package check

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"go/types"
	"strings"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (c *checker) match(n *ast.MatchExpr, expected types.Type, statement bool) types.Type {
	t := c.expr(n.X, nil)
	mode := ValueRef
	if ref, ok := referenceScrutinee(n.X).(*ast.RefExpr); ok {
		mode = ReadRef
		if ref.Mut {
			mode = WriteRef
		}
		t = c.out.Types[ref.X]
	}
	instance := c.out.EnumOf(t)
	if instance != nil && mode == WriteRef {
		c.diag(n.X, ReferencePermission, "enum payload slots are immutable; use a value pattern")
	}
	if instance != nil && !c.enumAccessible(instance) {
		c.diag(n, VariantMismatch, "internal-origin alias cannot expose enum variants outside its internal package boundary")
	}
	info := &Match{Enum: instance, Statement: statement, Reference: mode}
	c.out.Matches[n] = info
	if instance == nil {
		for _, arm := range n.Arms {
			var variant ast.Pat
			ast.Walk(arm.Pat, func(node ast.Node) bool {
				switch pattern := node.(type) {
				case *ast.TuplePat, *ast.StructPat:
					variant = node.(ast.Pat)
				case *ast.PathPat:
					if symbol := c.symbol(pattern.Path); symbol == nil || symbol.Kind != resolve.Constant {
						variant = pattern
					}
				case *ast.BindPat:
					if symbol := c.symbol(pattern.Name); symbol != nil && symbol.Kind == resolve.Variant {
						variant = pattern
					}
				}
				return variant == nil
			})
			if variant != nil {
				c.diag(variant, VariantMismatch, "enum variant pattern requires a Goxide enum value with source declarations")
				delete(c.out.Matches, n)
				return types.Typ[types.Invalid]
			}
		}
	}
	covered := map[string]bool{}
	patterns := map[string][]ast.Pat{}
	wild := false
	var result types.Type
	for _, arm := range n.Arms {
		if instance != nil && mode != WriteRef {
			ast.Walk(arm.Pat, func(node ast.Node) bool {
				if binding, ok := node.(*ast.BindPat); ok && binding.Ref && binding.Mut {
					c.diag(binding, ReferencePermission, "enum payload slots are immutable; use a value pattern")
				}
				return true
			})
		}
		name := c.patternName(arm.Pat)
		matchesEnum := c.patternMatches(arm.Pat, t)
		unreachable := wild || covered[name]
		if !unreachable {
			for _, prior := range patterns[name] {
				if c.subsumes(prior, arm.Pat, t) {
					unreachable = true
					break
				}
			}
		}
		if unreachable {
			c.diag(arm.Pat, UnreachableArm, "unreachable match arm")
		}
		if arm.Guard == nil && matchesEnum {
			if c.wildcard(arm.Pat) {
				wild = true
			} else {
				patterns[name] = append(patterns[name], arm.Pat)
				if c.irrefutable(arm.Pat, t) {
					covered[name] = true
				} else if instance != nil {
					if variant := instance.Variant(name); variant != nil {
						fields := instance.Fields(variant)
						var rows [][]ast.Pat
						for _, pattern := range patterns[name] {
							rows = append(rows, patternPayload(pattern, fields))
						}
						covered[name] = c.patternsCoverFields(rows, fields)
					}
				}
			}
		}
		c.bindPatternReference(arm.Pat, t, mode)
		c.expr(arm.Guard, types.Typ[types.Bool])
		armType := c.expr(arm.Body, expected)
		c.block(arm.Block)
		if !statement && valid(armType) {
			if armType == types.Typ[types.UntypedNil] && valid(expected) && !c.assignableGo(armType, expected) {
				c.diag(arm.Body, ArmTypeMismatch, fmt.Sprintf("cannot use nil as %s in match arm", expected))
			}
			if valid(expected) && (c.assignableGo(armType, expected) || untyped(armType) && types.Identical(defaultType(armType), expected)) {
				armType = expected
			}
			if result == nil {
				result = armType
			} else if !sameArmType(result, armType) {
				c.diag(arm.Body, ArmTypeMismatch, fmt.Sprintf("match arm has type %s, expected %s", armType, result))
			} else {
				result = mergeArmType(result, armType)
			}
		}
	}
	if instance != nil && !wild {
		for _, variant := range instance.Enum.Variants {
			if !covered[variant.Name] {
				info.Missing = append(info.Missing, variant)
				c.diag(n, NonExhaustive, fmt.Sprintf("match does not cover %s::%s", instance.Enum.Name, variant.Name))
			}
		}
	}
	if statement || result == nil {
		result = types.Typ[types.Invalid]
	} else if valid(expected) && (c.assignableGo(result, expected) || untyped(result) && types.Identical(defaultType(result), expected)) {
		result = expected
	}
	if c.out.EnumOf(result) != nil {
		for _, arm := range n.Arms {
			if c.nilLiteral(arm.Body) {
				c.expr(arm.Body, result)
			}
		}
	}
	if result == types.Typ[types.UntypedNil] {
		c.diag(n, ArmTypeMismatch, "cannot infer a match result type from untyped nil")
	}
	// Retain untyped arms until an enclosing binary expression or assignment
	// supplies their context. Lowering still needs a concrete storage type.
	info.Type = defaultType(result)
	return result
}

func sameEnumInstance(left, right *Instance) bool {
	if left.Enum != right.Enum || len(left.Args) != len(right.Args) {
		return false
	}
	for index, arg := range left.Args {
		if !types.Identical(arg, right.Args[index]) {
			return false
		}
	}
	return true
}

func (c *checker) enumAccessible(instance *Instance) bool {
	pkg := instance.Enum.Type.Obj().Pkg()
	if pkg == nil {
		return true
	}
	parts := strings.Split(pkg.Path(), "/")
	for index, part := range parts {
		if part == "internal" {
			parent := strings.Join(parts[:index], "/")
			if parent == "" || c.pkg.Path() != parent && !strings.HasPrefix(c.pkg.Path(), parent+"/") {
				return false
			}
		}
	}
	return true
}

func referenceScrutinee(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

func (c *checker) patternName(p ast.Pat) string {
	var path *ast.Path
	switch p := p.(type) {
	case *ast.BindPat:
		if s := c.symbol(p.Name); s != nil && s.Kind == resolve.Variant {
			return s.Name.Name
		}
	case *ast.PathPat:
		path = p.Path
	case *ast.TuplePat:
		path = p.Path
	case *ast.StructPat:
		path = p.Path
	}
	if path != nil && len(path.Segments) > 0 {
		return path.Segments[len(path.Segments)-1].Name.Name
	}
	return ""
}

func (c *checker) wildcard(p ast.Pat) bool {
	switch p := p.(type) {
	case *ast.WildPat:
		return true
	case *ast.BindPat:
		s := c.symbol(p.Name)
		return p.Sub == nil && (s == nil || s.Kind != resolve.Variant && s.Kind != resolve.Constant)
	}
	return false
}

func (c *checker) patternMatches(p ast.Pat, t types.Type) bool {
	if c.patternName(p) == "" {
		return true
	}
	var path *ast.Path
	var symbol *resolve.Symbol
	switch p := p.(type) {
	case *ast.BindPat:
		symbol = c.symbol(p.Name)
	case *ast.PathPat:
		path, symbol = p.Path, c.symbol(p.Path)
	case *ast.TuplePat:
		path, symbol = p.Path, c.symbol(p)
	case *ast.StructPat:
		path, symbol = p.Path, c.symbol(p)
	}
	if symbol != nil && symbol.Kind == resolve.Constant {
		return true
	}
	instance := c.out.EnumOf(t)
	con := c.constructor(path, symbol, t)
	if instance != nil && con != nil && sameEnumInstance(con.Enum, instance) {
		return true
	}
	c.diag(p, VariantMismatch, "pattern variant does not belong to the matched enum instance")
	return false
}

func (c *checker) patternFields(p ast.Pat, t types.Type) []Field {
	if enum := c.out.EnumOf(t); enum != nil {
		if variant := enum.Variant(c.patternName(p)); variant != nil {
			return enum.Fields(variant)
		}
	}
	return nil
}

// irrefutable checks the payload after selecting this pattern's variant.
func (c *checker) irrefutable(p ast.Pat, t types.Type) bool {
	switch p := p.(type) {
	case *ast.WildPat:
		return true
	case *ast.PathPat:
		s := c.symbol(p.Path)
		return s == nil || s.Kind != resolve.Constant
	case *ast.BindPat:
		if s := c.symbol(p.Name); s != nil && s.Kind == resolve.Constant {
			return false
		}
		return p.Sub == nil || c.payloadIrrefutable(p.Sub, t)
	case *ast.TuplePat:
		fields := c.patternFields(p, t)
		for i, e := range p.Elts {
			if i >= len(fields) || !c.payloadIrrefutable(e, fields[i].Type) {
				return false
			}
		}
		return true
	case *ast.StructPat:
		fields := c.patternFields(p, t)
		for _, f := range p.Fields {
			var typ types.Type
			for _, field := range fields {
				if field.Name == f.Name.Name {
					typ = field.Type
					break
				}
			}
			if typ == nil || !c.payloadIrrefutable(f.Pat, typ) {
				return false
			}
		}
		return true
	}
	return false
}

func (c *checker) payloadIrrefutable(p ast.Pat, t types.Type) bool {
	if c.patternName(p) != "" {
		if !c.patternMatches(p, t) {
			return false
		}
		if enum := c.out.EnumOf(t); enum != nil && len(enum.Enum.Variants) != 1 {
			return false
		}
	}
	return c.irrefutable(p, t)
}

func patternPayload(p ast.Pat, fields []Field) []ast.Pat {
	patterns := make([]ast.Pat, len(fields))
	switch p := p.(type) {
	case *ast.TuplePat:
		copy(patterns, p.Elts)
	case *ast.StructPat:
		for _, pattern := range p.Fields {
			for i, field := range fields {
				if pattern.Name.Name == field.Name {
					patterns[i] = pattern.Pat
				}
			}
		}
	}
	return patterns
}

// Each row matches one arm's payload. Specialize enum columns by variant so
// several nested arms can jointly cover a product of enum payloads.
func (c *checker) patternsCoverFields(rows [][]ast.Pat, fields []Field) bool {
	if len(rows) == 0 {
		return false
	}
	if len(fields) == 0 {
		return true
	}
	var unrestricted [][]ast.Pat
	for _, row := range rows {
		if row[0] == nil || c.payloadIrrefutable(row[0], fields[0].Type) {
			unrestricted = append(unrestricted, row[1:])
		}
	}
	if len(unrestricted) == len(rows) {
		return c.patternsCoverFields(unrestricted, fields[1:])
	}
	enum := c.out.EnumOf(fields[0].Type)
	if enum == nil {
		return c.patternsCoverFields(unrestricted, fields[1:])
	}
	for _, variant := range enum.Enum.Variants {
		payload := enum.Fields(variant)
		var specialized [][]ast.Pat
		for _, row := range rows {
			pattern := row[0]
			if bind, ok := pattern.(*ast.BindPat); ok && bind.Sub != nil {
				pattern = bind.Sub
			}
			if pattern == nil || c.wildcard(pattern) || c.patternName(pattern) == variant.Name && c.patternMatches(pattern, fields[0].Type) {
				specialized = append(specialized, append(patternPayload(pattern, payload), row[1:]...))
			}
		}
		if !c.patternsCoverFields(specialized, append(payload, fields[1:]...)) {
			return false
		}
	}
	return true
}

func (c *checker) subsumes(a, b ast.Pat, t types.Type) bool {
	if c.wildcard(a) {
		return true
	}
	if c.patternName(a) != c.patternName(b) {
		return false
	}
	if c.irrefutable(a, t) {
		return true
	}
	switch a := a.(type) {
	case *ast.BindPat:
		if s := c.symbol(a.Name); s != nil && s.Kind == resolve.Constant {
			return c.patternConstant(a) == c.patternConstant(b) && c.patternConstant(a) != ""
		}
	case *ast.PathPat:
		return c.patternConstant(a) == c.patternConstant(b) && c.patternConstant(a) != ""
	case *ast.LitPat:
		return c.patternConstant(a) == c.patternConstant(b) && c.patternConstant(a) != ""
	case *ast.TuplePat:
		if b, ok := b.(*ast.TuplePat); ok && len(a.Elts) == len(b.Elts) {
			fields := c.patternFields(a, t)
			for i := range a.Elts {
				if i >= len(fields) || !c.subsumes(a.Elts[i], b.Elts[i], fields[i].Type) {
					return false
				}
			}
			return true
		}
	case *ast.StructPat:
		if b, ok := b.(*ast.StructPat); ok {
			fields := c.patternFields(a, t)
			for _, af := range a.Fields {
				var typ types.Type
				for _, field := range fields {
					if field.Name == af.Name.Name {
						typ = field.Type
						break
					}
				}
				found := false
				for _, bf := range b.Fields {
					if typ != nil && af.Name.Name == bf.Name.Name && c.subsumes(af.Pat, bf.Pat, typ) {
						found = true
					}
				}
				if !found {
					return false
				}
			}
			return true
		}
	}
	return false
}

func (c *checker) patternConstant(p ast.Pat) string {
	var v constant.Value
	switch p := p.(type) {
	case *ast.LitPat:
		v = c.constant(p.Value)
	case *ast.BindPat:
		v = c.constants[c.symbol(p.Name)]
	case *ast.PathPat:
		v = c.constants[c.symbol(p.Path)]
	}
	if v == nil || v.Kind() == constant.Unknown {
		return ""
	}
	return v.ExactString()
}

func (c *checker) constant(e ast.Expr) constant.Value {
	switch e := e.(type) {
	case *ast.BoolLit:
		return constant.MakeBool(e.Value)
	case *ast.BasicLit:
		for _, kind := range []gotoken.Token{gotoken.INT, gotoken.FLOAT, gotoken.IMAG, gotoken.CHAR, gotoken.STRING} {
			if kind.String() == e.Kind.String() {
				return constant.MakeFromLiteral(e.Value, kind, 0)
			}
		}
	case *ast.Ident:
		if e.Name == "true" || e.Name == "false" {
			return constant.MakeBool(e.Name == "true")
		}
		if v := c.constants[c.symbol(e)]; v != nil {
			return v
		}
	case *ast.PathExpr:
		if v := c.constants[c.symbol(e)]; v != nil {
			return v
		}
	case *ast.UnaryExpr:
		v := c.constant(e.X)
		if v.Kind() != constant.Unknown {
			for _, op := range []gotoken.Token{gotoken.ADD, gotoken.SUB, gotoken.XOR, gotoken.NOT} {
				if op.String() == e.Op.String() {
					return constant.UnaryOp(op, v, 0)
				}
			}
		}
	case *ast.ParenExpr:
		return c.constant(e.X)
	}
	return constant.MakeUnknown()
}
func untyped(t types.Type) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Info()&types.IsUntyped != 0
}
func assignableArm(a, b types.Type) bool {
	return types.AssignableTo(a, b) || untyped(a) && types.Identical(defaultType(a), b)
}
func sameArmType(a, b types.Type) bool {
	return types.Identical(a, b) || untyped(a) && assignableArm(a, b) || untyped(b) && assignableArm(b, a)
}
func mergeArmType(a, b types.Type) types.Type {
	if untyped(a) && !untyped(b) {
		return b
	}
	return a
}

func (c *checker) try(n *ast.TryExpr) types.Type {
	input := c.out.EnumOf(c.expr(n.X, nil))
	info := &Try{Input: input}
	c.out.Tries[n] = info
	if input == nil || !input.Enum.Builtin {
		c.diag(n, TryReturnType, "? requires Option or Result")
		return types.Typ[types.Invalid]
	}
	if len(input.Args) > 0 {
		info.Value = input.Args[0]
	}
	if len(c.returns) == 1 {
		info.Output = c.out.EnumOf(c.returns[0])
	}
	if info.Output == nil || !info.Output.Enum.Builtin || input.Enum.Name != info.Output.Enum.Name {
		c.diag(n, TryReturnType, "? requires the nearest function to return the same Option or Result family")
		return info.Value
	}
	if input.Enum.Name == "Result" && len(input.Args) > 1 && len(info.Output.Args) > 1 {
		info.Error = input.Args[1]
		target := info.Output.Args[1]
		p := c.newPermissionChecker()
		source := p.enumField(c.memberSource(n.X, c.out.Types[n.X]), input.Variant("Err"), 0)
		want := p.fromGo(target)
		if len(c.returnSources) == 1 {
			want = p.enumField(c.returnSources[0], info.Output.Variant("Err"), 0)
		}
		if !c.assignableGo(info.Error, target) || !p.compatible(source, want, map[[2]types.Type]bool{}) {
			info.Conversion = c.tryConversion(n, target, info.Error, want, source)
		}
	}
	return info.Value
}

// ? is a language use of this exact standard declaration. User protocols with
// the same spelling, helper functions, and Into never enter this query.
func (c *checker) tryConversion(node *ast.TryExpr, target, source types.Type, targetSource, inputSource *permission) *MemberSelection {
	var trait *Trait
	for _, candidate := range c.out.Traits {
		if candidate.Symbol.Name.Name == "From" && c.out.CanonicalPackagePath(candidate.Type.Obj().Pkg()) == "github.com/goxide-lang/std/convert" {
			trait = candidate
			break
		}
	}
	if trait == nil || len(trait.TypeParams) != 1 || memberNamed(trait.Methods, "from", true) == nil {
		c.diag(node, TryReturnType, fmt.Sprintf("cannot propagate %s to %s: canonical github.com/goxide-lang/std/convert::From is unavailable", source, target))
		return nil
	}
	goal := &TraitBound{info: c.out, File: c.file.Name, Trait: trait, Target: target, Inputs: []types.Type{source}, targetPermission: targetSource, inputPermissions: []*permission{inputSource}}
	witness := c.proveApplication(goal)
	if witness.Impl == nil && witness.Bound == nil && len(witness.Cases) == 0 {
		message := fmt.Sprintf("cannot propagate %s to %s: requires %s", source, target, goal.ApplicationKey())
		if c.proofFailure != "" {
			message += ": " + c.proofFailure
		}
		c.diag(node, TryReturnType, message)
		return nil
	}
	selection := &MemberSelection{Evidence: witness}
	if witness.Impl != nil {
		selection.Impl = witness.Impl
		selection.Member = memberNamed(witness.Impl.Methods, "from", true)
		selection.Signature = selection.Member.Signature
	} else {
		selection.Bound = witness.Bound
		if selection.Bound == nil {
			selection.Bound = goal
		}
		selection.Member = memberNamed(trait.Methods, "from", true)
		selection.Signature = selection.Bound.MethodSignature(selection.Member)
	}
	return selection
}
