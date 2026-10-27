package analysis

import (
	"fmt"
	"go/token"
	"sort"
	"strings"

	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// ValidateBindings checks that an edit preserves every reliable authored source
// reference. Definitions are compared by their remapped source positions, not
// names or AST IDs, both of which may change during a fresh analysis.
func (q *Queries) ValidateBindings(candidate *Result, edits []SourceEdit) error {
	next := NewQueries(candidate)
	changes := map[string][]SourceEdit{}
	for _, edit := range edits {
		changes[edit.File] = append(changes[edit.File], edit)
	}
	for name := range changes {
		sort.Slice(changes[name], func(i, j int) bool { return changes[name][i].Span.Start < changes[name][j].Span.Start })
	}
	files := make([]string, 0, len(q.entries))
	for name := range q.entries {
		files = append(files, name)
	}
	sort.Strings(files)
	for _, name := range files {
		input := q.result.Sources[name]
		if input == nil {
			continue
		}
		seen := map[syntax.Span]bool{}
		for _, entry := range q.entries[name] {
			location := entry.location
			span := location.Span
			if location.Generated || seen[span] || span.End < span.Start || int(span.End) > len(input.Content) {
				continue
			}
			seen[span] = true
			if !token.IsIdentifier(strings.TrimPrefix(string(input.Content[span.Start:span.End]), "r#")) {
				continue
			}
			before, err := q.Definition(name, span.Start)
			if err != nil || len(before) != 1 || before[0].File == "" || before[0].Generated {
				continue // Only facts certified by the original snapshot are comparable.
			}
			mapped, ok := remapBindingLocation(location, changes)
			if !ok {
				return fmt.Errorf("candidate replaces a source reference at %s:%d", name, span.Start)
			}
			definition, ok := remapBindingLocation(before[0], changes)
			if !ok {
				return fmt.Errorf("candidate replaces a source declaration at %s:%d", before[0].File, before[0].Span.Start)
			}
			after, err := next.Definition(mapped.File, mapped.Span.Start)
			if err != nil || len(after) != 1 || after[0].Generated || after[0].File != definition.File || after[0].Span != definition.Span {
				return fmt.Errorf("candidate changes the source binding at %s:%d", name, span.Start)
			}
		}
	}
	return nil
}

func remapBindingLocation(location Location, edits map[string][]SourceEdit) (Location, bool) {
	start, end := int64(location.Span.Start), int64(location.Span.End)
	shift := int64(0)
	for _, edit := range edits[location.File] {
		a, b := int64(edit.Span.Start), int64(edit.Span.End)
		delta := int64(len(edit.NewText)) - (b - a)
		if b <= start {
			shift += delta
			continue
		}
		if a >= end {
			break
		}
		if a != start || b != end {
			return Location{}, false
		}
		end += delta
		break
	}
	location.Span = syntax.Span{Start: uint32(start + shift), End: uint32(end + shift)}
	return location, true
}
