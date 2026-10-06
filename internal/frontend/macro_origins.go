package frontend

import "github.com/hxsf/goxide/internal/sourcemap"

// macroSourceMapper only maps the propagation-preparation buffer back to its
// original file. Later lowering stages have their own maps and coordinates.
type macroSourceMapper struct {
	filename string
	original []byte
	prepared *sourcemap.Map
}

func validMacroSpan(span sourcemap.Span) bool {
	return span.File != "" && span.Start >= 0 && span.End > span.Start && span.Line > 0 && span.Column > 0
}

func (m macroSourceMapper) preparedSpan(span sourcemap.Span) bool {
	if !validMacroSpan(span) || span.File != m.filename || m.prepared == nil || span.End > len(m.prepared.Source) {
		return false
	}
	// A same numeric offset from another stage is not a coordinate proof.
	return span == sourcemap.Position(m.filename, m.prepared.Source, span.Start, span.End)
}

func (m macroSourceMapper) mappedSpan(span *sourcemap.Span) bool {
	if span == nil || !validMacroSpan(*span) || span.File != m.filename || span.End > len(m.original) {
		return false
	}
	return *span == sourcemap.Position(m.filename, m.original, span.Start, span.End)
}

// Prepare transports complete token bytes. A mapped endpoint with a changed
// width cannot be one of those copied tokens, even if its offsets are in range.
func (m macroSourceMapper) copiedToken(segment *sourcemap.Segment) bool {
	return segment != nil && segment.Start >= 0 && segment.End > segment.Start && segment.End <= len(m.prepared.Source) && m.mappedSpan(segment.Origin) && segment.End-segment.Start == segment.Origin.End-segment.Origin.Start
}

// point requires a complete, explicitly mapped token range. In particular,
// generated question markers and macro parentheses cannot borrow a neighbor's
// source location. Registry-owned original spans need structural validation,
// but must not be reinterpreted using another package's same-named file.
func (m macroSourceMapper) point(span sourcemap.Span, original bool) *sourcemap.Span {
	if !validMacroSpan(span) {
		return nil
	}
	if original {
		return &span
	}
	if !m.preparedSpan(span) {
		return nil
	}
	first, last := m.prepared.Lookup(span.Start), m.prepared.Lookup(span.End-1)
	if !m.copiedToken(first) || !m.copiedToken(last) || first.Start != span.Start || last.End != span.End {
		return nil
	}
	mapped := m.prepared.SpanFor(span.Start, span.End)
	if !m.mappedSpan(mapped) {
		return nil
	}
	return mapped
}

// frame maps a known macro definition/invocation envelope, not every byte in
// it. Its real token endpoints can enclose inserted question markers and changed
// displacements. SpanFor would incorrectly reject those reliable frames.
func (m macroSourceMapper) frame(span sourcemap.Span, original bool) *sourcemap.Span {
	if !validMacroSpan(span) {
		return nil
	}
	if original {
		return &span
	}
	if !m.preparedSpan(span) {
		return nil
	}
	first, last := m.prepared.Lookup(span.Start), m.prepared.Lookup(span.End-1)
	if !m.copiedToken(first) || !m.copiedToken(last) || first.Start != span.Start || last.End != span.End {
		return nil
	}
	if first.Origin.Start > last.Origin.Start || first.Origin.End > last.Origin.End {
		return nil
	}
	mapped := *first.Origin
	mapped.End = last.Origin.End
	return &mapped
}

// An incomplete chain is not presented as a complete expansion history. Point
// provenance is independent: a generated point can still have this known chain.
func (m macroSourceMapper) stack(frames []sourcemap.Expansion) []sourcemap.Expansion {
	if len(frames) == 0 {
		return nil
	}
	stack := append([]sourcemap.Expansion(nil), frames...)
	for i := range stack {
		definition := m.frame(stack[i].Definition, stack[i].DefinitionOriginal)
		callsite := m.frame(stack[i].Callsite, stack[i].CallsiteOriginal)
		if definition == nil || callsite == nil {
			return nil
		}
		stack[i].Definition, stack[i].Callsite = *definition, *callsite
		stack[i].DefinitionOriginal, stack[i].CallsiteOriginal = true, true
	}
	return stack
}
