package enums

import (
	"fmt"
	"go/scanner"
	"go/token"
)

// BindContextPatterns substitutes only qualified variant heads at arm depth.
// Constructor expressions inside arm bodies retain their own typed context.
func BindContextPatterns(source string, d Decl) (string, error) {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(source))
	var s scanner.Scanner
	s.Init(f, []byte(source), nil, 0)
	var ts []tok
	for {
		p, k, v := s.Scan()
		if k == token.EOF {
			break
		}
		if v == "" {
			v = k.String()
		}
		a := f.Offset(p)
		ts = append(ts, tok{kind: k, text: v, start: a, end: a + len(v)})
	}
	var changes []edit
	for _, i := range contextPatternHeads(ts) {
		t := ts[i]
		if (t.text == "Option" || t.text == "Result") && i+2 < len(ts) && ts[i+1].text == ":" && ts[i+2].text == ":" {
			if t.text != d.Family {
				return "", fmt.Errorf("pattern family conflicts with scrutinee")
			}
			changes = append(changes, edit{t.start, t.end, d.Name})
		}
	}
	for i := len(changes) - 1; i >= 0; i-- {
		e := changes[i]
		source = source[:e.start] + e.text + source[e.end:]
	}
	return source, nil
}

func contextPatternHeads(ts []tok) []int {
	depth, paren, bracket := 0, 0, 0
	head := false
	var result []int
	for i, t := range ts {
		if t.text == "{" {
			depth++
			if depth == 1 {
				head = true
			}
			continue
		}
		if t.text == "}" {
			depth--
			continue
		}
		if depth != 1 {
			continue
		}
		if paren == 0 && bracket == 0 && (t.text == "," || t.kind == token.SEMICOLON) {
			head = true
			continue
		}
		if head {
			result = append(result, i)
			head = false
		}
		switch t.text {
		case "(":
			paren++
		case ")":
			paren--
		case "[":
			bracket++
		case "]":
			bracket--
		}
	}
	return result
}
