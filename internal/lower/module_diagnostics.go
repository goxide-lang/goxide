package lower

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type moduleDiagnostic struct {
	message string
	cause   error
}

func (e *moduleDiagnostic) Error() string { return e.message }
func (e *moduleDiagnostic) Unwrap() error { return e.cause }

// FormatSourceDiagnostic prints only source-stage diagnostics. Native Go errors
// retain their generated positions because this pipeline has no Go source map.
func FormatSourceDiagnostic(err error, files map[string]*syntax.File, displayNames map[string]string, expanded *expand.Result) error {
	if err == nil {
		return nil
	}
	var formatted *moduleDiagnostic
	if errors.As(err, &formatted) {
		return err
	}
	name, span, message := "", syntax.Span{}, ""
	var definition, call expand.Origin
	var expn syntax.ExpnID
	var expansion *expand.Diagnostic
	var resolved resolve.Diagnostic
	var checked check.Diagnostic
	var lowered *Diagnostic
	switch {
	case errors.As(err, &expansion):
		message, definition, call = expansion.Message, expansion.Definition, expansion.Call
		expn = expansion.Expn
	case errors.As(err, &resolved):
		name, span, message = resolved.File, resolved.Span, resolved.Message
		definition, call = resolved.Definition, resolved.Call
		expn = resolved.Origin.Expn
		if resolved.Origin.File != "" {
			name, span = resolved.Origin.File, resolved.Origin.Span
		}
	case errors.As(err, &checked):
		name, span, message = checked.File, checked.Span, checked.Message
		expn = checked.Origin.Expn
		if checked.Origin.File != "" {
			name, span = checked.Origin.File, checked.Origin.Span
		}
	case errors.As(err, &lowered):
		name, span, message = lowered.File, lowered.Span, lowered.Message
		definition, call = lowered.Definition, lowered.Call
		expn = lowered.Origin.Expn
		if lowered.Origin.File != "" {
			name, span = lowered.Origin.File, lowered.Origin.Span
		}
	default:
		return err
	}
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fileNames := make(map[*syntax.File]string, len(files))
	for _, key := range keys {
		fileNames[files[key]] = key
	}
	position := func(name string, span syntax.Span) string {
		file := files[name]
		if file == nil {
			return ""
		}
		line, column := 1, int(span.Start)+1
		for index, offset := range file.LineStarts {
			if offset > span.Start {
				break
			}
			line, column = index+1, int(span.Start-offset)+1
		}
		if displayNames[name] != "" {
			name = displayNames[name]
		}
		return fmt.Sprintf("%s:%d:%d", name, line, column)
	}
	originPosition := func(origin expand.Origin) string {
		return position(fileNames[origin.File], origin.Span)
	}
	if name == "" {
		origin := definition
		if origin.File == nil {
			origin = call
		}
		name, span = fileNames[origin.File], origin.Span
	}
	location := position(name, span)
	if location == "" {
		return err
	}
	var notes []string
	if expanded != nil && expn != 0 {
		for _, id := range expanded.Expansions[expn].Stack {
			frame := expanded.Expansions[id]
			defined, called := originPosition(frame.Definition), originPosition(frame.Call)
			macro := ""
			if frame.Definition.File != nil {
				for _, item := range frame.Definition.File.Items {
					if decl, ok := item.(*syntax.MacroRulesDecl); ok && decl.Span() == frame.Definition.Span {
						macro = " " + decl.Name.Name
						break
					}
				}
			}
			if defined != "" && called != "" {
				notes = append(notes, "macro"+macro+" defined at "+defined+"; called at "+called)
			}
		}
	}
	if len(notes) == 0 && definition.File != nil && call.File != nil {
		defined, called := originPosition(definition), originPosition(call)
		if defined != "" && called != "" && defined != called {
			notes = append(notes, "macro definition "+defined+"; call "+called)
		}
	}
	if len(notes) != 0 {
		message += "\n  " + strings.Join(notes, "\n  ")
	}
	return &moduleDiagnostic{message: location + ": " + message, cause: err}
}
