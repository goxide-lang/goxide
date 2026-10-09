// Package printing lowers compiler-owned print plans after ordinary expression
// typing. It deliberately has no dynamic formatter or method-name fallback.
package printing

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/sourcemap"
)

type Config struct {
	Importer    types.Importer
	PackagePath string
	DebugTypes  map[string]bool
	Maps        map[string]*sourcemap.Map
}

type part struct {
	literal string
	debug   bool
	slot    bool
}

func plan(format string) ([]part, error) {
	var out []part
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			out = append(out, part{literal: text.String()})
			text.Reset()
		}
	}
	for i := 0; i < len(format); {
		if format[i] == '{' {
			if strings.HasPrefix(format[i:], "{{") {
				text.WriteByte('{')
				i += 2
				continue
			}
			flush()
			if strings.HasPrefix(format[i:], "{}") {
				out = append(out, part{slot: true})
				i += 2
				continue
			}
			if strings.HasPrefix(format[i:], "{:?}") {
				out = append(out, part{slot: true, debug: true})
				i += 4
				continue
			}
			return nil, fmt.Errorf("unsupported or unclosed format placeholder")
		}
		if format[i] == '}' {
			if strings.HasPrefix(format[i:], "}}") {
				text.WriteByte('}')
				i += 2
				continue
			}
			return nil, fmt.Errorf("unmatched closing format brace")
		}
		text.WriteByte(format[i])
		i++
	}
	flush()
	return out, nil
}

type edit struct {
	start, end int
	text       string
	segments   []sourcemap.Segment
}

