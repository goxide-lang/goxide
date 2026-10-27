package lower

import (
	"bytes"
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"reflect"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/source"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// recordSource is called where a source construct is lowered, never inferred
// from its token.Pos. Fresh temporaries and backend-only helpers stay unmapped.
func (e *emitter) recordSource(node goast.Node, original syntax.Node) {
	if e.sourceNodes == nil || node == nil || original == nil || reflect.ValueOf(node).IsNil() || reflect.ValueOf(original).IsNil() {
		return
	}
	if _, identifier := node.(*goast.Ident); identifier {
		switch original.(type) {
		case *syntax.Ident, *syntax.BoolLit:
		default:
			return
		}
	}
	if e.sourceNodes[node] == nil {
		e.sourceNodes[node] = original
	}
}

func (e *emitter) copySource(from, to goast.Node) {
	if original := e.sourceNodes[from]; original != nil {
		e.sourceNodes[to] = original
	}
}

// alignGoNodes follows the emitted tree's structure in the reparsed final Go.
// Formatting can insert parentheses; import declarations can be reordered.
// Neither case needs a source-position or text-distance heuristic. Unknown
// structural changes fail closed: their subtree receives no transferred map.
func alignGoNodes(before, after goast.Node, pair func(goast.Node, goast.Node)) {
	if nilGoNode(before) || nilGoNode(after) {
		return
	}
	if reflect.TypeOf(before) != reflect.TypeOf(after) {
		if paren, ok := after.(*goast.ParenExpr); ok {
			alignGoNodes(before, paren.X, pair)
		} else if paren, ok := before.(*goast.ParenExpr); ok {
			alignGoNodes(paren.X, after, pair)
		}
		return
	}
	if a, ok := before.(*goast.Ident); ok && a.Name != after.(*goast.Ident).Name {
		return
	}
	pair(before, after)
	if file, ok := before.(*goast.File); ok {
		other := after.(*goast.File)
		alignGoNodes(file.Name, other.Name, pair)
		imports := map[string]goast.Decl{}
		var ordinaryBefore, ordinaryAfter []goast.Decl
		for _, declaration := range file.Decls {
			if key := importDeclarationKey(declaration); key != "" {
				imports[key] = declaration
			} else {
				ordinaryBefore = append(ordinaryBefore, declaration)
			}
		}
		for _, declaration := range other.Decls {
			if key := importDeclarationKey(declaration); key != "" {
				alignGoNodes(imports[key], declaration, pair)
			} else {
				ordinaryAfter = append(ordinaryAfter, declaration)
			}
		}
		if len(ordinaryBefore) == len(ordinaryAfter) {
			for i, declaration := range ordinaryBefore {
				alignGoNodes(declaration, ordinaryAfter[i], pair)
			}
		}
		return
	}
	var children func(reflect.Value, reflect.Value)
	children = func(a, b reflect.Value) {
		if !a.IsValid() || !b.IsValid() || a.Type() != b.Type() {
			return
		}
		if a.Kind() == reflect.Slice {
			if a.Len() == b.Len() {
				for i := 0; i < a.Len(); i++ {
					children(a.Index(i), b.Index(i))
				}
			}
			return
		}
		if a.Kind() == reflect.Pointer || a.Kind() == reflect.Interface {
			if a.IsNil() || b.IsNil() {
				return
			}
			old, ok := a.Interface().(goast.Node)
			current, other := b.Interface().(goast.Node)
			if ok && other {
				alignGoNodes(old, current, pair)
			}
		}
	}
	a, b := reflect.ValueOf(before).Elem(), reflect.ValueOf(after).Elem()
	for i := 0; i < a.NumField(); i++ {
		switch a.Type().Field(i).Name {
		case "Doc", "Comment", "Comments", "Obj", "Scope", "Imports", "Unresolved":
			continue
		}
		children(a.Field(i), b.Field(i))
	}
}

func nilGoNode(node goast.Node) bool { return node == nil || reflect.ValueOf(node).IsNil() }

func importDeclarationKey(node goast.Decl) string {
	declaration, ok := node.(*goast.GenDecl)
	if !ok || declaration.Tok != gotoken.IMPORT {
		return ""
	}
	var key string
	for _, spec := range declaration.Specs {
		key += spec.(*goast.ImportSpec).Path.Value + "\x00"
	}
	return key
}

// Validation uses unique virtual token positions. The validation AST need not
// be printable Go (symbolic array carrier names can contain brackets), and
// ordinary CLI generation does not pay for an extra format/parse round trip.
// Each diagnostic position points to the construct that explicitly emitted it.
func validationPositions(emitters []*emitter) *gotoken.FileSet {
	set := gotoken.NewFileSet()
	positionType := reflect.TypeOf(gotoken.NoPos)
	for _, emitter := range emitters {
		var nodes []goast.Node
		seen := map[goast.Node]bool{}
		stride, slots := 2, 0
		goast.Inspect(emitter.goFile, func(node goast.Node) bool {
			if node == nil || seen[node] {
				return false
			}
			seen[node] = true
			nodes = append(nodes, node)
			switch node := node.(type) {
			case *goast.Ident:
				stride = max(stride, len(node.Name)+2)
			case *goast.BasicLit:
				stride = max(stride, len(node.Value)+2)
			}
			value := reflect.ValueOf(node).Elem()
			for index := 0; index < value.NumField(); index++ {
				if value.Field(index).Type() == positionType {
					slots++
				}
			}
			return true
		})
		emitter.tokenFile = set.AddFile(emitter.name, -1, (slots+1)*stride)
		emitter.base = gotoken.Pos(emitter.tokenFile.Base())
		emitter.diagnosticNodes = map[gotoken.Pos]syntax.Node{}
		position := emitter.base
		assign := func(node goast.Node, closing bool) {
			value := reflect.ValueOf(node).Elem()
			for index := 0; index < value.NumField(); index++ {
				field := value.Field(index)
				if field.Type() != positionType {
					continue
				}
				name := value.Type().Field(index).Name
				if closingGoPosition(name) != closing {
					continue
				}
				// These absent positions encode syntax, rather than an omitted
				// location. In particular Assign and Ellipsis affect go/types.
				if field.Int() == 0 && optionalGoPosition(node, name) {
					continue
				}
				field.SetInt(int64(position))
				if original := emitter.sourceNodes[node]; original != nil {
					emitter.diagnosticNodes[position] = original
				}
				position += gotoken.Pos(stride)
			}
		}
		clear(seen)
		var stack []goast.Node
		goast.Inspect(emitter.goFile, func(node goast.Node) bool {
			if node == nil {
				last := len(stack) - 1
				assign(stack[last], true)
				stack = stack[:last]
				return false
			}
			if seen[node] {
				return false
			}
			seen[node] = true
			assign(node, false)
			stack = append(stack, node)
			return true
		})
		// Pos/End may be computed from a child or a token's width. Register
		// these exact positions as well, preferring the child's own origin.
		for index := len(nodes) - 1; index >= 0; index-- {
			node := nodes[index]
			if original := emitter.sourceNodes[node]; original != nil {
				for _, position := range []gotoken.Pos{node.Pos(), node.End()} {
					if position.IsValid() && emitter.diagnosticNodes[position] == nil {
						emitter.diagnosticNodes[position] = original
					}
				}
			}
		}
	}
	return set
}

func closingGoPosition(field string) bool {
	switch field {
	case "Rbrace", "Rparen", "Rbrack", "Closing", "EndPos", "FileEnd":
		return true
	}
	return false
}

func optionalGoPosition(node goast.Node, field string) bool {
	switch node.(type) {
	case *goast.TypeSpec:
		return field == "Assign"
	case *goast.CallExpr:
		return field == "Ellipsis"
	case *goast.GenDecl:
		return field == "Lparen" || field == "Rparen"
	case *goast.FieldList:
		return field == "Opening" || field == "Closing"
	case *goast.ImportSpec:
		return field == "EndPos"
	}
	return false
}

func buildSourceMaps(result *analysis.Result, output map[string][]byte, emitters map[string]*emitter) (map[string]*source.Map, error) {
	files := map[*syntax.File]string{}
	for name, file := range result.Files {
		files[file] = name
	}
	for name, file := range result.Sources {
		if file.Syntax != nil {
			files[file.Syntax] = name
		}
	}
	if result.Resolved != nil {
		for name, file := range result.Resolved.Files {
			files[file.AST] = name
		}
	}
	maps := map[string]*source.Map{}
	for name, emitter := range emitters {
		data := output[name]
		set := gotoken.NewFileSet()
		parsed, err := goparser.ParseFile(set, name, data, goparser.AllErrors|goparser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse generated source map: %w", err)
		}
		nodes := map[goast.Node]syntax.Node{}
		alignGoNodes(emitter.goFile, parsed, func(before, after goast.Node) {
			if original := emitter.sourceNodes[before]; original != nil {
				nodes[after] = original
			}
		})
		file := set.File(parsed.Pos())
		entries := []source.Mapping{{Generated: source.ByteSpan{Start: 0, End: len(data)}, Kind: source.Synthetic}}
		goast.Inspect(parsed, func(node goast.Node) bool {
			if node == nil {
				return true
			}
			span := source.ByteSpan{Start: file.Offset(node.Pos()), End: file.Offset(node.End())}
			entry := source.Mapping{Generated: span, Kind: source.Synthetic}
			if original := nodes[node]; original != nil {
				origin := resolve.Origin{File: name, Span: original.Span()}
				if resolved, ok := emitter.result.Origins[resolve.Ref{File: emitter.name, ID: original.ID()}]; ok {
					origin = resolved
				}
				entry.Source = source.Location{File: origin.File, Span: byteSpan(origin.Span)}
				entry.Kind = source.Derived
				// Only lexical leaves are exact. Structurally equal enclosing
				// expressions must not authorize edits of synthetic children.
				switch node.(type) {
				case *goast.Ident, *goast.BasicLit:
					if input := result.Sources[origin.File]; input != nil {
						from := entry.Source.Span
						if from.Start >= 0 && from.End >= from.Start && from.End <= len(input.Content) && bytes.Equal(input.Content[from.Start:from.End], data[span.Start:span.End]) {
							entry.Kind = source.Exact
						}
					}
				}
				for _, id := range origin.Stack {
					expansion := emitter.result.Expansions[id]
					entry.Expansions = append(entry.Expansions, source.Expansion{ID: uint32(id), Definition: originLocation(expansion.Definition, files), Call: originLocation(expansion.Call, files)})
				}
			}
			entries = append(entries, entry)
			return true
		})
		maps[name] = source.NewMap(entries)
	}
	return maps, nil
}

func byteSpan(span syntax.Span) source.ByteSpan {
	return source.ByteSpan{Start: int(span.Start), End: int(span.End)}
}

func originLocation(origin expand.Origin, files map[*syntax.File]string) source.Location {
	return source.Location{File: files[origin.File], Span: byteSpan(origin.Span)}
}
