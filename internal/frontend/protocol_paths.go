package frontend

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/protocols"
	"github.com/hxsf/goxide/internal/sourcemap"
)

const protocolTypeInfoAlias = "__goxide_protocol_typeinfo"

func (u *usePackage) protocolMetadata(path string) (protocols.Metadata, error) {
	if u.protocols == nil {
		u.protocols = map[string]protocols.Metadata{}
	}
	if m, ok := u.protocols[path]; ok {
		return m, nil
	}
	if p, ok := u.known[path]; ok && p.protocolMetadata.Present() {
		u.protocols[path] = p.protocolMetadata
		return p.protocolMetadata, nil
	}
	m, err := loadProtocolArtifact(path, u.imp)
	if err == nil {
		u.protocols[path] = m
	}
	return m, err
}

func (u *usePackage) protocolDefinition(owner, name string) (protocols.Definition, error) {
	m, err := u.protocolMetadata(owner)
	if err != nil {
		return protocols.Definition{}, err
	}
	for _, d := range m.Definitions {
		if d.Owner == owner && d.Name == name && d.Public {
			return d, nil
		}
	}
	return protocols.Definition{}, fmt.Errorf("unknown or private protocol %s::%s", owner, name)
}

// Protocol reexports denote the canonical source declaration, while the Go
// route is a checked transparent alias of its generated operations constraint.
func (u *usePackage) validateProtocolUse(b useBinding) error {
	d, err := u.protocolDefinition(b.OriginPath, b.OriginName)
	if err != nil {
		return err
	}
	if b.RoutePath == "" || b.RouteName == "" {
		return fmt.Errorf("invalid protocol use route")
	}
	origin, err := u.imp.Import(d.Owner)
	if err != nil {
		return err
	}
	route, err := u.imp.Import(b.RoutePath)
	if err != nil {
		return err
	}
	if origin == nil || route == nil {
		return fmt.Errorf("protocol use package unavailable")
	}
	a, ok := origin.Scope().Lookup(d.OpsName).(*types.TypeName)
	z, routeOK := route.Scope().Lookup(b.RouteName).(*types.TypeName)
	if !ok || !routeOK || !z.Exported() || !sameUseType(a.Type(), z.Type()) {
		return fmt.Errorf("protocol use identity mismatch for %s::%s", b.RoutePath, b.Name)
	}
	return nil
}

// The parser asks this resolver only about protocol constraints. Native Go
// interfaces remain ordinary type bindings even when their methods match.
func protocolOptions(u *usePackage, imported map[string]protocols.Metadata) (protocols.Options, error) {
	if u == nil {
		return protocols.Options{}, nil
	}
	if u.protocols == nil {
		u.protocols = map[string]protocols.Metadata{}
	}
	for path, m := range imported {
		u.protocols[path] = m
	}
	return protocols.Options{LookupProtocol: func(file, spelling string) (protocols.Definition, bool, error) {
		names, err := paths.Parse(strings.ReplaceAll(spelling, ".", "::"))
		if err != nil {
			return protocols.Definition{}, false, nil
		}
		b, err := u.resolvePath(file, names.Names())
		if err != nil {
			var unbound *paths.UnboundError
			if errors.As(err, &unbound) {
				return protocols.Definition{}, false, nil
			}
			return protocols.Definition{}, false, err
		}
		if b.Kind != "protocol" {
			return protocols.Definition{}, false, nil
		}
		d, err := u.protocolDefinition(b.OwnerPath, b.Name)
		if err != nil {
			return d, false, err
		}
		aliases := []string{}
		for alias, path := range u.imports[file] {
			if path == b.RoutePath {
				aliases = append(aliases, alias)
			}
		}
		sort.Strings(aliases)
		if len(aliases) == 0 {
			return d, false, fmt.Errorf("protocol route has no selected import: %s", b.RoutePath)
		}
		d.Route = aliases[0] + "." + b.RouteName
		return d, true, nil
	}}, nil
}

func protocolPathError(file string, source []byte, mapping *sourcemap.Map, t useToken, err error) error {
	result := &sourcemap.Error{Message: err.Error(), Generated: t.pos}
	if mapping != nil {
		if seg := mapping.Lookup(t.start); seg != nil {
			result.Origin, result.Stack = seg.Origin, seg.Stack
		}
	} else {
		origin := sourcemap.Position(file, source, t.start, t.end)
		result.Origin = &origin
	}
	return result
}

