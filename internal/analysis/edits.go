package analysis

import syntax "github.com/goxide-lang/goxide/internal/syntax/ast"

// SourceEdit replaces an original source byte range in this analysis snapshot.
// File is the canonical source key, not a filesystem path or generated Go file.
type SourceEdit struct {
	File    string
	Span    syntax.Span
	NewText string
}
