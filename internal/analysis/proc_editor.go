package analysis

import (
	"errors"
	"path"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// SourceKey maps a producer source to this consumer's plan-specific namespace.
// Ordinary producer dependencies are retained only as diagnostic source bytes;
// they never become runtime declarations in the consumer's graph.
func (library *ProcLibrary) SourceKey(file string) string {
	if path.Dir(file) == library.compiledPath {
		return path.Join(library.PackagePath, path.Base(file))
	}
	if failure := library.Unavailable; failure != nil {
		for _, module := range failure.Modules {
			for _, dependency := range module.ProcLibraries {
				if strings.HasPrefix(file, dependency.PackagePath+"/") {
					return file
				}
			}
		}
	}
	return path.Join(library.PackagePath, "goxide_diagnostics", file)
}

func (library *ProcLibrary) sourceDiagnostic(err error) error {
	name, span, message := "", syntax.Span{}, ""
	var analyzed *Diagnostic
	var checked check.Diagnostic
	var resolved resolve.Diagnostic
	var registration *procmacro.Diagnostic
	var expansion *expand.Diagnostic
	switch {
	case errors.As(err, &analyzed):
		name, span, message = analyzed.File, analyzed.Span, analyzed.Message
	case errors.As(err, &checked):
		name, span, message = checked.File, checked.Span, checked.Message
	case errors.As(err, &resolved):
		name, span, message = resolved.File, resolved.Span, resolved.Message
	case errors.As(err, &registration):
		name, span, message = registration.File, registration.Span, registration.Message
	case errors.As(err, &expansion):
		span, message = expansion.Location.Span, expansion.Message
		for key, file := range library.Unavailable.Files {
			if file == expansion.Location.File {
				name = key
				break
			}
		}
	default:
		return err
	}
	if name == "" {
		return err
	}
	name = library.SourceKey(name)
	return &Diagnostic{File: name, Span: span, Message: message, Origin: resolve.Origin{File: name, Span: span}}
}
