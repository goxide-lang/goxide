// compare_ast compares complete parsed Go ASTs, not selected tokens or text.
// Numeric source positions, comments and parser caches are deliberately excluded;
// whether a token position exists (including alias '=' and call '...') is retained.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"os"
	"reflect"
)

var positionType = reflect.TypeOf(token.NoPos)

func shape(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if v.Type() == positionType {
		return v.Int() != 0
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if v.IsNil() {
			return nil
		}
		return shape(v.Elem())
	case reflect.Struct:
		m := map[string]any{"$type": v.Type().String()}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			switch field.Name {
			case "Obj", "Scope", "Unresolved", "Comments", "Doc", "Comment":
				continue
			}
			m[field.Name] = shape(v.Field(i))
		}
		return m
	case reflect.Slice:
		if v.IsNil() {
			return nil
		}
		a := make([]any, v.Len())
		for i := range a {
			a[i] = shape(v.Index(i))
		}
		return a
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint()
	default:
		panic("unhandled AST field type: " + v.Type().String())
	}
}

func shapeDigest(filename string, source any) (string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), filename, source, parser.SkipObjectResolution)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(shape(reflect.ValueOf(f)))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func compare(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: compare-ast BASELINE.go CURRENT.go")
		return 2
	}
	a, err := shapeDigest(args[0], nil)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	b, err := shapeDigest(args[1], nil)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := json.NewEncoder(stdout).Encode(map[string]any{
		"baseline": args[0], "current": args[1],
		"baseline_shape_sha256": a, "current_shape_sha256": b, "identical": a == b,
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if a != b {
		return 1
	}
	return 0
}

func main() { os.Exit(compare(os.Args[1:], os.Stdout, os.Stderr)) }