func Lower(files map[string][]byte, cfg Config) (map[string][]byte, error) {
	active := false
	for _, s := range files {
		active = active || bytes.Contains(s, []byte("__goxide_print"))
	}
	if !active {
		return files, nil
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	fs := token.NewFileSet()
	trees := map[string]*ast.File{}
	var all []*ast.File
	typedCalls := map[string][]*ast.CallExpr{}
	for _, n := range names {
		f, err := parser.ParseFile(fs, n, files[n], parser.ParseComments|parser.AllErrors)
		if err != nil {
			return nil, err
		}
		trees[n] = f
		typed, err := parser.ParseFile(fs, n, files[n], parser.AllErrors)
		if err != nil {
			return nil, err
		}
		rewrite := func(stmts []ast.Stmt) {
			for i, stmt := range stmts {
				es, ok := stmt.(*ast.ExprStmt)
				if !ok {
					continue
				}
				call, ok := es.X.(*ast.CallExpr)
				if !ok {
					continue
				}
				id, ok := call.Fun.(*ast.Ident)
				if !ok || (id.Name != "__goxide_print" && id.Name != "__goxide_println") {
					continue
				}
				typedCalls[n] = append(typedCalls[n], call)
				block := &ast.BlockStmt{Lbrace: call.Pos()}
				for _, arg := range call.Args {
					block.List = append(block.List, &ast.AssignStmt{Lhs: []ast.Expr{ast.NewIdent("_")}, Tok: token.ASSIGN, Rhs: []ast.Expr{arg}})
				}
				stmts[i] = block
			}
		}
		ast.Inspect(typed, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.BlockStmt:
				rewrite(node.List)
			case *ast.CaseClause:
				rewrite(node.Body)
			case *ast.CommClause:
				rewrite(node.Body)
			}
			return true
		})
		all = append(all, typed)
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	path := cfg.PackagePath
	if path == "" {
		path = "goxide/generated"
	}
	conf := types.Config{Importer: cfg.Importer, Error: func(error) {}}
	_, _ = conf.Check(path, fs, all, info)
	argTypes := map[string]map[int]types.Type{}
	for name, calls := range typedCalls {
		argTypes[name] = map[int]types.Type{}
		for _, call := range calls {
			for _, arg := range call.Args {
				argTypes[name][fs.Position(arg.Pos()).Offset] = info.TypeOf(arg)
			}
		}
	}
	result := map[string][]byte{}
	changed := false
	for fileIndex, n := range names {
		source := files[n]
		tree := trees[n]
		mapping := cfg.Maps[n]
		pos := func(p token.Pos) int { return fs.Position(p).Offset }
		fail := func(node ast.Node, msg string) error {
			p := fs.Position(node.Pos())
			e := &sourcemap.Error{Message: "format: " + msg, Generated: p}
			if mapping != nil {
				// Macro argument precedence wrappers are generated; their
				// enclosed expression still has a proven copied source span.
				candidate := node
				for {
					if seg := mapping.Lookup(pos(candidate.Pos())); seg != nil {
						e.Stack = seg.Stack
						if seg.Origin != nil {
							e.Origin = seg.Origin
							break
						}
					}
					paren, ok := candidate.(*ast.ParenExpr)
					if !ok {
						break
					}
					candidate = paren.X
				}
			}
			return e
		}
		parents := map[ast.Node]ast.Node{}
		var stack []ast.Node
		ast.Inspect(tree, func(node ast.Node) bool {
			if node == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			if len(stack) > 0 {
				parents[node] = stack[len(stack)-1]
			}
			stack = append(stack, node)
			return true
		})
		var edits []edit
		var failure error
		next := 0
		generated := "__goxide_print_generated"
		for bytes.Contains(source, []byte(generated)) {
			generated += "X"
		}
		runtimeAlias := fmt.Sprintf("%s_runtime_f%d", generated, fileIndex)
		var aliases strings.Builder
		ast.Inspect(tree, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || failure != nil {
				return true
			}
			id, ok := call.Fun.(*ast.Ident)
			if !ok || (id.Name != "__goxide_print" && id.Name != "__goxide_println") {
				return true
			}
			statement, ok := parents[call].(*ast.ExprStmt)
			if !ok {
				failure = fail(call, "print macros currently require a direct statement (no go/defer or value context)")
				return false
			}
			switch parents[statement].(type) {
			case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
			default:
				failure = fail(call, "print macros cannot be used as an initializer or loop post statement")
				return false
			}
			format := ""
			args := call.Args
			if len(args) == 0 {
				if id.Name != "__goxide_println" {
					failure = fail(call, "print! requires a format string literal")
					return false
				}
			} else {
				lit, ok := args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					failure = fail(args[0], "first argument must be a string literal")
					return false
				}
				var err error
				format, err = strconv.Unquote(lit.Value)
				if err != nil {
					failure = fail(lit, err.Error())
					return false
				}
				args = args[1:]
			}
			parts, err := plan(format)
			if err != nil {
				failure = fail(call, err.Error())
				return false
			}
			slots := 0
			for _, p := range parts {
				if p.slot {
					slots++
				}
			}
			if len(args) != slots || call.Ellipsis.IsValid() {
				failure = fail(call, fmt.Sprintf("format has %d placeholders but %d arguments", slots, len(args)))
				return false
			}
			prefix := fmt.Sprintf("%s_f%d_n%d_", generated, fileIndex, next)
			next++
			var body strings.Builder
			body.WriteString("{\n")
			var segments []sourcemap.Segment
			for i, arg := range args {
				fmt.Fprintf(&body, "%sa%d := ", prefix, i)
				if mapping != nil {
					segments = append(segments, mapping.Copy(pos(arg.Pos()), pos(arg.End()), body.Len())...)
				}
				body.Write(source[pos(arg.Pos()):pos(arg.End())])
				body.WriteString("\n")
			}
			fmt.Fprintf(&body, "%stext := %q\n", prefix, "")
			ai := 0
			for _, p := range parts {
				if !p.slot {
					fmt.Fprintf(&body, "%stext += %q\n", prefix, p.literal)
					continue
				}
				helper, derived, err := formatter(argTypes[n][pos(args[ai].Pos())], p.debug, path, cfg.DebugTypes)
				if err != nil {
					failure = fail(args[ai], err.Error())
					return false
				}
				if derived {
					alias := fmt.Sprintf("%sdebug%d", prefix, ai)
					fmt.Fprintf(&aliases, "\nvar %s = %s\n", alias, helper)
					fmt.Fprintf(&body, "%stext += %s(&%sa%d)\n", prefix, alias, prefix, ai)
				} else {
					fmt.Fprintf(&body, "%stext += %s.%s(%sa%d)\n", prefix, runtimeAlias, helper, prefix, ai)
				}
				ai++
			}
			if id.Name == "__goxide_println" {
				fmt.Fprintf(&body, "%stext += \"\\n\"\n", prefix)
			}
			fmt.Fprintf(&body, "%s.Write(%stext)\n}", runtimeAlias, prefix)
			edits = append(edits, edit{pos(statement.Pos()), pos(statement.End()), body.String(), segments})
			return false
		})
		if failure != nil {
			return nil, failure
		}
		if len(edits) == 0 {
			result[n] = source
			continue
		}
		changed = true
		edits = append(edits, edit{pos(tree.Name.End()), pos(tree.Name.End()), fmt.Sprintf("\nimport %s %q\n", runtimeAlias, "github.com/goxide-lang/std/formatting"), nil})
		edits = append(edits, edit{len(source), len(source), aliases.String(), nil})
		sort.SliceStable(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
		var out bytes.Buffer
		cursor := 0
		mapped := &sourcemap.Map{}
		if mapping != nil {
			mapped.Stages = append(append([]string(nil), mapping.Stages...), "printing.Lower")
		}
		for _, e := range edits {
			if mapping != nil {
				mapped.Segments = append(mapped.Segments, mapping.Copy(cursor, e.start, out.Len())...)
			}
			out.Write(source[cursor:e.start])
			offset := out.Len()
			for _, seg := range e.segments {
				seg.Start += offset
				seg.End += offset
				mapped.Segments = append(mapped.Segments, seg)
			}
			out.WriteString(e.text)
			cursor = e.end
		}
		if mapping != nil {
			mapped.Segments = append(mapped.Segments, mapping.Copy(cursor, len(source), out.Len())...)
		}
		out.Write(source[cursor:])
		formatted, err := format.Source(out.Bytes())
		if err != nil {
			return nil, fmt.Errorf("internal print lowering: %w", err)
		}
		result[n] = formatted
		mapped.Source = out.Bytes()
		mapped = mapped.Format(out.Bytes(), formatted, "printing.format")
		if mapping != nil {
			*mapping = *mapped
		}
	}
	if changed {
		for _, source := range result {
			if bytes.Contains(source, []byte("__goxide_print(")) || bytes.Contains(source, []byte("__goxide_println(")) {
				return Lower(result, cfg)
			}
		}
	}
	return result, nil
}

