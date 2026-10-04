// Package propagate lowers the dedicated Option/Result propagation operator.
package propagate

import (
	"fmt"
	"github.com/hxsf/goxide/internal/sourcemap"
	"go/scanner"
	"go/token"
	"strings"
)

const marker = "__goxide_question"

// Prepare turns postfix propagation into an internal parsing marker. It does not
// grant arbitrary named types a propagation implementation.
func Prepare(filename string, source []byte) ([]byte, error) {
	return PrepareMapped(filename, source, nil)
}
func PrepareMapped(filename string, source []byte, provenance *sourcemap.Map) ([]byte, error) {
	if strings.IndexByte(string(source), 0) >= 0 {
		return nil, fmt.Errorf("%s: NUL byte in source", filename)
	}
	s := string(source)
	for {
		fs := token.NewFileSet()
		f := fs.AddFile(filename, -1, len(s))
		var sc scanner.Scanner
		sc.Init(f, []byte(s), func(token.Position, string) {}, 0)
		type tk struct {
			k    token.Token
			text string
			off  int
		}
		var ts []tk
		qi := -1
		for {
			p, k, v := sc.Scan()
			if k == token.EOF {
				break
			}
			if v == "" {
				v = k.String()
			}
			ts = append(ts, tk{k, v, f.Offset(p)})
			if k == token.ILLEGAL && v == "?" {
				qi = len(ts) - 1
				break
			}
		}
		if qi < 0 {
			return []byte(s), nil
		}
		var primary func(int) (int, error)
		primary = func(i int) (int, error) {
			if i < 0 {
				return 0, fmt.Errorf("missing propagation operand")
			}
			end := i
			if ts[i].k == token.RPAREN || ts[i].k == token.RBRACK {
				close := ts[i].k
				open := token.LPAREN
				if close == token.RBRACK {
					open = token.LBRACK
				}
				depth := 1
				i--
				for i >= 0 {
					if ts[i].k == close {
						depth++
					}
					if ts[i].k == open {
						depth--
						if depth == 0 {
							break
						}
					}
					i--
				}
				if i < 0 {
					return 0, fmt.Errorf("unbalanced propagation operand")
				}
				if i > 1 && ts[i-1].k == token.NOT && ts[i-2].k == token.IDENT {
					return primary(i - 2)
				}
				if i > 0 && (ts[i-1].k == token.IDENT || ts[i-1].k == token.RPAREN || ts[i-1].k == token.RBRACK) {
					return primary(i - 1)
				}
			} else if ts[i].k != token.IDENT {
				return 0, fmt.Errorf("unsupported propagation operand ending %s", ts[end].text)
			}
			if i >= 2 && ts[i-1].k == token.PERIOD {
				return primary(i - 2)
			}
			if i >= 3 && ts[i-1].k == token.COLON && ts[i-2].k == token.COLON {
				return primary(i - 3)
			}
			if i > 0 && ts[i-1].text == "$" {
				return i - 1, nil
			}
			return i, nil
		}
		start, e := primary(qi - 1)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", fs.Position(f.Pos(ts[qi].off)), e)
		}
		a, b := ts[start].off, ts[qi].off
		operand := strings.TrimSpace(s[a:b])
		if provenance != nil {
			f := &sourcemap.Fragments{Input: provenance}
			start := a + strings.Index(s[a:b], operand)
			data, m := f.Finish(f.Copy(0, a)+marker+"("+f.Copy(start, start+len(operand))+")"+f.Copy(b+1, len(s)), "propagate.Prepare")
			*provenance = *m
			s = string(data)
		} else {
			s = s[:a] + marker + "(" + operand + ")" + s[b+1:]
		}
	}
}
