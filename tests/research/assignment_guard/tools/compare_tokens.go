// Compare generated Go token streams, ignoring formatting and source positions.
// Run with: go run compare_tokens.go BASELINE_DIRECTORY CURRENT_DIRECTORY
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type comparison struct {
	File        string
	Identical   bool
	TokenSHA256 string
}

func tokens(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var s scanner.Scanner
	var scanErr error
	fs := token.NewFileSet()
	s.Init(fs.AddFile(path, fs.Base(), len(data)), data, func(p token.Position, msg string) { scanErr = fmt.Errorf("%s: %s", p, msg) }, 0)
	var out strings.Builder
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		fmt.Fprintf(&out, "%d:%q\n", tok, lit)
	}
	return out.String(), scanErr
}
func main() {
	if len(os.Args) != 3 {
		panic("usage: compare_tokens.go BASELINE_DIRECTORY CURRENT_DIRECTORY")
	}
	rows := []comparison{}
	same := true
	err := filepath.WalkDir(os.Args[1], func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, e := filepath.Rel(os.Args[1], path)
		if e != nil {
			return e
		}
		a, e := tokens(path)
		if e != nil {
			return e
		}
		b, e := tokens(filepath.Join(os.Args[2], rel))
		if e != nil {
			return e
		}
		rows = append(rows, comparison{rel, a == b, fmt.Sprintf("%x", sha256.Sum256([]byte(b)))})
		same = same && a == b
		return nil
	})
	if err != nil {
		panic(err)
	}
	// Tests are installed separately by the research runner; compare the full
	// generated production file set in both directions.
	err = filepath.WalkDir(os.Args[2], func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, e := filepath.Rel(os.Args[2], path)
		if e != nil {
			return e
		}
		_, e = os.Stat(filepath.Join(os.Args[1], rel))
		return e
	})
	if err != nil {
		panic(err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rows); err != nil {
		panic(err)
	}
	if len(rows) == 0 || !same {
		os.Exit(1)
	}
}
