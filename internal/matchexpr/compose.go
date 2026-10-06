package matchexpr

import (
	"fmt"
	"go/token"
	"strings"
)

// composer lowers only complete expressions with a proven contextual type.
// Every generated statement stays in the original function, including ? exits.
type composer struct {
	source    []byte
	ts        []tok
	pairs     map[int]int
	functions map[int]int
	copy      func(int, int) string
	fail      func(int, string) error
	origin    func(int) string
	serial    int
}

func (c *composer) temp() string { c.serial++; return fmt.Sprintf("__goxide_match_expr_%d", c.serial) }
func (c *composer) contains(a, b int) bool {
	for i := a; i < b; i++ {
		if end, ok := c.functions[i]; ok {
			i = end
			continue
		}
		if c.ts[i].k == token.IDENT && c.ts[i].s == "match" {
			return true
		}
	}
	return false
}
func (c *composer) assign(a, b int, dest, typ string) (string, error) {
	ts := c.ts
	for a < b && ts[a].k == token.LPAREN && c.pairs[a] == b-1 {
		a++
		b--
	}
	if a >= b {
		return "", c.fail(a, "empty arm expression")
	}
	if ts[a].k == token.IDENT && ts[a].s == "match" {
		j := a + 1
		for j < b && ts[j].k != token.LBRACE {
			if end, ok := c.pairs[j]; ok {
				j = end
			}
			j++
		}
		end, ok := c.pairs[j]
		if !ok || end != b-1 {
			return "", c.fail(a, "match must occupy the complete contextual expression")
		}
		var out strings.Builder
		fmt.Fprintf(&out, "match %s {\n", c.copy(ts[a].b, ts[j].a))
		for k := j + 1; k < end; {
			if ts[k].k == token.COMMA || ts[k].k == token.SEMICOLON {
				k++
				continue
			}
			arm := k
			for k+1 < end && !(ts[k].k == token.ASSIGN && ts[k+1].k == token.GTR) {
				if e, ok := c.pairs[k]; ok {
					k = e
				}
				k++
			}
			if k+2 >= end {
				return "", c.fail(arm, "missing expression arm")
			}
			expr := k + 2
			if ts[expr].k == token.LBRACE {
				return "", c.fail(expr, "block expression arms unsupported")
			}
			p := expr
			for p < end && ts[p].k != token.COMMA && ts[p].k != token.SEMICOLON {
				if e, ok := c.pairs[p]; ok {
					p = e
				}
				p++
			}
			value, err := c.assign(expr, p, dest, typ)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&out, "%s => { %s },\n", c.copy(ts[arm].a, ts[k].a), value)
			k = p
		}
		out.WriteString("}\n")
		return out.String(), nil
	}
	if c.contains(a, b) {
		value, err := c.expression(a, b, dest, "direct")
		if err != nil {
			return "", err
		}
		return dest + " = (" + value + ");\n", nil
	}
	return dest + " = (" + c.copy(ts[a].a, ts[b-1].b) + ");\n", nil
}
