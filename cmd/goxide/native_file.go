package main

import (
	"encoding/json"
	"go/parser"
	"go/token"

	"github.com/hxsf/goxide/internal/frontend"
	"github.com/hxsf/goxide/internal/sourcemap"
)

// A CLI-selected .go file uses Go syntax. The language frontend deliberately
// treats its filename as a display name, so selecting native Go belongs here.
// Actual build/run still uses the existing isolated single-file Go driver.
func compileNativeGoFile(filename string, source []byte) (frontend.Result, error) {
	if _, err := parser.ParseFile(token.NewFileSet(), filename, source, parser.AllErrors|parser.ParseComments); err != nil {
		return frontend.Result{}, err
	}
	mapping := sourcemap.Identity(filename, source)
	encoded, err := json.MarshalIndent(map[string]any{"diagnostics": mapping, "sourceBytes": len(source), "note": "native Go source is unchanged"}, "", "  ")
	if err != nil {
		return frontend.Result{}, err
	}
	return frontend.Result{Go: source, Expanded: source, Diagnostics: mapping, SourceMap: append(encoded, '\n')}, nil
}
