package sourcemap

import (
	"bytes"
	"go/token"
	"testing"
)

func TestRawCRLFTokenSpan(t *testing.T) {
	src := []byte("package p\nvar X=`a\r\nb`\n")
	m := Identity("raw.gox", src)
	start := bytes.IndexByte(src, '`')
	end := bytes.LastIndexByte(src, '`') + 1
	s := m.Lookup(end - 1)
	if s == nil || s.Start != start || s.End != end || s.Origin.End != end {
		t.Fatalf("raw literal span: %+v", s)
	}
}
func TestSpanForRejectsMixedStacksAndUnknownTokens(t *testing.T) {
	src := []byte("[]int")
	m := Identity("field.gox", src)
	m.Segments[1].Stack = []Expansion{{Macro: "nested"}}
	if s := m.SpanFor(0, len(src)); s != nil {
		t.Fatalf("mixed stack fabricated span: %+v", s)
	}
	m = Identity("field.gox", src)
	m.Segments = append(m.Segments[:1], m.Segments[2:]...)
	if s := m.SpanFor(0, len(src)); s != nil {
		t.Fatalf("uncovered token fabricated span: %+v", s)
	}
}

func TestDiagnosticsFileMacroStackWithoutOrigin(t *testing.T) {
	stack := []Expansion{{
		Macro:      "wrap",
		Definition: Span{File: "definitions.gox", Start: 10, End: 40, Line: 2, Column: 1},
		Callsite:   Span{File: "caller.gox", Start: 50, End: 58, Line: 4, Column: 9},
	}}
	const frame = "  macro wrap defined at definitions.gox:2:1; invoked at caller.gox:4:9\n"
	source := []byte("(\n")
	origin := &Span{File: "caller.gox", Start: 52, End: 53, Line: 4, Column: 11}
	for _, tc := range []struct {
		name, file string
		segment    *Segment
		want       string
	}{
		{"unmapped with stack", "main.go", &Segment{Start: 0, End: 1, Stack: stack, Kind: "generated/unmapped"}, "goxide: main.go:1:1: generated/unmapped\n" + frame},
		{"unmapped without stack", "main.go", &Segment{Start: 0, End: 1, Kind: "generated/unmapped"}, "goxide: main.go:1:1: generated/unmapped\n"},
		{"missing segment", "main.go", nil, "goxide: main.go:1:1: generated/unmapped\n"},
		{"mapped with stack", "main.go", &Segment{Start: 0, End: 1, Origin: origin, Stack: stack, Kind: "macro"}, "goxide: caller.gox:4:11: invalid expression [macro]\n" + frame},
		{"clean current filename", "./main.go", &Segment{Start: 0, End: 1, Stack: stack, Kind: "generated/unmapped"}, "goxide: ./main.go:1:1: generated/unmapped\n" + frame},
		{"other filename", "other.go", &Segment{Start: 0, End: 1, Stack: stack, Kind: "generated/unmapped"}, ""},
		{"same basename in another directory", "elsewhere/main.go", &Segment{Start: 0, End: 1, Stack: stack, Kind: "generated/unmapped"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &Map{Source: source}
			if tc.segment != nil {
				m.Segments = []Segment{*tc.segment}
			}
			stderr := tc.file + ":1:1: invalid expression\n"
			if got := m.DiagnosticsFile(source, stderr, "main.go"); got != tc.want {
				t.Fatalf("diagnostic mismatch:\ngot:  %q\nwant: %q", got, tc.want)
			}
		})
	}
}

func TestErrorMacroStackWithoutOrigin(t *testing.T) {
	d := &Error{
		Message:   "invalid expression",
		Generated: token.Position{Filename: "main.go", Line: 1, Column: 1},
		Stack: []Expansion{{
			Macro:      "wrap",
			Definition: Span{File: "definitions.gox", Start: 10, End: 40, Line: 2, Column: 1},
			Callsite:   Span{File: "caller.gox", Start: 50, End: 58, Line: 4, Column: 9},
		}},
	}
	want := "main.go:1:1: invalid expression [generated/unmapped]\n  macro wrap defined at definitions.gox:2:1; invoked at caller.gox:4:9"
	if got := d.Error(); got != want {
		t.Fatalf("diagnostic mismatch:\ngot:  %q\nwant: %q", got, want)
	}
}