// protocolScopeView only supplies parser scopes. Enum declarations are opaque
// nominal types here; their syntax and representation are owned by enums.
func protocolScopeView(file string, source []byte) []byte {
	out := paths.GoView(source)
	ts := pathTokens(file, source)
	depth := 0
	for i := 0; i+2 < len(ts); i++ {
		if depth == 0 && ts[i].text == "enum" && ts[i+1].kind == token.IDENT && ts[i+2].kind == token.LBRACE {
			j, n := i+3, 1
			for ; j < len(ts) && n > 0; j++ {
				if ts[j].kind == token.LBRACE {
					n++
				}
				if ts[j].kind == token.RBRACE {
					n--
				}
			}
			if n == 0 {
				start, end := ts[i].start, ts[j-1].end
				if i > 0 && ts[i-1].text == "pub" {
					start = ts[i-1].start
				}
				for k := start; k < end; k++ {
					if out[k] != '\n' && out[k] != '\r' {
						out[k] = ' '
					}
				}
				copy(out[start:end], "type "+ts[i+1].text+" int")
				i = j - 1
				continue
			}
		}
		if ts[i].kind == token.LBRACE {
			depth++
		}
		if ts[i].kind == token.RBRACE {
			depth--
		}
	}
	return out
}

type protocolLexicalBinding struct {
	name       string
	start, end int
	typ        *ast.TypeSpec
}

// Parser Ident.Obj alone is insufficient for type parameters: local type
// declarations in a function body may shadow a parameter's spelling. Track
// lexical declaration intervals explicitly before selecting any protocol.
func protocolLocalBindings(fs *token.FileSet, f *ast.File) []protocolLexicalBinding {
	var out []protocolLexicalBinding
	var stack []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return false
		}
		end := 0
		for j := len(stack) - 1; j >= 0; j-- {
			switch scope := stack[j].(type) {
			case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause, *ast.ForStmt, *ast.RangeStmt, *ast.IfStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt:
				end = fs.Position(scope.End()).Offset
			}
			if end != 0 {
				break
			}
		}
		add := func(id *ast.Ident, start int, typ *ast.TypeSpec) {
			if end != 0 && id.Name != "_" {
				out = append(out, protocolLexicalBinding{id.Name, start, end, typ})
			}
		}
		switch n := n.(type) {
		case *ast.TypeSpec:
			add(n.Name, fs.Position(n.Name.Pos()).Offset, n)
		case *ast.ValueSpec:
			for _, id := range n.Names {
				add(id, fs.Position(n.End()).Offset, nil)
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				for _, e := range n.Lhs {
					if id, ok := e.(*ast.Ident); ok {
						add(id, fs.Position(n.End()).Offset, nil)
					}
				}
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				end = fs.Position(n.End()).Offset
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if id, ok := e.(*ast.Ident); ok {
						add(id, fs.Position(n.Body.Pos()).Offset, nil)
					}
				}
			}
		case *ast.FuncLit:
			if n.Body != nil {
				end = fs.Position(n.Body.End()).Offset
				for _, fields := range []*ast.FieldList{n.Type.Params, n.Type.Results} {
					if fields != nil {
						for _, field := range fields.List {
							for _, id := range field.Names {
								add(id, fs.Position(n.Body.Pos()).Offset, nil)
							}
						}
					}
				}
			}
		}
		stack = append(stack, n)
		return true
	})
	return out
}

// Resolve transparent package aliases in their declaring file's namespace.
// Defined types and native interface shapes never inherit this evidence.
func (u *usePackage) protocolAlias(file, receiver string) (string, string, error) {
	if u == nil {
		return file, receiver, nil
	}
	seen := map[string]bool{}
	for !strings.Contains(receiver, ".") {
		key := file + "::" + receiver
		if seen[key] {
			return file, receiver, fmt.Errorf("cyclic type alias in associated path %s", receiver)
		}
		seen[key] = true
		found := false
		for _, name := range sortedSources(u.original) {
			// Native Go grouping is declaration syntax, not a different alias
			// identity. Inspect top-level TypeSpecs before the DSL fallback.
			parsed, _ := parser.ParseFile(token.NewFileSet(), name, protocolScopeView(name, u.original[name]), parser.AllErrors)
			if parsed != nil {
				for _, decl := range parsed.Decls {
					group, ok := decl.(*ast.GenDecl)
					if !ok || group.Tok != token.TYPE {
						continue
					}
					for _, spec := range group.Specs {
						typ := spec.(*ast.TypeSpec)
						if typ.Name.Name != receiver || !typ.Assign.IsValid() {
							continue
						}
						if target, _, ok := protocolAliasExpression(typ.Type, parsed, map[*ast.Object]bool{}); ok {
							file, receiver, found = name, target, true
						}
						break
					}
					if found {
						break
					}
				}
			}
			if found {
				break
			}
			ts := pathTokens(name, u.original[name])
			depth := 0
			for i := 0; i+3 < len(ts); i++ {
				if ts[i].kind == token.LBRACE {
					depth++
				}
				if ts[i].kind == token.RBRACE {
					depth--
				}
				if depth != 0 || ts[i].kind != token.TYPE || ts[i+1].text != receiver || ts[i+2].kind != token.ASSIGN {
					continue
				}
				j := i + 3
				var names []string
				for j < len(ts) && ts[j].kind == token.IDENT {
					names = append(names, ts[j].text)
					j++
					if j < len(ts) && ts[j].kind == token.PERIOD {
						j++
						continue
					}
					if j+1 < len(ts) && ts[j].kind == token.COLON && ts[j+1].kind == token.COLON {
						j += 2
						continue
					}
					break
				}
				if len(names) == 0 || (j < len(ts) && ts[j].kind != token.SEMICOLON) {
					continue
				}
				file, receiver, found = name, strings.Join(names, "."), true
				break
			}
			if found {
				break
			}
		}
		if !found {
			break
		}
	}
	return file, receiver, nil
}

