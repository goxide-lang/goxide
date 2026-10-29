package analysis

import (
	"fmt"

	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type Severity uint8

const Error Severity = 0

type Diagnostic struct {
	Definition, Call expand.Origin
	Severity         Severity
	File             string
	Source           *syntax.File
	Span             syntax.Span
	Message          string
	Origin           resolve.Origin
}

func (d *Diagnostic) Error() string {
	message := fmt.Sprintf("%s:%d:%d: %s", d.File, d.Span.Start, d.Span.End, d.Message)
	if d.Call.File != nil {
		message += fmt.Sprintf(" (macro definition %d:%d, call %d:%d)", d.Definition.Span.Start, d.Definition.Span.End, d.Call.Span.Start, d.Call.Span.End)
	}
	return message
}
