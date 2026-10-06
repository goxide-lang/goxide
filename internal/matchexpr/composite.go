package matchexpr

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

const literalContext = "__goxide_match_expr_literal()"

// literal retains the Go composite type and element roles. The typed pass, not
// the token parser, chooses the actual field/element/map-key/map-value identity.
func (c *composer) literal(a, b int, context string) (string, bool, error) {
	for open := a; open < b; open++ {
		end, paired := c.pairs[open]
		if !paired {
			continue
		}
		if c.ts[open].k != token.LBRACE || end != b-1 {
			open = end
			continue
		}
		if open == a {
			if context != literalContext {
				return "", false, nil
			}
		} else if !c.literalType(a, open) {
			return "", false, nil
		}
		var elements []string
		for start := open + 1; start < end; {
			stop, colon := start, -1
			for stop < end && c.ts[stop].k != token.COMMA {
				if close, ok := c.pairs[stop]; ok {
					stop = close + 1
					continue
				}
				if c.ts[stop].k == token.COLON && (stop == start || c.ts[stop-1].k != token.COLON) && (stop+1 == end || c.ts[stop+1].k != token.COLON) {
					if colon >= 0 {
						return "", true, c.fail(stop, "invalid composite element")
					}
					colon = stop
				}
				stop++
			}
			valueStart := start
			key := ""
			if colon >= 0 {
				var err error
				key, err = c.expression(start, colon, literalContext, "direct")
				if err != nil {
					return "", true, err
				}
				key += ":"
				valueStart = colon + 1
			}
			value, err := c.expression(valueStart, stop, literalContext, "direct")
			if err != nil {
				return "", true, err
			}
			elements = append(elements, key+value)
			start = stop + 1
		}
		typeText := ""
		if a < open {
			typeText = c.copy(c.ts[a].a, c.ts[open].a)
		}
		return typeText + "{" + strings.Join(elements, ",") + "}", true, nil
	}
	return "", false, nil
}

func (c *composer) literalType(a, open int) bool {
	expr, err := parser.ParseExpr(string(c.source[c.ts[a].a:c.ts[open].a]) + "{}")
	_, ok := expr.(*ast.CompositeLit)
	return err == nil && ok
}

// compositeOpen proves that an enclosing brace is a literal rather than a
// statement/function/match block before statement-boundary scanning crosses it.
func (c *composer) compositeOpen(open, functionOpen int) bool {
	for a := functionOpen + 1; a < open; a++ {
		switch c.ts[a].k {
		case token.IDENT, token.LBRACK, token.MAP, token.STRUCT:
		default:
			continue
		}
		switch c.ts[a-1].k {
		case token.ASSIGN, token.DEFINE, token.RETURN, token.LPAREN, token.COMMA, token.COLON, token.LBRACE:
			if c.literalType(a, open) {
				return true
			}
		}
	}
	// Go elides nested literal types only in another literal's element slot.
	if open > functionOpen+1 {
		switch c.ts[open-1].k {
		case token.COLON, token.COMMA, token.LBRACE:
			parent := -1
			for a, b := range c.pairs {
				if a < open && open < b && c.ts[a].k == token.LBRACE && a > parent {
					parent = a
				}
			}
			return parent > functionOpen && c.compositeOpen(parent, functionOpen)
		}
	}
	return false
}
