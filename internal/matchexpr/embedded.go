package matchexpr

import (
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

// expression retains the host expression, using compiler-only closures
// as typed match atoms. propagate removes these closures before Go emission;
// they are not user function boundaries and never execute as calls.
func (c *composer) expression(a, b int, context, kind string) (string, error) {
	ts := c.ts
	if a >= b {
		return "", c.fail(a, "empty expression")
	}
	if ts[a].k == token.LPAREN && c.pairs[a] == b-1 {
		s, e := c.expression(a+1, b-1, context, kind)
		return "(" + s + ")", e
	}
	if !c.contains(a, b) {
		return c.copy(ts[a].a, ts[b-1].b), nil
	}
	split, prec := -1, 100
	for i := a; i < b; i++ {
		if end, ok := c.pairs[i]; ok {
			i = end
			continue
		}
		p := ts[i].k.Precedence()
		if p == 0 || i == a {
			continue
		}
		// Prefix unary operators are not binary roots.
		if ts[i-1].k.IsOperator() && ts[i-1].k != token.RPAREN && ts[i-1].k != token.RBRACK && ts[i-1].k != token.RBRACE {
			continue
		}
		if p <= prec {
			split, prec = i, p
		}
	}
	if split >= 0 {
		op := ts[split].k
		childKind := "numeric"
		switch op {
		case token.LAND, token.LOR:
			childKind = "boolean"
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			context = ""
			childKind = "comparable"
		}
		left, e := c.expression(a, split, context, childKind)
		if e != nil {
			return "", e
		}
		rightContext := context
		if op == token.SHL || op == token.SHR {
			rightContext = ""
		}
		right, e := c.expression(split+1, b, rightContext, childKind)
		if e != nil {
			return "", e
		}
		return "(" + left + " " + op.String() + " " + right + ")", nil
	}
	// These unary operations preserve their operand's scalar identity. Address,
	// dereference and receive contexts keep their existing explicit boundary.
	switch ts[a].k {
	case token.ADD, token.SUB, token.XOR, token.NOT:
		childKind := "numeric"
		if ts[a].k == token.NOT {
			childKind = "boolean"
		}
		value, err := c.expression(a+1, b, context, childKind)
		return ts[a].k.String() + "(" + value + ")", err
	}
	if value, matched, err := c.literal(a, b, context); matched {
		return value, err
	}
	// Find the final call suffix, independent of callee surface syntax. The
	// actual signature and parameter identity are resolved by go/types later.
	for open := a; open < b; open++ {
		end, paired := c.pairs[open]
		if !paired {
			continue
		}
		if ts[open].k != token.LPAREN || end != b-1 || open == a {
			open = end
			continue
		}
		callee := c.copy(ts[a].a, ts[open].a)
		if _, err := parser.ParseExpr(string(c.source[ts[a].a:ts[open].a])); err != nil {
			break
		}
		var args []string
		for i := open + 1; i < b-1; {
			start := i
			for i < b-1 && ts[i].k != token.COMMA {
				if end, ok := c.pairs[i]; ok {
					i = end
				}
				i++
			}
			argEnd := i
			spread := argEnd > start && ts[argEnd-1].k == token.ELLIPSIS
			if spread {
				argEnd--
			}
			value, err := c.expression(start, argEnd, "__goxide_match_expr_argument()", "direct")
			if err != nil {
				return "", err
			}
			if spread {
				value += "..."
			}
			args = append(args, value)
			if i < b-1 {
				i++
			}
		}
		return callee + "(" + strings.Join(args, ",") + ")", nil
	}
	if ts[a].k != token.IDENT || ts[a].s != "match" {
		return "", c.fail(a, "embedded match requires supported operators, calls, parentheses or composite elements")
	}
	name := strings.Replace(c.temp(), "__goxide_match_expr_", "__goxide_match_expr_atom_", 1)
	marker := "__goxide_match_expr_infer(" + strconv.Quote(c.origin(a)) + ")"

	if context != "" {
		marker = "__goxide_match_expr_operand_context(" + context + "," + strconv.Quote(kind) + "," + strconv.Quote(c.origin(a)) + ")"
	}

	body, e := c.assign(a, b, name, "")
	if e != nil {
		return "", e
	}
	return "func() __goxide_match_expr_pending { var " + name + " = " + marker + ";\n" + body + "return " + name + " }()", nil
}

// embedded handles full statement boundaries only. Keeping the actual indexed
// LHS in the assignment delegates its storage/evaluation semantics to place.
func (c *composer) embedded(i, open int, result string) (edit, int, bool, error) {
	ts := c.ts
	reverse := map[int]int{}
	for a, b := range c.pairs {
		reverse[b] = a
	}
	start := i - 1
	for start > open {
		if a, ok := reverse[start]; ok {
			start = a - 1
			continue
		}
		if ts[start].k == token.LBRACE && c.compositeOpen(start, open) {
			start--
			continue
		}
		if ts[start].k == token.SEMICOLON || ts[start].k == token.LBRACE || ts[start].k == token.RBRACE {
			break
		}
		start--
	}
	start++
	expr, assign := -1, -1
	context := ""
	prefix := ""
	if ts[start].k == token.RETURN {
		expr = start + 1
		if result == "" {
			return edit{}, 0, false, c.fail(i, "return requires one concrete result type")
		}
		context = "__goxide_match_expr_return()"
		prefix = "return "
	} else {
		for n := start; n < i; n++ {
			if e, ok := c.pairs[n]; ok {
				n = e
				continue
			}
			if ts[n].k == token.ASSIGN || ts[n].k == token.DEFINE {
				assign = n
				break
			}
		}
		if assign < 0 {
			// Whole expression-statement calls share the same typed call path.
			// Statement match and go/defer/control headers are not expressions.
			if ts[start].s == "match" {
				return edit{}, 0, false, nil
			}
			end := start
			for end < len(ts) && ts[end].k != token.SEMICOLON && ts[end].k != token.RBRACE {
				if close, ok := c.pairs[end]; ok {
					end = close
				}
				end++
			}
			if end > i && ts[end-1].k == token.RPAREN {
				value, err := c.expression(start, end, "", "direct")
				if err == nil {
					return edit{ts[start].a, ts[end-1].b, value}, end - 1, true, nil
				}
			}
			return edit{}, 0, false, nil
		}
		expr = assign + 1
		if ts[start].k == token.VAR {
			if start+2 < assign {
				context = "__goxide_match_expr_zero[" + c.copy(ts[start+2].a, ts[assign].a) + "]()"
			}
		} else if assign == start+1 && ts[start].k == token.IDENT {
			if ts[assign].k == token.ASSIGN {
				context = c.copy(ts[start].a, ts[assign].a)
			}
		} else {
			// This first indexed-target slice is one identifier and one subscript.
			if ts[assign].k != token.ASSIGN || ts[start].k != token.IDENT || start+1 >= assign || ts[start+1].k != token.LBRACK || c.pairs[start+1] != assign-1 {
				return edit{}, 0, false, nil
			}
			context = c.copy(ts[start].a, ts[assign].a)
		}
		prefix = c.copy(ts[start].a, ts[expr].a)
	}
	end := expr
	for end < len(ts) && ts[end].k != token.SEMICOLON && ts[end].k != token.RBRACE {
		if e, ok := c.pairs[end]; ok {
			end = e
		}
		end++
	}
	if end <= i {
		return edit{}, 0, false, nil
	}
	if expr == i && (ts[start].k == token.RETURN || ts[start].k == token.VAR || assign == start+1) {
		j := i + 1
		for j < end && ts[j].k != token.LBRACE {
			if e, ok := c.pairs[j]; ok {
				j = e
			}
			j++
		}
		if c.pairs[j] == end-1 {
			return edit{}, 0, false, nil
		}
	}
	value, e := c.expression(expr, end, context, "direct")
	if e != nil {
		return edit{}, 0, false, nil
	}
	return edit{ts[start].a, ts[end-1].b, strings.TrimSpace(prefix) + " " + value}, end - 1, true, nil
}