// Follow AST alias objects, retaining the identity of a terminal type
// parameter. A package alias named T must not inherit an unrelated function's
// T bound merely because the resolved spelling is the same.
func protocolAliasExpression(e ast.Expr, f *ast.File, seen map[*ast.Object]bool) (string, *ast.Ident, bool) {
	if e == nil {
		return "", nil, false
	}
	switch e := ast.Unparen(e).(type) {
	case *ast.Ident:
		obj := e.Obj
		if obj == nil && f != nil {
			obj = f.Scope.Lookup(e.Name)
		}
		if obj != nil {
			if obj.Kind != ast.Typ || seen[obj] {
				return "", nil, false
			}
			if typ, ok := obj.Decl.(*ast.TypeSpec); ok && typ.Assign.IsValid() {
				seen[obj] = true
				return protocolAliasExpression(typ.Type, f, seen)
			}
		}
		return e.Name, e, true
	case *ast.SelectorExpr:
		root, ok := e.X.(*ast.Ident)
		if !ok || (root.Obj != nil && root.Obj.Kind != ast.Pkg) {
			return "", nil, false
		}
		return root.Name + "." + e.Sel.Name, nil, true
	}
	return "", nil, false
}

// lowerProtocolPaths rewrites only proven associated operations. Each emitted
// selector is causal generated syntax; unchanged call arguments keep their
// exact original spans for later type and permission diagnostics.
func lowerProtocolPaths(file string, source []byte, mapping *sourcemap.Map, p *protocols.Package, u *usePackage) ([]byte, error) {
	if p == nil {
		return source, nil
	}
	fs := token.NewFileSet()
	f, _ := parser.ParseFile(fs, file, protocolScopeView(file, source), parser.AllErrors)
	ids := map[int]*ast.Ident{}
	var funcs []*ast.FuncDecl
	var lexical []protocolLexicalBinding
	if f != nil {
		lexical = protocolLocalBindings(fs, f)
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				ids[fs.Position(id.Pos()).Offset] = id
			}
			if fn, ok := n.(*ast.FuncDecl); ok {
				funcs = append(funcs, fn)
			}
			return true
		})
	}
	ts := pathTokens(file, source)
	var edits []useEdit
	for i := 1; i+2 < len(ts); i++ {
		if ts[i].kind != token.COLON || ts[i+1].kind != token.COLON || ts[i-1].kind != token.IDENT || ts[i+2].kind != token.IDENT {
			continue
		}
		start := i - 1
		for start >= 2 && ts[start-1].kind == token.PERIOD && ts[start-2].kind == token.IDENT {
			start -= 2
		}
		// A longer static path belongs to a later member, not this prefix.
		if i+4 < len(ts) && ts[i+3].kind == token.COLON && ts[i+4].kind == token.COLON {
			continue
		}
		receiver := string(source[ts[start].start:ts[i-1].end])
		receiver = strings.ReplaceAll(receiver, " ", "")
		function := ""
		var enclosing *ast.FuncDecl
		for _, fn := range funcs {
			if fs.Position(fn.Pos()).Offset <= ts[start].start && ts[start].start < fs.Position(fn.End()).Offset {
				function = fn.Name.Name
				enclosing = fn
				break
			}
		}
		root := ids[ts[start].start]
		var local *protocolLexicalBinding
		for j := range lexical {
			b := &lexical[j]
			if b.name == ts[start].text && b.start <= ts[start].start && ts[start].start < b.end && (local == nil || b.start > local.start) {
				local = b
			}
		}
		if local != nil && (local.typ == nil || !local.typ.Assign.IsValid()) {
			continue
		}
		// The Go parser resolves lexical values and local aliases even before
		// type checking. A same-spelled value must never select package impls.
		if root != nil && root.Obj != nil && root.Obj.Kind != ast.Typ {
			continue
		}
		var receiverExpr ast.Expr = root
		if strings.Contains(receiver, ".") || root == nil {
			receiverExpr, _ = parser.ParseExpr(receiver)
		}
		if local != nil {
			receiverExpr = local.typ.Type
		}
		if local == nil && root != nil && root.Obj != nil {
			if decl, ok := root.Obj.Decl.(*ast.TypeSpec); ok && !decl.Assign.IsValid() && (f == nil || f.Scope.Lookup(root.Name) != root.Obj) {
				continue
			}
		}
		canonical, resolved, valid := protocolAliasExpression(receiverExpr, f, map[*ast.Object]bool{})
		if !valid {
			continue
		}
		boundReceiver := false
		if enclosing != nil && enclosing.Type.TypeParams != nil && resolved != nil {
			for _, field := range enclosing.Type.TypeParams.List {
				for _, id := range field.Names {
					boundReceiver = boundReceiver || (id.Obj != nil && id.Obj == resolved.Obj)
				}
			}
		}
		canonicalFile := file
		if u != nil && !boundReceiver {
			var aliasErr error
			canonicalFile, canonical, aliasErr = u.protocolAlias(file, canonical)
			if aliasErr != nil {
				return nil, protocolPathError(file, source, mapping, ts[i+2], aliasErr)
			}
		}
		lookupFunction := ""
		if boundReceiver {
			lookupFunction = function
		}
		selected, ok, err := p.LookupAssociated(file, lookupFunction, canonical, ts[i+2].text)
		if err != nil {
			return nil, protocolPathError(file, source, mapping, ts[i+2], err)
		}
		if !ok && u != nil {
			selected, ok, err = u.importedAssociated(canonicalFile, canonical, ts[i+2].text)
			if err != nil {
				return nil, protocolPathError(file, source, mapping, ts[i+2], err)
			}
		}
		if !ok {
			if enclosing != nil && enclosing.Type.TypeParams != nil && local == nil {
				for _, field := range enclosing.Type.TypeParams.List {
					unconstrained := false
					if id, ok := field.Type.(*ast.Ident); ok {
						unconstrained = id.Name == "any"
					}
					if constraint, ok := field.Type.(*ast.InterfaceType); ok {
						unconstrained = constraint.Methods == nil || len(constraint.Methods.List) == 0
					}
					if !unconstrained {
						continue
					}
					for _, id := range field.Names {
						if id.Name == receiver {
							return nil, protocolPathError(file, source, mapping, ts[i+2], fmt.Errorf("type parameter %s requires an explicit protocol bound for associated member %s", receiver, ts[i+2].text))
						}
					}
				}
			}
			continue
		}
		text := protocolTypeInfoAlias + ".Of[" + receiver + "]()." + selected.Member.GoName
		edits = append(edits, useEdit{ts[start].start, ts[i+2].end, text})
		i += 2
	}
	if len(edits) == 0 {
		return source, nil
	}
	out, err := applyUseEdits(source, mapping, edits, "protocol associated paths", true, "protocol-associated")
	if err != nil {
		return nil, err
	}
	fimports, err := parseSourceImports(file, out)
	if err != nil {
		return nil, err
	}
	for alias, path := range defaultImportPaths(fimports, nil) {
		if alias == protocolTypeInfoAlias && path == protocols.TypeInfoPackage {
			return out, nil
		}
	}
	return addUseImportsMapped(file, out, map[string]string{protocols.TypeInfoPackage: protocolTypeInfoAlias}, mapping)
}

func (u *usePackage) importedAssociated(file, receiver, member string) (protocols.Associated, bool, error) {
	names := strings.Split(receiver, ".")
	b, err := u.resolvePath(file, names)
	if err != nil {
		return protocols.Associated{}, false, nil
	}
	if b.Kind != "type" || b.OwnerPath == u.metadata.Path {
		return protocols.Associated{}, false, nil
	}
	m, err := u.protocolMetadata(b.OwnerPath)
	if err != nil {
		return protocols.Associated{}, false, err
	}
	var found *protocols.Associated
	for _, impl := range m.Implementations {
		if impl.Target.Owner != b.OwnerPath || impl.Target.Name != b.Name {
			continue
		}
		for _, op := range impl.Members {
			if op.Name == member {
				if found != nil {
					return protocols.Associated{}, false, fmt.Errorf("ambiguous associated member %s::%s", receiver, member)
				}
				a := protocols.Associated{File: file, Protocol: impl.Protocol, Target: impl.Target, Receiver: receiver, CarrierName: impl.CarrierName, Member: op}
				found = &a
			}
		}
	}
	if found != nil {
		return *found, true, nil
	}
	return protocols.Associated{}, false, nil
}
