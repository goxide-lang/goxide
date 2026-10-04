// Package sourcemap records token provenance through compiler transformations.
package sourcemap

import (
	"fmt"
	"go/scanner"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

type Span struct {
	File                     string
	Start, End, Line, Column int
}
type Expansion struct {
	Macro                string
	Definition, Callsite Span
	// Package definitions may use original file coordinates while caller
	// tokens still refer to the propagation preparation buffer.
	DefinitionOriginal bool `json:",omitempty"`
	CallsiteOriginal   bool `json:",omitempty"`
}
type Segment struct {
	Start, End int
	Origin     *Span       `json:",omitempty"`
	Stack      []Expansion `json:",omitempty"`
	Kind       string
}
type Map struct {
	Source   []byte `json:"-"`
	Segments []Segment
	Stages   []string
}
type lexeme struct {
	start, end int
	text       string
}

func tokens(src []byte) []lexeme {
	fs := token.NewFileSet()
	f := fs.AddFile("", -1, len(src))
	var s scanner.Scanner
	s.Init(f, src, func(token.Position, string) {}, 0)
	var out []lexeme
	for {
		p, k, v := s.Scan()
		if k == token.EOF {
			break
		}
		if k == token.SEMICOLON {
			continue
		}
		if v == "" {
			v = k.String()
		}
		a := f.Offset(p)
		end := a + len(v)
		if k == token.STRING && a < len(src) && src[a] == '`' {
			end = a + 1
			for end < len(src) && src[end] != '`' {
				end++
			}
			if end < len(src) {
				end++
			}
		}
		if end > len(src) {
			end = len(src)
		}
		out = append(out, lexeme{a, end, string(src[a:end])})
	}
	return out
}
func Position(file string, src []byte, start, end int) Span {
	if start < 0 {
		start = 0
	}
	if start > len(src) {
		start = len(src)
	}
	prefix := string(src[:start])
	line := strings.Count(prefix, "\n") + 1
	col := start - strings.LastIndex(prefix, "\n")
	return Span{file, start, end, line, col}
}
func Identity(file string, src []byte) *Map {
	m := &Map{Source: src}
	for _, t := range tokens(src) {
		p := Position(file, src, t.start, t.end)
		m.Segments = append(m.Segments, Segment{Start: t.start, End: t.end, Origin: &p, Kind: "source"})
	}
	return m
}
func (m *Map) Lookup(offset int) *Segment {
	for i := len(m.Segments) - 1; i >= 0; i-- {
		s := &m.Segments[i]
		if offset >= s.Start && offset < s.End {
			return s
		}
	}
	return nil
}

// Compose retains origins only when the entire token stream is unchanged
// (formatting), or the entire source is unchanged. Changed passes must provide
// explicit spans; equal repeated text is insufficient evidence of provenance.
func (m *Map) Compose(before, after []byte, stage string) *Map {
	a, b := tokens(before), tokens(after)
	next := &Map{Source: after, Stages: append(append([]string{}, m.Stages...), stage)}
	equal := len(a) == len(b)
	if equal {
		for i := range a {
			if a[i].text != b[i].text {
				equal = false
				break
			}
		}
	}
	for j, t := range b {
		s := Segment{Start: t.start, End: t.end, Kind: "generated/unmapped"}
		if equal {
			if old := m.Lookup(a[j].start); old != nil {
				s.Origin = old.Origin
				s.Stack = old.Stack
				s.Kind = old.Kind
			}
		}
		next.Segments = append(next.Segments, s)
	}
	return next
}
func (m *Map) Attach(start, end int, origin *Span, stack []Expansion, kind string) {
	m.Segments = append(m.Segments, Segment{start, end, origin, stack, kind})
}

var diagnostic = regexp.MustCompile(`(?m)^([^\n]+\.go):([0-9]+):([0-9]+): (.*)$`)

// Diagnostics supplements, never replaces, the original Go compiler stderr.
func (m *Map) Diagnostics(src []byte, stderr string) string {
	return m.DiagnosticsFile(src, stderr, "main.go")
}

// DiagnosticsFile supplements errors for one exact generated file.
func (m *Map) DiagnosticsFile(src []byte, stderr, filename string) string {
	var out strings.Builder
	for _, v := range diagnostic.FindAllStringSubmatch(stderr, -1) {
		if filepath.Clean(v[1]) != filepath.Clean(filename) {
			continue
		}
		line, _ := strconv.Atoi(v[2])
		col, _ := strconv.Atoi(v[3])
		off := 0
		for n := 1; n < line; n++ {
			p := strings.IndexByte(string(src[off:]), '\n')
			if p < 0 {
				off = len(src)
				break
			}
			off += p + 1
		}
		off += col - 1
		s := m.Lookup(off)
		if s == nil || s.Origin == nil {
			fmt.Fprintf(&out, "goxide: %s:%s:%s: generated/unmapped\n", v[1], v[2], v[3])
			continue
		}
		p := s.Origin
		fmt.Fprintf(&out, "goxide: %s:%d:%d: %s [%s]\n", p.File, p.Line, p.Column, v[4], s.Kind)
		for _, f := range s.Stack {
			fmt.Fprintf(&out, "  macro %s defined at %s:%d:%d; invoked at %s:%d:%d\n", f.Macro, f.Definition.File, f.Definition.Line, f.Definition.Column, f.Callsite.File, f.Callsite.Line, f.Callsite.Column)
		}
	}
	return out.String()
}

// Copy transports a known unchanged byte range to a known destination.
func (m *Map) Copy(start, end, destination int) []Segment {
	var out []Segment
	for _, s := range m.Segments {
		if s.Start >= start && s.End <= end {
			s.Start += destination - start
			s.End += destination - start
			out = append(out, s)
		}
	}
	return out
}

// SpanFor returns a contiguous original span only when every recorded token in
// the requested range has the same byte displacement and expansion context.
func (m *Map) SpanFor(start, end int) *Span {
	if start < 0 || end > len(m.Source) || start >= end {
		return nil
	}
	first := m.Lookup(start)
	last := m.Lookup(end - 1)
	if first == nil || last == nil || first.Origin == nil || last.Origin == nil {
		return nil
	}
	delta := first.Origin.Start - first.Start
	for _, t := range tokens(m.Source[start:end]) {
		s := m.Lookup(start + t.start)
		if s == nil || s.Origin == nil || s.End < start+t.end || s.Origin.File != first.Origin.File || s.Origin.Start-s.Start != delta || !reflect.DeepEqual(s.Stack, first.Stack) {
			return nil
		}
	}
	p := *first.Origin
	p.End = last.Origin.End
	return &p
}

type Error struct {
	Message   string
	Origin    *Span
	Stack     []Expansion
	Generated token.Position
}

func (e *Error) Error() string {
	var out strings.Builder
	if e.Origin == nil {
		fmt.Fprintf(&out, "%s: %s [generated/unmapped]", e.Generated, e.Message)
	} else {
		fmt.Fprintf(&out, "%s:%d:%d: %s (source span [%d,%d))", e.Origin.File, e.Origin.Line, e.Origin.Column, e.Message, e.Origin.Start, e.Origin.End)
	}
	for _, frame := range e.Stack {
		fmt.Fprintf(&out, "\n  macro %s defined at %s:%d:%d; invoked at %s:%d:%d", frame.Macro, frame.Definition.File, frame.Definition.Line, frame.Definition.Column, frame.Callsite.File, frame.Callsite.Line, frame.Callsite.Column)
	}
	return out.String()
}

// Format composes a go/format pass. go/format may remove redundant parentheses;
// if this occurs, only non-parenthesis tokens in the otherwise identical ordered
// stream retain origins. No matching by token value or occurrence search occurs.
func (m *Map) Format(before, after []byte, stage string) *Map {
	next := m.Compose(before, after, stage)
	a, b := tokens(before), tokens(after)
	filter := func(ts []lexeme) []lexeme {
		out := []lexeme{}
		for _, t := range ts {
			if t.text != "(" && t.text != ")" {
				out = append(out, t)
			}
		}
		return out
	}
	a, b = filter(a), filter(b)
	if len(a) != len(b) {
		return next
	}
	for i := range a {
		if a[i].text != b[i].text {
			return next
		}
	}
	for i, t := range b {
		if s := m.Lookup(a[i].start); s != nil {
			next.Attach(t.start, t.end, s.Origin, s.Stack, s.Kind)
		}
	}
	return next
}
