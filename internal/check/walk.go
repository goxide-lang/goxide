package check

import (
	"go/constant"
	gotoken "go/token"
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func (c *checker) globals(items []ast.Item) {
	for _, item := range items {
		switch n := item.(type) {
		case *ast.VarDecl:
			c.values(n.Specs)
		case *ast.ConstDecl:
			c.values(n.Specs)
		case *ast.ModDecl:
			c.globals(n.Items)
		case *ast.DeclareDecl:
			c.globals([]ast.Item{n.Decl})
		}
	}
}

func (c *checker) item(item ast.Item) {
	switch n := item.(type) {
	case *ast.FuncDecl:
		old := c.returns
		oldBounds := c.activeBounds
		c.activeBounds = c.out.GenericBounds[n]
		oldReceiver := c.currentReceiver
		if m := c.memberSymbols[c.symbol(n.Name)]; m != nil && !m.Static {
			c.currentReceiver = c.symbol(n.Params[0].Names[0])
		}
		c.returns = tupleTypes(c.tuple(n.Results))
		c.tuple(n.Params)
		c.tuple(n.Recv)
		c.block(n.Body)
		c.returns = old
		c.currentReceiver = oldReceiver
		c.activeBounds = oldBounds
	case *ast.VarDecl:
		c.values(n.Specs)
	case *ast.ConstDecl:
		c.values(n.Specs)
	case *ast.ModDecl:
		for _, i := range n.Items {
			c.item(i)
		}
	case *ast.DeclareDecl:
		c.item(n.Decl)
	case *ast.ImplDecl:
		if impl := c.out.Impls[n]; impl != nil {
			c.enterImpl(impl)
			defer c.leaveImpl()
		}
		for _, i := range n.Items {
			c.item(i)
		}
	}
}

func (c *checker) values(specs []*ast.ValueSpec) {
	for _, s := range specs {
		var want types.Type
		if s.Type != nil {
			want = c.typ(s.Type)
		}
		var values []types.Type
		for _, v := range s.Values {
			t := c.expr(v, want)
			if tuple, ok := t.(*types.Tuple); ok {
				values = append(values, tupleTypes(tuple)...)
			} else {
				values = append(values, t)
			}
		}
		for i, name := range s.Names {
			t := want
			if t == nil && i < len(values) {
				t = defaultType(values[i])
			}
			if t == nil {
				t = types.Typ[types.Invalid]
			}
			if sym := c.symbol(name); sym != nil {
				c.out.Symbols[sym] = t
				if s.Type != nil {
					c.sourceTypes[sym] = permissionDeclaration{c.file, s.Type}
				} else if i < len(s.Values) {
					c.sourceValues[sym] = sourceExpression{c.file, s.Values[i]}
				}
				if sym.Kind == resolve.Constant && i < len(s.Values) {
					c.constants[sym] = c.constant(s.Values[i])
				}
			}
		}
	}
}

func (c *checker) block(b *ast.Block) {
	if b != nil {
		for _, s := range b.List {
			c.stmt(s)
		}
	}
}

func (c *checker) stmt(stmt ast.Stmt) {
	if stmt == nil {
		return
	}
	switch n := stmt.(type) {
	case *ast.Block:
		c.block(n)
	case *ast.DeclStmt:
		if decl, ok := n.Decl.(*ast.TypeDecl); ok {
			items := []ast.Item{decl}
			c.collect(items)
			c.prepareGenerics(items)
			c.signatures(items)
		}
		c.item(n.Decl)
	case *ast.LetStmt:
		var want types.Type
		if n.Type != nil {
			want = c.typ(n.Type)
		}
		got := c.expr(n.Value, want)
		if want == nil {
			want = defaultType(got)
		}
		c.bindPattern(n.Pat, want)
		if bind, ok := n.Pat.(*ast.BindPat); ok {
			if sym := c.symbol(bind.Name); sym != nil {
				if n.Type != nil {
					c.sourceTypes[sym] = permissionDeclaration{c.file, n.Type}
				} else {
					c.sourceValues[sym] = sourceExpression{c.file, n.Value}
				}
			}
		}
	case *ast.AssignStmt:
		var values []types.Type
		for i, r := range n.Rhs {
			var want types.Type
			if i < len(n.Lhs) {
				want = c.expr(n.Lhs[i], nil)
			}
			t := c.expr(r, want)
			if tuple, ok := t.(*types.Tuple); ok {
				values = append(values, tupleTypes(tuple)...)
			} else {
				values = append(values, t)
			}
		}
		if len(n.Rhs) == 1 && len(n.Lhs) == 2 && len(values) == 1 {
			switch n.Rhs[0].(type) {
			case *ast.IndexExpr, *ast.TypeAssertExpr, *ast.UnaryExpr:
				values = append(values, types.Typ[types.Bool])
			}
		}
		for i, l := range n.Lhs {
			if id, ok := l.(*ast.Ident); ok && i < len(values) {
				if sym := c.symbol(id); sym != nil {
					if !valid(c.out.Symbols[sym]) {
						c.out.Symbols[sym] = defaultType(values[i])
					}
					c.out.Types[id] = c.out.Symbols[sym]
					if i < len(n.Rhs) && c.sourceTypes[sym].node == nil {
						c.sourceValues[sym] = sourceExpression{c.file, n.Rhs[i]}
					}
				}
			} else {
				c.expr(l, nil)
			}
		}
	case *ast.ExprStmt:
		if m, ok := n.X.(*ast.MatchExpr); ok {
			c.out.Types[m] = c.match(m, nil, true)
		} else {
			c.expr(n.X, nil)
		}
	case *ast.ReturnStmt:
		for i, v := range n.Results {
			var want types.Type
			if i < len(c.returns) {
				want = c.returns[i]
			}
			c.expr(v, want)
		}
	case *ast.IfStmt:
		c.checkMatchHeader(n.Init, n.Cond)
		c.stmt(n.Init)
		c.expr(n.Cond, types.Typ[types.Bool])
		c.block(n.Body)
		c.stmt(n.Else)
	case *ast.ForStmt:
		c.checkMatchHeader(n.Init, n.Cond, n.Post)
		c.stmt(n.Init)
		c.expr(n.Cond, types.Typ[types.Bool])
		c.block(n.Body)
		c.stmt(n.Post)
	case *ast.RangeStmt:
		c.checkMatchHeader(n.Key, n.Value, n.X)
		c.checkRangePropagation(n.Key, "error propagation in a range assignment target is not supported")
		c.checkRangePropagation(n.Value, "error propagation in a range assignment target is not supported")
		var want types.Type
		if n.Tok == token.ASSIGN && n.Key != nil {
			if typ := c.expr(n.Key, nil); valid(typ) {
				if integer, ok := typ.Underlying().(*types.Basic); ok && integer.Info()&types.IsInteger != 0 {
					want = typ
				}
			}
		}
		t := c.expr(n.X, want)
		key, value := types.Type(types.Typ[types.Int]), types.Type(types.Typ[types.Invalid])
		if valid(t) {
			switch x := t.Underlying().(type) {
			case *types.Array:
				value = x.Elem()
			case *types.Slice:
				value = x.Elem()
			case *types.Map:
				key, value = x.Key(), x.Elem()
			case *types.Chan:
				key = x.Elem()
			case *types.Basic:
				if x.Info()&types.IsString != 0 {
					value = types.Typ[types.Rune]
				} else if x.Info()&types.IsInteger != 0 {
					key = defaultType(t)
				}
			case *types.Signature:
				c.checkRangePropagation(n.X, "error propagation in a function iterator source is not supported")
			}
		}
		if n.Tok == token.DEFINE {
			c.bindExpr(n.Key, key)
			c.bindExpr(n.Value, value)
		} else {
			c.expr(n.Value, value)
		}
		c.block(n.Body)
	case *ast.SwitchStmt:
		c.checkMatchHeader(n.Init, n.Tag)
		c.stmt(n.Init)
		tag := c.expr(n.Tag, nil)
		var constants []constant.Value
		var constantTypes []types.Type
		for _, cl := range n.Cases {
			for _, e := range cl.List {
				want := tag
				if c.nilLiteral(e) {
					want = nil
				}
				typ := c.expr(e, want)
				value := c.constant(e)
				if n.Tag != nil && value.Kind() != constant.Unknown {
					for index, previous := range constants {
						if types.Identical(defaultType(typ), defaultType(constantTypes[index])) && constant.Compare(value, gotoken.EQL, previous) {
							c.diag(e, "switch-duplicate-case", "duplicate case in switch")
							break
						}
					}
					constants = append(constants, value)
					constantTypes = append(constantTypes, typ)
				}
			}
			for _, s := range cl.Body {
				c.stmt(s)
			}
		}
	case *ast.TypeSwitchStmt:
		c.checkMatchHeader(n.Init, n.Assign)
		c.stmt(n.Init)
		var subject types.Type
		if a, ok := n.Assign.(*ast.AssignStmt); ok && len(a.Rhs) == 1 {
			if x, ok := a.Rhs[0].(*ast.TypeAssertExpr); ok {
				subject = c.expr(x.X, nil)
			}
		} else if s, ok := n.Assign.(*ast.ExprStmt); ok {
			if x, ok := s.X.(*ast.TypeAssertExpr); ok {
				subject = c.expr(x.X, nil)
			}
		}
		for _, cl := range n.Cases {
			typ := subject
			if len(cl.Types) == 1 {
				if t := c.typ(cl.Types[0]); valid(t) && t != types.Typ[types.UntypedNil] {
					typ = t
				}
			}
			if symbol := c.file.Definitions[cl.ID()]; symbol != nil && typ != nil {
				c.out.Symbols[symbol] = typ
			}
			for _, s := range cl.Body {
				c.stmt(s)
			}
		}
	case *ast.SelectStmt:
		for _, cl := range n.Cases {
			c.stmt(cl.Comm)
			for _, s := range cl.Body {
				c.stmt(s)
			}
		}
	case *ast.SendStmt:
		channel := c.expr(n.Chan, nil)
		var want types.Type
		if channel, ok := channel.Underlying().(*types.Chan); ok {
			want = channel.Elem()
		}
		c.expr(n.Value, want)
	case *ast.IncDecStmt:
		c.expr(n.X, nil)
	case *ast.GoStmt:
		c.expr(n.Call, nil)
	case *ast.DeferStmt:
		c.expr(n.Call, nil)
	case *ast.LabeledStmt:
		c.stmt(n.Stmt)
	case *ast.LoopStmt:
		c.block(n.Body)
	case *ast.WhileStmt:
		c.checkMatchHeader(n.Cond)
		c.expr(n.Cond, types.Typ[types.Bool])
		c.block(n.Body)
	}
}

func (c *checker) checkMatchHeader(nodes ...ast.Node) {
	for _, node := range nodes {
		ast.Walk(node, func(current ast.Node) bool {
			switch current.(type) {
			case *ast.FuncLit:
				return false
			case *ast.MatchExpr:
				c.diag(current, "match-control-header", "match expressions in control headers are not supported")
				return false
			}
			return true
		})
	}
}

func (c *checker) checkRangePropagation(node ast.Node, message string) {
	ast.Walk(node, func(current ast.Node) bool {
		switch current.(type) {
		case *ast.FuncLit:
			return false
		case *ast.TryExpr:
			c.diag(current, "try-range", message)
			return false
		}
		return true
	})
}

func (c *checker) bindExpr(e ast.Expr, t types.Type) {
	if id, ok := e.(*ast.Ident); ok {
		if sym := c.symbol(id); sym != nil {
			c.out.Symbols[sym] = t
		}
		c.out.Types[id] = t
	} else {
		c.expr(e, nil)
	}
}

func (c *checker) bindPattern(p ast.Pat, t types.Type) {
	c.bindPatternReference(p, t, ValueRef)
}

func (c *checker) bindPatternReference(p ast.Pat, t types.Type, mode RefMode) {
	if !c.patternMatches(p, t) {
		return
	}
	switch p := p.(type) {
	case *ast.BindPat:
		if s := c.symbol(p.Name); s != nil && (s.Kind == resolve.Variant || s.Kind == resolve.Constant) {
			if s.Kind == resolve.Constant && valid(t) && !types.Comparable(t) {
				c.diag(p, IncomparablePayload, "constant payload pattern requires a comparable type; use a binding with a guard")
			}
			return
		}
		binding := mode
		if p.Ref {
			binding = ReadRef
			if p.Mut {
				binding = WriteRef
			}
		}
		if binding != ValueRef {
			c.out.RefBindings[p] = binding
			c.referenceModes[c.symbol(p.Name)] = binding
			c.bindExpr(p.Name, types.NewPointer(t))
		} else {
			c.bindExpr(p.Name, t)
		}
		if p.Sub != nil {
			c.bindPatternReference(p.Sub, t, mode)
		}
	case *ast.LitPat:
		c.expr(p.Value, t)
		if valid(t) && !types.Comparable(t) {
			c.diag(p, IncomparablePayload, "constant payload pattern requires a comparable type; use a binding with a guard")
		}
	case *ast.TuplePat:
		if enum := c.out.EnumOf(t); enum != nil {
			if v := enum.Variant(c.patternName(p)); v != nil {
				fields := enum.Fields(v)
				for i, elt := range p.Elts {
					if i < len(fields) {
						c.bindPatternReference(elt, fields[i].Type, mode)
					}
				}
			}
		}
	case *ast.StructPat:
		if enum := c.out.EnumOf(t); enum != nil {
			if v := enum.Variant(c.patternName(p)); v != nil {
				fields := enum.Fields(v)
				for _, f := range p.Fields {
					for _, field := range fields {
						if field.Name == f.Name.Name {
							c.bindPatternReference(f.Pat, field.Type, mode)
						}
					}
				}
			}
		}
	case *ast.OrPat:
		for _, alt := range p.Alts {
			c.bindPatternReference(alt, t, mode)
		}
	}
}
