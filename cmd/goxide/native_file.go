package main

import (
	"go/parser"
	"go/token"
)

// Native Go inputs keep their source bytes and use the isolated Go driver.
func compileNativeGoFile(filename string, source []byte) (compiledFile, error) {
	if _, err := parser.ParseFile(token.NewFileSet(), filename, source, parser.AllErrors|parser.ParseComments); err != nil {
		return compiledFile{}, err
	}
	return compiledFile{Go: source, Expanded: source}, nil
}
