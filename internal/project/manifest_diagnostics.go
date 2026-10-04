package project

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

func manifestDecodeError(filename string, err error) error {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		var parts []string
		for _, e := range strict.Errors {
			line, col := e.Position()
			parts = append(parts, fmt.Sprintf("%s:%d:%d: unknown field/table %s", filename, line, col, strings.Join(e.Key(), ".")))
		}
		return errors.New(strings.Join(parts, "\n"))
	}
	var decode *toml.DecodeError
	if errors.As(err, &decode) {
		line, col := decode.Position()
		return fmt.Errorf("%s:%d:%d: %s", filename, line, col, decode.Error())
	}
	return fmt.Errorf("%s:1:1: %w", filename, err)
}

// Use the library's TOML AST for positions, including quoted/dotted keys and inline tables.
// This does not parse TOML independently; the strict decoder has already validated it.
func validateNames(filename string, data []byte) error {
	var p unstable.Parser
	p.Reset(data)
	var table []string
	var walk func(*unstable.Node, []string) error
	walk = func(n *unstable.Node, prefix []string) error {
		key := append([]string(nil), prefix...)
		it := n.Key()
		for it.Next() {
			k := it.Node()
			key = append(key, string(k.Data))
			if !manifestKeyKnown(key) {
				pos := p.Shape(k.Raw).Start
				return fmt.Errorf("%s:%d:%d: unknown field/table %s (keys are case-sensitive)", filename, pos.Line, pos.Column, strings.Join(key, "."))
			}
			if len(key) == 2 && (key[0] == "dependencies" || key[0] == "features") && !ident.MatchString(key[1]) {
				pos := p.Shape(k.Raw).Start
				return fmt.Errorf("%s:%d:%d: invalid %s name %q", filename, pos.Line, pos.Column, key[0], key[1])
			}
		}
		if n.Kind == unstable.KeyValue {
			var value func(*unstable.Node) error
			value = func(v *unstable.Node) error {
				if v.Kind == unstable.InlineTable {
					it := v.Children()
					for it.Next() {
						if err := walk(it.Node(), key); err != nil {
							return err
						}
					}
				}
				if v.Kind == unstable.Array {
					it := v.Children()
					for it.Next() {
						if err := value(it.Node()); err != nil {
							return err
						}
					}
				}
				return nil
			}
			if err := value(n.Value()); err != nil {
				return err
			}
		}
		return nil
	}
	for p.NextExpression() {
		n := p.Expression()
		switch n.Kind {
		case unstable.Table, unstable.ArrayTable:
			if err := walk(n, nil); err != nil {
				return err
			}
			table = nil
			it := n.Key()
			for it.Next() {
				table = append(table, string(it.Node().Data))
			}
		case unstable.KeyValue:
			if err := walk(n, table); err != nil {
				return err
			}
		}
	}
	return nil
}

// Decoder struct matching is case-insensitive; enforce exact schema tags using
// the parser AST, including inline tables and arrays, without a second TOML parser.
func manifestKeyKnown(path []string) bool {
	if len(path) == 3 && path[0] == "package" && (path[1] == "go" || path[1] == "toolchain") && path[2] == "workspace" {
		return true
	}
	typ := reflect.TypeOf(manifestDocument{})
	for _, key := range path {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
			typ = typ.Elem()
		}
		if typ.Kind() == reflect.Map {
			typ = typ.Elem()
			continue
		}
		if typ.Kind() != reflect.Struct {
			return false
		}
		found := false
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			name := strings.Split(f.Tag.Get("toml"), ",")[0]
			if name == key {
				typ = f.Type
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
