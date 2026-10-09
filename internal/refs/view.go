package refs

import (
	"bytes"
	"go/parser"
	"go/scanner"
	"go/token"
)

// GoView provides an offset-preserving parser view of reference syntax. It is
// not lowering or permission evidence: callers must emit the original source
// and retain the normal Prepare/Finish checks. Comments and literals are never
// rewritten. Ordinary binary AND, including x & mut, keeps its Go spelling.
func GoView(source []byte) []byte {
	out := append([]byte(nil), source...)
	ts := tokens(out)
	for i, t := range ts {
		if t.kind == token.AND && i+1 < len(ts) && ts[i+1].text == "mut" && unaryAddress(ts, i) {
			for at := ts[i+1].start; at < ts[i+1].end; at++ {
				out[at] = ' '
			}
		}
	}
	for attempts := 0; attempts < len(ts); attempts++ {
		_, exprErr := parser.ParseExpr(string(out))
		errors := []struct {
			err        error
			adjustment int
		}{{exprErr, 0}}
		if len(ts) > 0 && ts[0].kind == token.FUNC {
			const prefix = "package p\n"
			_, err := parser.ParseFile(token.NewFileSet(), "", prefix+string(out)+"{}", parser.AllErrors)
			errors = append(errors, struct {
				err        error
				adjustment int
			}{err, -len(prefix)})
		}
		changed := false
		for _, parsed := range errors {
			list, ok := parsed.err.(scanner.ErrorList)
			if !ok {
				continue
			}
			for _, diagnostic := range list {
				at := diagnostic.Pos.Offset + parsed.adjustment
				if at < 0 || at >= len(out) || out[at] != '&' {
					continue
				}
				// An ampersand where the Go parser expects a type is viewed as
				// a pointer solely to discover the enclosing declaration shape.
				for i, t := range ts {
					if t.start != at || t.kind != token.AND {
						continue
					}
					out[at] = '*'
					if i+1 < len(ts) && ts[i+1].text == "mut" {
						for j := ts[i+1].start; j < ts[i+1].end; j++ {
							out[j] = ' '
						}
					}
					changed = true
					break
				}
				if changed {
					break
				}
			}
			if changed {
				break
			}
		}
		if !changed {
			break
		}
	}
	if bytes.Equal(out, source) {
		return source
	}
	return out
}
