package typecheck

import (
	"bytes"
	"fmt"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/expand"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
)

// DiagnosticSource associates a logical compiler key with the original input.
// Name is the filename displayed to the caller; AST is the unexpanded tree.
type DiagnosticSource struct {
	Name string
	Text []byte
	AST  *syntax.File
}

func sourcePosition(source []byte, offset uint32) (int, int) {
	end := min(int(offset), len(source))
	prefix := source[:end]
	return bytes.Count(prefix, []byte{'\n'}) + 1, end - bytes.LastIndexByte(prefix, '\n')
}

func ParserError(name string, source []byte, diagnostic parser.Diagnostic) error {
	line, column := sourcePosition(source, diagnostic.Span.Start)
	return fmt.Errorf("%s:%d:%d: %s", name, line, column, diagnostic.Message)
}

// SourceError formats source-stage byte spans as filename:line:column. It does
// not invent a source map for errors reported against generated Go.
func SourceError(err error, sources map[string]DiagnosticSource, expanded *expand.Result) error {
	if err == nil {
		return nil
	}
	files := make(map[string]*syntax.File, len(sources))
	names := make(map[string]string, len(sources))
	for name, source := range sources {
		files[name], names[name] = source.AST, source.Name
	}
	return analysis.FormatSourceDiagnostic(err, files, names, expanded)
}
