package check

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"go/types"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

func (c *checker) match(n *ast.MatchExpr, expected types.Type, statement bool) types.Type {
	t := c.expr(n.X, nil)
	instance := c.out.EnumOf(t)
	info := &Match{Enum: instance, Statement: statement}
	c.out.Matches[n] = info
	covered := map[string]bool{}
	patterns := map[string][]ast.Pat{}
	wild := false
	var result types.Type
	for _, arm := range n.Arms {
		name := c.patternName(arm.Pat)
		matchesEnum := true
		if instance != nil && name != "" {
			var path *ast.Path
			switch p := arm.Pat.(type) {
			case *ast.PathPat:
				path = p.Path
			case *ast.TuplePat:
				path = p.Path
			case *ast.StructPat:
				path = p.Path
			}
			con := c.constructor(path, c.symbol(arm.Pat), t)
			if con == nil || con.Enum.Enum != instance.Enum || !types.Identical(instanceType(con.Enum), t) {
				matchesEnum = false
				c.diag(arm.Pat, VariantMismatch, "pattern variant does not belong to the matched enum instance")
			}
		}
		unreachable := wild || covered[name]
		if !unreachable {
			for _, prior := range patterns[name] {
				if c.subsumes(prior, arm.Pat) {
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
				if c.irrefutable(arm.Pat) {
					covered[name] = true
				}
			}
		}
		c.bindPattern(arm.Pat, t)
		c.expr(arm.Guard, types.Typ[types.Bool])
		armType := c.expr(arm.Body, expected)
		c.block(arm.Block)
		if !statement && valid(armType) {
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
				c.diag(n, NonExhaustive, fmt.Sprintf("match does not cover %s::%s", instance.Enum.Name, variant.Name))
			}
		}
	}
	if statement || result == nil {
		result = types.Typ[types.Invalid]
	} else if valid(expected) && assignableArm(result, expected) {
		result = expected
	} else {
		result = defaultType(result)
	}
	info.Type = result
	return result
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
func (c *checker) irrefutable(p ast.Pat) bool {
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
		return p.Sub == nil || c.irrefutable(p.Sub)
	case *ast.TuplePat:
		for _, e := range p.Elts {
			if !c.irrefutable(e) {
				return false
			}
		}
		return true
	case *ast.StructPat:
		for _, f := range p.Fields {
			if !c.irrefutable(f.Pat) {
				return false
			}
		}
		return true
	}
	return false
}

func (c *checker) subsumes(a, b ast.Pat) bool {
	if c.wildcard(a) {
		return true
	}
	if c.patternName(a) != c.patternName(b) {
		return false
	}
	if c.irrefutable(a) {
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
			for i := range a.Elts {
				if !c.subsumes(a.Elts[i], b.Elts[i]) {
					return false
				}
			}
			return true
		}
	case *ast.StructPat:
		if b, ok := b.(*ast.StructPat); ok {
			for _, af := range a.Fields {
				found := false
				for _, bf := range b.Fields {
					if af.Name.Name == bf.Name.Name && c.subsumes(af.Pat, bf.Pat) {
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
		if !types.AssignableTo(info.Error, target) {
			var found int
			for _, helper := range c.conversions {
				sig, ok := c.out.Symbols[helper].(*types.Signature)
				if ok && sig.Params().Len() == 1 && sig.Results().Len() == 1 && types.Identical(sig.Params().At(0).Type(), info.Error) && types.Identical(sig.Results().At(0).Type(), target) {
					info.Conversion = helper
					found++
				}
			}
			if found != 1 {
				reason := "no explicit one-step conversion"
				if found > 1 {
					reason = "ambiguous explicit one-step conversion"
				}
				c.diag(n, TryReturnType, strings.Join([]string{"cannot propagate", info.Error.String(), "to", target.String() + ":", reason}, " "))
			}
		}
	}
	return info.Value
}
