package analysis

import (
	"errors"
	"fmt"
	"go/types"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type moduleDiagnostic struct {
	location string
	message  string
	notes    string
	cause    error
}

func (e *moduleDiagnostic) Error() string { return e.location + ": " + e.message + e.notes }
func (e *moduleDiagnostic) Unwrap() error { return e.cause }

type generatedDisplayDiagnostic struct {
	message string
	cause   error
}

func (e *generatedDisplayDiagnostic) Error() string { return e.message }
func (e *generatedDisplayDiagnostic) Unwrap() error { return e.cause }

// SummarizeProcDiagnostics retains the source location and expansion trace after
// a caller has already displayed every procedural diagnostic in source order.
func SummarizeProcDiagnostics(err error) error {
	var formatted *moduleDiagnostic
	var expansion *expand.Diagnostic
	if !errors.As(err, &formatted) || !errors.As(err, &expansion) {
		return err
	}
	return &moduleDiagnostic{location: formatted.location, message: "procedural macro expansion failed", notes: formatted.notes, cause: err}
}

// FormatSourceDiagnostic prints source-stage diagnostics with authored positions.
// Native Go errors use display filenames but retain their generated line and
// column because this pipeline has no Go source map.
func FormatSourceDiagnostic(err error, files map[string]*syntax.File, displayNames map[string]string, expanded *expand.Result) error {
	if err == nil {
		return nil
	}
	var formatted *moduleDiagnostic
	if errors.As(err, &formatted) {
		return err
	}
	name, span, message := "", syntax.Span{}, ""
	var definition, call, display expand.Origin
	var expn syntax.ExpnID
	var expansion *expand.Diagnostic
	var registration *procmacro.Diagnostic
	var resolved resolve.Diagnostic
	var checked check.Diagnostic
	var analyzed *Diagnostic
	switch {
	case errors.As(err, &registration):
		name, span, message = registration.File, registration.Span, registration.Message
	case errors.As(err, &expansion):
		message, definition, call = expansion.Message, expansion.Definition, expansion.Call
		display = expansion.Location
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
	case errors.As(err, &analyzed):
		name, span, message = analyzed.File, analyzed.Span, analyzed.Message
		definition, call = analyzed.Definition, analyzed.Call
		expn = analyzed.Origin.Expn
		if analyzed.Origin.File != "" {
			name, span = analyzed.Origin.File, analyzed.Origin.Span
		}
	default:
		return formatGeneratedDisplayName(err, displayNames)
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
	if displayName, ok := fileNames[display.File]; ok {
		name, span = displayName, display.Span
	}
	position := func(name string, span syntax.Span) string {
		file := files[name]
		if file == nil && analyzed != nil && analyzed.File == name {
			file = analyzed.Source
		}
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
		if files[name] != nil && displayNames[name] != "" {
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
	trace := ""
	if len(notes) != 0 {
		trace = "\n  " + strings.Join(notes, "\n  ")
	}
	return &moduleDiagnostic{location: location, message: message, notes: trace, cause: err}
}

func formatGeneratedDisplayName(err error, displayNames map[string]string) error {
	var diagnostic types.Error
	if !errors.As(err, &diagnostic) || diagnostic.Fset == nil {
		return err
	}
	position := diagnostic.Fset.Position(diagnostic.Pos)
	name := displayNames[position.Filename]
	if name == "" || name == position.Filename {
		return err
	}
	original, message := diagnostic.Error(), err.Error()
	if !strings.HasSuffix(message, original) {
		return err
	}
	location := position.String()
	position.Filename = name
	// Change only the structured diagnostic's filename. Keep wrapper context,
	// message text and the original error/position available to other consumers.
	return &generatedDisplayDiagnostic{
		message: strings.TrimSuffix(message, original) + position.String() + strings.TrimPrefix(original, location),
		cause:   err,
	}
}
