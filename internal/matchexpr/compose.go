package matchexpr

import (
	"fmt"
	"go/token"
	"strings"
)

// composer lowers only complete expressions with a proven contextual type.
// Every generated statement stays in the original function, including ? exits.
type composer struct {
	ts      []tok
	pairs   map[int]int
	copy    func(int, int) string
	fail    func(int, string) error
	origin  func(int) string
	callees map[string][]string
	hash    string
	serial  int
	aliases strings.Builder
}

func (c *composer) temp() string { c.serial++; return fmt.Sprintf("__hgo_match_expr_%d", c.serial) }
func (c *composer) contains(a, b int) bool {
	for i := a; i < b; i++ {
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
		if value, e := c.expression(a, b, dest, "direct"); e == nil {
			return dest + " = (" + value + ");\n", nil
		}
		pre, call, err := c.call(a, b)
		if err != nil {
			return "", err
		}
		return pre + dest + " = (" + call + ");\n", nil
	}
	return dest + " = (" + c.copy(ts[a].a, ts[b-1].b) + ");\n", nil
}
func (c *composer) call(a, b int) (string, string, error) {
	ts := c.ts
	if a+2 >= b || ts[a].k != token.IDENT || ts[a+1].k != token.LPAREN || c.pairs[a+1] != b-1 {
		return "", "", c.fail(a, "embedded match requires a complete direct call with a known same-file signature")
	}
	params, ok := c.callees[ts[a].s]
	if !ok {
		return "", "", c.fail(a, "match argument callee is outside the supported signature/resolution subset")
	}
	var args [][2]int
	for i := a + 2; i < b-1; {
		start := i
		for i < b-1 && ts[i].k != token.COMMA {
			if e, ok := c.pairs[i]; ok {
				i = e
			}
			i++
		}
		args = append(args, [2]int{start, i})
		if i < b-1 {
			i++
		}
	}
	if len(args) != len(params) {
		return "", "", c.fail(a, "match argument call requires exact non-variadic arity")
	}
	var out strings.Builder
	var names []string
	for n, arg := range args {
		name := c.temp()
		alias := fmt.Sprintf("__hgo_match_expr_type_%s_%d", c.hash, c.serial)
		fmt.Fprintf(&c.aliases, "\ntype %s = %s\n", alias, params[n])
		fmt.Fprintf(&out, "var %s %s;\n", name, alias)
		value, err := c.assign(arg[0], arg[1], name, alias)
		if err != nil {
			return "", "", err
		}
		out.WriteString(value)
		names = append(names, name)
	}
	return out.String(), ts[a].s + "(" + strings.Join(names, ",") + ")", nil
}