func formatter(t types.Type, debug bool, path string, known map[string]bool) (string, bool, error) {
	if t == nil {
		return "", false, fmt.Errorf("cannot resolve formatting argument type")
	}
	t = types.Unalias(t)
	if named, ok := t.(*types.Named); ok {
		obj := named.Obj()
		if debug && obj.Pkg() != nil && obj.Pkg().Path() == path && obj.Pkg().Scope().Lookup(obj.Name()) == obj && known[obj.Name()] {
			return "Debug" + obj.Name(), true, nil
		}
		return "", false, fmt.Errorf("type %s has no explicit supported formatting protocol", t)
	}
	b, ok := t.(*types.Basic)
	if !ok {
		return "", false, fmt.Errorf("type %s has no explicit supported formatting protocol", t)
	}
	if b.Info()&types.IsUntyped != 0 {
		b, _ = types.Default(b).(*types.Basic)
		if b == nil {
			return "", false, fmt.Errorf("unsupported untyped formatting argument")
		}
	}
	if b.Kind() == types.String {
		if debug {
			return "DebugString", false, nil
		}
		return "DisplayString", false, nil
	}
	names := map[types.BasicKind]string{types.Bool: "Bool", types.Int: "Int", types.Int8: "Int8", types.Int16: "Int16", types.Int32: "Int32", types.Int64: "Int64", types.Uint: "Uint", types.Uint8: "Uint8", types.Uint16: "Uint16", types.Uint32: "Uint32", types.Uint64: "Uint64", types.Uintptr: "Uintptr", types.Float32: "Float32", types.Float64: "Float64", types.Complex64: "Complex64", types.Complex128: "Complex128"}
	if name := names[b.Kind()]; name != "" {
		return name, false, nil
	}
	return "", false, fmt.Errorf("type %s has no explicit supported formatting protocol", t)
}
