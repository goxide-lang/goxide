package enums

import (
	"encoding/json"
	"fmt"
	"go/scanner"
	"go/token"
	"strings"

	"github.com/hxsf/goxide/internal/sourcemap"
)

// contextBody is compiler IR. Its token origins are copied at serialization,
// before contextual type inference delays lowering the arm expressions.
type contextBody struct {
	Version      int
	Mode, Borrow string
	Source       string
	Segments     []sourcemap.Segment
	Stages       []string
}

func encodeContextBody(source []byte, start, end int, mapping *sourcemap.Map, mode, borrow string) (string, error) {
	body := contextBody{Version: 2, Mode: mode, Borrow: borrow, Source: string(source[start:end])}
	if mapping != nil {
		body.Segments = mapping.Copy(start, end, 0)
		body.Stages = append([]string(nil), mapping.Stages...)
	}
	data, err := json.Marshal(body)
	return string(data), err
}

// DecodeContextBody restores the exact source fragment recorded by the enum
// lowerer. Plain strings are not an alternate IR representation.
func DecodeContextBody(encoded string) ([]byte, *sourcemap.Map, string, string, error) {
	var body contextBody
	if err := json.Unmarshal([]byte(encoded), &body); err != nil {
		return nil, nil, "", "", fmt.Errorf("invalid contextual match payload: %w", err)
	}
	if body.Version != 2 {
		return nil, nil, "", "", fmt.Errorf("invalid contextual match payload version")
	}
	if (body.Mode != "value" && body.Mode != "read" && body.Mode != "write") || (body.Borrow != "" && body.Borrow != "read" && body.Borrow != "write") {
		return nil, nil, "", "", fmt.Errorf("invalid contextual match binding mode")
	}
	source := []byte(body.Source)
	for _, s := range body.Segments {
		if s.Start < 0 || s.End < s.Start || s.End > len(source) {
			return nil, nil, "", "", fmt.Errorf("invalid contextual match token range")
		}
	}
	return source, &sourcemap.Map{Source: source, Segments: body.Segments, Stages: body.Stages}, body.Mode, body.Borrow, nil
}

// BindContextPatterns substitutes only qualified variant heads at arm depth.
// Constructor expressions inside arm bodies retain their own typed context.
func BindContextPatterns(source string, d Decl) (string, error) {
	return BindContextPatternsMapped(source, d, nil)
}

// BindContextPatternsMapped transports only copied ranges. Replaced family
// names are generated bindings, not source tokens with a coincident spelling.
func BindContextPatternsMapped(source string, d Decl, mapping *sourcemap.Map) (string, error) {
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
	if mapping != nil {
		fragments := sourcemap.Fragments{Input: mapping}
		var out strings.Builder
		at := 0
		for _, e := range changes {
			out.WriteString(fragments.Copy(at, e.start))
			out.WriteString(e.text)
			at = e.end
		}
		out.WriteString(fragments.Copy(at, len(source)))
		data, next := fragments.Finish(out.String(), "contextual pattern binding")
		*mapping = *next
		source = string(data)
	} else {
		for i := len(changes) - 1; i >= 0; i-- {
			e := changes[i]
			source = source[:e.start] + e.text + source[e.end:]
		}
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
