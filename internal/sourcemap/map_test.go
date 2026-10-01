package sourcemap

import (
	"bytes"
	"testing"
)

func TestRawCRLFTokenSpan(t *testing.T) {
	src := []byte("package p\nvar X=`a\r\nb`\n")
	m := Identity("raw.hgo", src)
	start := bytes.IndexByte(src, '`')
	end := bytes.LastIndexByte(src, '`') + 1
	s := m.Lookup(end - 1)
	if s == nil || s.Start != start || s.End != end || s.Origin.End != end {
		t.Fatalf("raw literal span: %+v", s)
	}
}
func TestSpanForRejectsMixedStacksAndUnknownTokens(t *testing.T) {
	src := []byte("[]int")
	m := Identity("field.hgo", src)
	m.Segments[1].Stack = []Expansion{{Macro: "nested"}}
	if s := m.SpanFor(0, len(src)); s != nil {
		t.Fatalf("mixed stack fabricated span: %+v", s)
	}
	m = Identity("field.hgo", src)
	m.Segments = append(m.Segments[:1], m.Segments[2:]...)
	if s := m.SpanFor(0, len(src)); s != nil {
		t.Fatalf("uncovered token fabricated span: %+v", s)
	}
}
