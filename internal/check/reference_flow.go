package check

import (
	"go/constant"
	"go/types"

	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type permissionFlow struct {
	label                    *resolve.Symbol
	loop                     bool
	breaks, continues, falls []permissionEnv
}

func clonePermissionEnv(in permissionEnv) permissionEnv {
	if in == nil {
		return nil
	}
	out := make(permissionEnv, len(in))
	for symbol, value := range in {
		out[symbol] = value
	}
	return out
}

func (p *permissionChecker) joinEnvs(envs ...permissionEnv) permissionEnv {
	var out permissionEnv
	for _, env := range envs {
		if env == nil {
			continue
		}
		if out == nil {
			out = permissionEnv{}
		}
		for symbol, value := range env {
			out[symbol] = p.merge(out[symbol], value)
		}
	}
	return out
}

func (p *permissionChecker) sameEnv(a, b permissionEnv) bool {
	if (a == nil) != (b == nil) || len(a) != len(b) {
		return false
	}
	for symbol, value := range a {
		if !p.equal(value, b[symbol]) {
			return false
		}
	}
	return true
}

// Label inputs are monotone; another pass accounts for a backward goto without
// changing the generated control flow or merging a terminated path into a join.
func (p *permissionChecker) functionBlock(body *ast.Block) bool {
	outerFlow, outerLabels, outerChanged := p.flow, p.labels, p.labelsChanged
	defer func() { p.flow, p.labels, p.labelsChanged = outerFlow, outerLabels, outerChanged }()
	p.flow, p.labels = nil, map[*resolve.Symbol]permissionEnv{}
	entry := clonePermissionEnv(p.env)
	for {
		p.labelsChanged = false
		p.env = clonePermissionEnv(entry)
		p.block(body)
		if !p.labelsChanged {
			return p.env != nil
		}
	}
}

func (p *permissionChecker) block(body *ast.Block) bool {
	if body != nil {
		p.statements(body.List)
	}
	return p.env != nil
}

func (p *permissionChecker) statements(list []ast.Stmt) {
	for _, statement := range list {
		if p.env != nil {
			p.stmt(statement)
		} else if _, label := statement.(*ast.LabeledStmt); label {
			p.stmt(statement)
		}
	}
}

func (p *permissionChecker) stmt(statement ast.Stmt) bool {
	if statement == nil {
		return p.env != nil
	}
	switch n := statement.(type) {
	case *ast.Block:
		return p.block(n)
	case *ast.IfStmt:
		p.stmt(n.Init)
		p.expr(n.Cond, nil)
		entry := clonePermissionEnv(p.env)
		p.block(n.Body)
		left := p.env
		p.env = clonePermissionEnv(entry)
		p.stmt(n.Else)
		p.env = p.joinEnvs(left, p.env)
	case *ast.ForStmt, *ast.RangeStmt, *ast.LoopStmt, *ast.WhileStmt:
		p.loop(statement, nil)
	case *ast.SwitchStmt:
		p.switchStatement(n, nil)
	case *ast.TypeSwitchStmt:
		p.typeSwitch(n, nil)
	case *ast.SelectStmt:
		p.selectStatement(n, nil)
	case *ast.LabeledStmt:
		symbol := p.c.symbol(n.Label)
		p.env = p.joinEnvs(p.env, p.labels[symbol])
		if p.env == nil {
			return false
		}
		switch nested := n.Stmt.(type) {
		case *ast.ForStmt, *ast.RangeStmt, *ast.LoopStmt, *ast.WhileStmt:
			p.loop(n.Stmt, symbol)
		case *ast.SwitchStmt:
			p.switchStatement(nested, symbol)
		case *ast.TypeSwitchStmt:
			p.typeSwitch(nested, symbol)
		case *ast.SelectStmt:
			p.selectStatement(nested, symbol)
		default:
			p.stmt(n.Stmt)
		}
	case *ast.BranchStmt:
		p.branch(n)
	default:
		if !p.simpleStmt(statement) {
			p.env = nil
		}
	}
	return p.env != nil
}

func (p *permissionChecker) branch(n *ast.BranchStmt) {
	var label *resolve.Symbol
	if n.Label != nil {
		label = p.c.symbol(n.Label)
	}
	if n.Tok == token.GOTO {
		if label != nil {
			joined := p.joinEnvs(p.labels[label], p.env)
			if !p.sameEnv(joined, p.labels[label]) {
				p.labels[label], p.labelsChanged = joined, true
			}
		}
		p.env = nil
		return
	}
	for index := len(p.flow) - 1; index >= 0; index-- {
		frame := p.flow[index]
		if label != nil && frame.label != label || n.Tok == token.CONTINUE && !frame.loop {
			continue
		}
		switch n.Tok {
		case token.BREAK:
			frame.breaks = append(frame.breaks, clonePermissionEnv(p.env))
		case token.CONTINUE:
			frame.continues = append(frame.continues, clonePermissionEnv(p.env))
		case token.FALLTHROUGH:
			frame.falls = append(frame.falls, clonePermissionEnv(p.env))
		}
		break
	}
	p.env = nil
}

func (p *permissionChecker) loop(statement ast.Stmt, label *resolve.Symbol) {
	var body *ast.Block
	var cond ast.Expr
	var post ast.Stmt
	var iterator *ast.RangeStmt
	var source *permission
	switch n := statement.(type) {
	case *ast.ForStmt:
		p.stmt(n.Init)
		body, cond, post = n.Body, n.Cond, n.Post
	case *ast.RangeStmt:
		body, iterator = n.Body, n
		source = p.expr(n.X, nil)
		if source != nil && source.typ != nil {
			switch source.typ.Underlying().(type) {
			case *types.Signature:
				p.applyEffects(source)
			case *types.Chan:
				p.synchronize()
			}
		}
	case *ast.WhileStmt:
		body, cond = n.Body, n.Cond
	case *ast.LoopStmt:
		body = n.Body
	}
	entry := clonePermissionEnv(p.env)
	head := entry
	frame := &permissionFlow{label: label, loop: true}
	p.flow = append(p.flow, frame)
	defer func() { p.flow = p.flow[:len(p.flow)-1] }()
	for {
		frame.breaks, frame.continues = nil, nil
		p.env = clonePermissionEnv(head)
		p.expr(cond, nil)
		conditionExit := clonePermissionEnv(p.env)
		if iterator != nil {
			p.rangeBind(iterator, source)
		}
		p.block(body)
		back := p.joinEnvs(append(frame.continues, p.env)...)
		p.env = back
		if back != nil {
			p.stmt(post)
			back = p.env
		}
		next := p.joinEnvs(entry, back)
		if !p.sameEnv(head, next) {
			head = next
			continue
		}
		exits := frame.breaks
		if iterator != nil {
			if p.rangeMayBeEmpty(iterator, source) {
				exits = append(exits, entry)
			}
			exits = append(exits, back)
		} else if cond != nil && !p.constantTrue(cond) {
			exits = append(exits, conditionExit)
		}
		p.env = p.joinEnvs(exits...)
		return
	}
}

func (p *permissionChecker) constantTrue(expr ast.Expr) bool {
	value := p.c.constant(expr)
	return value != nil && value.Kind() == constant.Bool && constant.BoolVal(value)
}

func (p *permissionChecker) rangeMayBeEmpty(n *ast.RangeStmt, source *permission) bool {
	if source != nil && source.typ != nil {
		typ := source.typ
		if pointer, ok := typ.Underlying().(*types.Pointer); ok {
			typ = pointer.Elem()
		}
		if array, ok := p.c.out.ArrayOf(typ); ok {
			return array.Length.Param != nil || array.Length.Value == 0
		}
	}
	value := p.c.constant(n.X)
	if value == nil {
		return true
	}
	if value.Kind() == constant.Int {
		return constant.Sign(value) <= 0
	}
	if value.Kind() == constant.String {
		return len(constant.StringVal(value)) == 0
	}
	return true
}

func (p *permissionChecker) switchStatement(n *ast.SwitchStmt, label *resolve.Symbol) {
	p.stmt(n.Init)
	p.expr(n.Tag, nil)
	inputs := make([]permissionEnv, len(n.Cases))
	defaultCase := -1
	for index, clause := range n.Cases {
		if clause.Default {
			defaultCase = index
			continue
		}
		for _, value := range clause.List {
			p.expr(value, nil)
			inputs[index] = p.joinEnvs(inputs[index], p.env)
		}
	}
	var exits []permissionEnv
	if defaultCase >= 0 {
		inputs[defaultCase] = clonePermissionEnv(p.env)
	} else {
		exits = append(exits, p.env)
	}
	frame := &permissionFlow{label: label}
	p.flow = append(p.flow, frame)
	defer func() { p.flow = p.flow[:len(p.flow)-1] }()
	for index, clause := range n.Cases {
		p.env = p.joinEnvs(append(frame.falls, inputs[index])...)
		frame.falls = nil
		p.statements(clause.Body)
		exits = append(exits, p.env)
	}
	p.env = p.joinEnvs(append(exits, frame.breaks...)...)
}

func (p *permissionChecker) typeSwitch(n *ast.TypeSwitchStmt, label *resolve.Symbol) {
	p.stmt(n.Init)
	var subject ast.Expr
	switch assignment := n.Assign.(type) {
	case *ast.AssignStmt:
		if len(assignment.Rhs) == 1 {
			if assertion, ok := assignment.Rhs[0].(*ast.TypeAssertExpr); ok {
				subject = assertion.X
			}
		}
	case *ast.ExprStmt:
		if assertion, ok := assignment.X.(*ast.TypeAssertExpr); ok {
			subject = assertion.X
		}
	}
	value := p.expr(subject, nil)
	entry := clonePermissionEnv(p.env)
	frame := &permissionFlow{label: label}
	p.flow = append(p.flow, frame)
	defer func() { p.flow = p.flow[:len(p.flow)-1] }()
	var exits []permissionEnv
	hasDefault := false
	for _, clause := range n.Cases {
		hasDefault = hasDefault || clause.Default
		p.env = clonePermissionEnv(entry)
		if symbol := p.c.file.Definitions[clause.ID()]; symbol != nil {
			bound := p.fromGo(p.c.out.Symbols[symbol])
			if value != nil && (value.shared || value.ref == ReadRef) {
				bound = p.readValue(bound)
			}
			p.setSymbol(symbol, bound)
		}
		p.statements(clause.Body)
		exits = append(exits, p.env)
	}
	if !hasDefault {
		exits = append(exits, entry)
	}
	p.env = p.joinEnvs(append(exits, frame.breaks...)...)
}

func (p *permissionChecker) selectStatement(n *ast.SelectStmt, label *resolve.Symbol) {
	// Go evaluates all channel operands and send values before selecting a case.
	for _, clause := range n.Cases {
		switch comm := clause.Comm.(type) {
		case *ast.SendStmt:
			p.simpleStmt(comm)
		case *ast.AssignStmt:
			for _, rhs := range comm.Rhs {
				if receive, ok := rhs.(*ast.UnaryExpr); ok && receive.Op == token.ARROW {
					p.expr(receive.X, nil)
				}
			}
		case *ast.ExprStmt:
			if receive, ok := comm.X.(*ast.UnaryExpr); ok && receive.Op == token.ARROW {
				p.expr(receive.X, nil)
			}
		}
	}
	entry := clonePermissionEnv(p.env)
	frame := &permissionFlow{label: label}
	p.flow = append(p.flow, frame)
	defer func() { p.flow = p.flow[:len(p.flow)-1] }()
	var exits []permissionEnv
	for _, clause := range n.Cases {
		p.env = clonePermissionEnv(entry)
		if _, send := clause.Comm.(*ast.SendStmt); !send {
			p.stmt(clause.Comm)
		}
		p.statements(clause.Body)
		exits = append(exits, p.env)
	}
	p.env = p.joinEnvs(append(exits, frame.breaks...)...)
}
