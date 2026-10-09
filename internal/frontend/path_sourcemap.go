package frontend

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/hxsf/goxide/internal/sourcemap"
)

func sourceMapAfterBlanks(file string, before, after []byte) *sourcemap.Map {
	m := sourcemap.Identity(file, before)
	out := &sourcemap.Map{Source: after, Stages: []string{"use declarations"}}
	for _, s := range m.Segments {
		if s.End <= len(after) && bytes.Equal(before[s.Start:s.End], after[s.Start:s.End]) {
			out.Segments = append(out.Segments, s)
		}
	}
	return out
}

func (u *usePackage) sourceMap(file string) *sourcemap.Map {
	if m := u.preparedMaps[file]; m != nil {
		return &sourcemap.Map{Source: m.Source, Segments: append([]sourcemap.Segment(nil), m.Segments...), Stages: append([]string(nil), m.Stages...)}
	}
	return sourcemap.Identity(file, u.prepared[file])
}

// applyUseEdits transports unchanged byte ranges, never matches repeated text.
// A route substitution may carry the replaced terminal identifier's origin;
// callers know its semantic binding before requesting this mapping.
func applyUseEdits(source []byte, mapping *sourcemap.Map, edits []useEdit, stage string, terminalOrigin bool, semanticKind ...string) ([]byte, error) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	out := []byte{}
	next := &sourcemap.Map{}
	cursor := 0
	for _, e := range edits {
		if e.start < cursor || e.end < e.start || e.end > len(source) {
			return nil, fmt.Errorf("overlapping or invalid %s edits", stage)
		}
		if mapping != nil {
			next.Segments = append(next.Segments, mapping.Copy(cursor, e.start, len(out))...)
		}
		out = append(out, source[cursor:e.start]...)
		start := len(out)
		out = append(out, e.text...)
		if mapping != nil && terminalOrigin && e.end > e.start {
			if len(semanticKind) > 0 {
				// A renamed namespace is a semantic envelope, never a copied
				// token. Macro frames may use its full original path extent;
				// point diagnostics and selector spelling must not do so.
				if origin := mapping.SpanFor(e.start, e.end); origin != nil {
					prior := mapping.Lookup(e.start)
					next.Attach(start, len(out), origin, prior.Stack, semanticKind[0])
				}
			} else if prior := mapping.Lookup(e.end - 1); prior != nil {
				next.Attach(start, len(out), prior.Origin, prior.Stack, prior.Kind)
			}
		}
		cursor = e.end
	}
	if mapping != nil {
		next.Segments = append(next.Segments, mapping.Copy(cursor, len(source), len(out))...)
	}
	out = append(out, source[cursor:]...)
	if mapping != nil {
		next.Source = out
		next.Stages = append(append([]string(nil), mapping.Stages...), stage)
		*mapping = *next
	}
	return out, nil
}

func addUseImportsMapped(file string, source []byte, imports map[string]string, mapping *sourcemap.Map) ([]byte, error) {
	out, err := addUseImports(file, source, imports)
	if err != nil || mapping == nil || len(out) == len(source) {
		return out, err
	}
	f, err := parseSourceImports(file, source)
	if err != nil {
		return nil, err
	}
	at := int(f.Name.End()) - 1
	return applyUseEdits(source, mapping, []useEdit{{at, at, string(out[at : at+len(out)-len(source)])}}, "use imports", false)
}
