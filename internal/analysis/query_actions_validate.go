package analysis

import (
	"errors"
	"fmt"
	"sort"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type actionDiagnostic struct {
	file, kind, code, message string
	span                      syntax.Span
}

// ValidateCodeAction permits existing diagnostics to remain while a scaffold is
// incomplete. Only explicitly planned associated-type placeholders may add an
// unresolved-name diagnostic; every other new failure rejects the whole action.
func (q *Queries) ValidateCodeAction(candidate *Result, action CodeAction) error {
	if candidate == nil || candidate.Resolved == nil || candidate.Checked == nil {
		return fmt.Errorf("candidate action has no checked semantic result")
	}
	changes := map[string][]SourceEdit{}
	for _, edit := range action.Edits {
		changes[edit.File] = append(changes[edit.File], edit)
	}
	for file := range changes {
		sort.Slice(changes[file], func(i, j int) bool { return changes[file][i].Span.Start < changes[file][j].Span.Start })
	}
	allowed := map[actionDiagnostic]int{}
	placeholders := map[int]bool{}
	for _, diagnostic := range q.result.Diagnostics {
		key, ok := actionDiagnosticKey(diagnostic)
		if !ok {
			continue
		}
		key.span = actionDiagnosticSpan(key.span, changes[key.file])
		allowed[key]++
	}
	for _, diagnostic := range candidate.Diagnostics {
		key, ok := actionDiagnosticKey(diagnostic)
		if !ok {
			return fmt.Errorf("candidate action has an unverified diagnostic: %w", diagnostic)
		}
		if allowed[key] > 0 {
			allowed[key]--
			continue
		}
		if index, ok := action.placeholderDiagnostic(candidate, key, changes[key.file]); ok {
			placeholders[index] = true
			continue
		}
		return fmt.Errorf("candidate action adds a diagnostic: %w", diagnostic)
	}
	if candidate.Err != nil && len(candidate.Diagnostics) == 0 {
		return fmt.Errorf("candidate action failed without source diagnostics: %w", candidate.Err)
	}
	if len(placeholders) != len(action.placeholders) {
		return fmt.Errorf("candidate action resolved an associated-type placeholder without a source choice")
	}
	return q.ValidateBindings(candidate, action.Edits)
}

func actionDiagnosticKey(err error) (actionDiagnostic, bool) {
	var checked check.Diagnostic
	var resolved resolve.Diagnostic
	var analyzed *Diagnostic
	switch {
	case errors.As(err, &checked):
		return actionDiagnostic{checked.File, "check", checked.Code, checked.Message, checked.Span}, true
	case errors.As(err, &resolved):
		return actionDiagnostic{resolved.File, "resolve", resolved.Code, resolved.Message, resolved.Span}, true
	case errors.As(err, &analyzed):
		return actionDiagnostic{analyzed.File, "analysis", "", analyzed.Message, analyzed.Span}, true
	default:
		return actionDiagnostic{}, false
	}
}

// Diagnostics often cover an entire impl or match. Insertions within that
// original region extend its end, unlike replacement of an identifier binding.
func actionDiagnosticSpan(span syntax.Span, edits []SourceEdit) syntax.Span {
	start, end := int64(span.Start), int64(span.End)
	for _, edit := range edits {
		delta := int64(len(edit.NewText)) - int64(edit.Span.End-edit.Span.Start)
		if edit.Span.End <= span.Start {
			start += delta
		}
		if edit.Span.Start < span.End {
			end += delta
		}
	}
	return syntax.Span{Start: uint32(start), End: uint32(end)}
}

func (a CodeAction) placeholderDiagnostic(candidate *Result, diagnostic actionDiagnostic, edits []SourceEdit) (int, bool) {
	if diagnostic.kind != "resolve" || diagnostic.code != "unresolved" || diagnostic.message != "unresolved name" {
		return 0, false
	}
	input := candidate.Sources[diagnostic.file]
	if input == nil || diagnostic.span.End < diagnostic.span.Start || int(diagnostic.span.End) > len(input.Content) {
		return 0, false
	}
	name := string(input.Content[diagnostic.span.Start:diagnostic.span.End])
	shift := int64(0)
	for _, edit := range edits {
		for index, placeholder := range a.placeholders {
			if placeholder.file != diagnostic.file || placeholder.name != name || placeholder.insertion != edit.Span.Start || placeholder.offset < 0 {
				continue
			}
			start := int64(edit.Span.Start) + shift + int64(placeholder.offset)
			if int64(diagnostic.span.Start) == start && int64(diagnostic.span.End) == start+int64(len(name)) {
				return index, true
			}
		}
		shift += int64(len(edit.NewText)) - int64(edit.Span.End-edit.Span.Start)
	}
	return 0, false
}
