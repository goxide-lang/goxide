// Package lower 将检查后的语法树转换为格式化的 Go 源码。
package lower

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	"go/importer"
	goparser "go/parser"
	"go/printer"
	gotoken "go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/resolve"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type Severity uint8

const Error Severity = 0

type Diagnostic struct {
	Definition, Call expand.Origin
	Severity         Severity
	File             string
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

// Lower 的输出键沿用输入文件名；调用方写盘时将 .gox 后缀换成 .go。
// files 和 info 必须描述同一批 AST。失败时返回 nil，不返回部分输出。
func Lower(files map[string]*syntax.File, info *check.Info) (map[string][]byte, error) {
	return lower(files, info, emissionOptions{})
}

type emissionOptions struct {
	lineDirectives bool
	originals      map[string]*syntax.File
}

func lower(files map[string]*syntax.File, info *check.Info, options emissionOptions) (map[string][]byte, error) {
	var res *resolve.Result
	if info != nil {
		res = info.Resolve
		if len(info.Diagnostics) > 0 {
			return nil, info.Diagnostics[0]
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	hygienicNames := macroNames(info)
	traits := prepareTraits(info)
	fset := gotoken.NewFileSet()
	var emitters []*emitter
	memberSymbols := make(map[*resolve.Symbol]bool)
	externalTests := make(map[*resolve.Scope]bool)
	if res != nil {
		for _, pkg := range res.Packages {
			if pkg.IsExternalTest() {
				externalTests[pkg.Scope] = true
			}
		}
		for _, f := range res.Files {
			for _, scope := range f.Scopes {
				if scope.Kind == resolve.MemberScope {
					for _, symbol := range scope.Bindings {
						memberSymbols[symbol] = true
					}
				}
			}
		}
	}
	for _, name := range names {
		tree := files[name]
		e := &emitter{symbolNames: hygienicNames, traits: traits, info: info, name: name, tree: tree, result: res, imports: make(map[string]*importUse), reserved: make(map[string]bool), memberSymbols: memberSymbols, memberNames: make(map[*goast.Ident]string)}
		e.lineDirectives, e.original = options.lineDirectives, options.originals[name]
		if res != nil {
			e.file = res.Files[name]
		}
		if tree == nil {
			return nil, &Diagnostic{File: name, Message: "缺少输入 AST"}
		}
		if e.file == nil || e.file.AST != tree {
			e.fail(tree, "缺少与输入 AST 对应的名字解析结果")
			return nil, e.err
		}
		e.externalTest = externalTests[e.file.Scope.Parent]
		filename := name
		if options.lineDirectives {
			filename = filepath.Base(name)
		}
		e.tokenFile = fset.AddFile(filename, -1, int(tree.Span().End)+256)
		e.base = gotoken.Pos(e.tokenFile.Base())
		e.reserveNames()
		e.reserveTraitNames()
		e.validatePrintStatements()
		e.checkMacroNames()
		f := &goast.File{Package: e.pos(tree.Package) - 8, Name: &goast.Ident{NamePos: e.pos(tree.Package), Name: tree.Package.Name}}
		if f.Package < e.base {
			f.Package = e.base
		}
		for _, item := range tree.Items {
			if enum, ok := item.(*syntax.EnumDecl); ok {
				f.Decls = append(f.Decls, e.enumDecl(enum)...)
				continue
			}
			if decl := e.decl(item); decl != nil {
				f.Decls = append(f.Decls, decl)
			}
		}
		f.Decls = append(f.Decls, e.deriveDeclarations()...)
		f.Decls = append(f.Decls, e.errorDeclarations()...)
		f.Decls = append(f.Decls, e.serdeDeclarations()...)
		f.Decls = append(f.Decls, e.traitDeclarations()...)
		f.Decls = append(f.Decls, e.resultAdapters...)
		f.Decls = append(f.Decls, e.flowHelpers...)
		if e.err != nil {
			return nil, e.err
		}
		for _, diagnostic := range res.Diagnostics {
			if diagnostic.File == name && diagnostic.Severity == resolve.Error {
				return nil, &Diagnostic{File: name, Span: diagnostic.Span, Message: diagnostic.Message, Origin: diagnostic.Origin, Definition: diagnostic.Definition, Call: diagnostic.Call}
			}
		}
		e.addImports(f)
		e.positions(f, fset)
		if options.lineDirectives {
			multilineSourceBodies(f, fset)
		}
		e.goFile = f
		emitters = append(emitters, e)
	}
	// A diagnostic may be displayed in a macro definition or another input
	// file that is not among the emitted files.
	if res != nil {
		for _, diagnostic := range res.Diagnostics {
			if diagnostic.Severity == resolve.Error {
				return nil, &Diagnostic{File: diagnostic.File, Span: diagnostic.Span, Message: diagnostic.Message, Origin: diagnostic.Origin, Definition: diagnostic.Definition, Call: diagnostic.Call}
			}
		}
	}
	bindMembers(fset, emitters)
	out := make(map[string][]byte, len(files))
	for _, e := range emitters {
		var buf bytes.Buffer
		var err error
		if options.lineDirectives {
			config := printer.Config{Mode: printer.UseSpaces | printer.TabIndent | printer.SourcePos, Tabwidth: 8}
			err = config.Fprint(&buf, fset, e.goFile)
		} else {
			err = format.Node(&buf, fset, e.goFile)
		}
		if err != nil {
			e.fail(e.tree, "Go 输出格式化失败: "+err.Error())
			return nil, e.err
		}
		data, err := format.Source(buf.Bytes())
		if err != nil {
			e.fail(e.tree, "生成的 Go 语法无效: "+err.Error())
			return nil, e.err
		}
		if _, err := goparser.ParseFile(gotoken.NewFileSet(), e.name, data, goparser.AllErrors); err != nil {
			e.fail(e.tree, "生成的 Go 语法无效: "+err.Error())
			return nil, e.err
		}
		out[e.name] = data
	}
	return out, nil
}

type goImporter func(string) (*types.Package, error)

func (f goImporter) Import(path string) (*types.Package, error) { return f(path) }

func bindMembers(fset *gotoken.FileSet, emitters []*emitter) {
	groups := map[string][]*emitter{}
	tests := map[*resolve.Scope][]*emitter{}
	needed := false
	for _, e := range emitters {
		key := filepath.ToSlash(filepath.Dir(e.name))
		if e.externalTest {
			tests[e.file.Scope.Parent] = append(tests[e.file.Scope.Parent], e)
		} else {
			groups[key] = append(groups[key], e)
		}
		needed = needed || len(e.memberNames) > 0
	}
	if !needed {
		return
	}
	// Go's receiver validation requires a file for both declared type names and
	// instantiated receiver names. Give generated names temporary source positions.
	var generatedTypePositions []*gotoken.Pos
	for _, e := range emitters {
		goast.Inspect(e.goFile, func(node goast.Node) bool {
			if spec, ok := node.(*goast.TypeSpec); ok && !spec.Name.NamePos.IsValid() {
				generatedTypePositions = append(generatedTypePositions, &spec.Name.NamePos)
				spec.Name.NamePos = e.goFile.Package
			}
			if method, ok := node.(*goast.FuncDecl); ok && method.Recv != nil {
				goast.Inspect(method.Recv, func(node goast.Node) bool {
					var position *gotoken.Pos
					switch n := node.(type) {
					case *goast.Ident:
						position = &n.NamePos
					case *goast.StarExpr:
						position = &n.Star
					}
					if position != nil && !position.IsValid() {
						generatedTypePositions = append(generatedTypePositions, position)
						*position = e.goFile.Package
					}
					return true
				})
			}
			return true
		})
	}
	defer func() {
		for _, position := range generatedTypePositions {
			*position = gotoken.NoPos
		}
	}()
	var renamer memberRenamer
	loaded := map[string]*types.Package{}
	loadedGroups := map[*emitter]*types.Package{}
	loading := map[*emitter]bool{}
	standard := importer.Default()
	if emitters[0].info.GoImporter != nil {
		standard = emitters[0].info.GoImporter
	}
	indexSourcePackages(groups, emitters, standard)
	var load goImporter
	rename := func(path string, group []*emitter) *types.Package {
		var files []*goast.File
		names := map[*goast.Ident]string{}
		for _, e := range group {
			files = append(files, e.goFile)
			for id, name := range e.memberNames {
				names[id] = name
			}
		}
		return renamer.rename(path, fset, files, names, load, group[0].info.GoSizes)
	}
	load = func(path string) (*types.Package, error) {
		if p := loaded[path]; p != nil {
			return p, nil
		}
		group := groups[path]
		if group == nil {
			return standard.Import(path)
		}
		identity := group[0]
		if p := loadedGroups[identity]; p != nil {
			return p, nil
		}
		if loading[identity] {
			return nil, fmt.Errorf("import cycle: %s", path)
		}
		loading[identity] = true
		p := rename(path, group)
		loaded[path] = p
		loadedGroups[identity] = p
		loading[identity] = false
		return p, nil
	}
	for _, e := range emitters {
		path := filepath.ToSlash(filepath.Dir(e.name))
		if e.externalTest {
			group := tests[e.file.Scope.Parent]
			if group[0] == e {
				rename(path, group)
			}
		} else {
			_, _ = load(path)
		}
	}
}

// 文件名可以是源码路径；同一模块的包目录共享一个导入前缀。
func indexSourcePackages(groups map[string][]*emitter, emitters []*emitter, standard types.Importer) {
	root := filepath.Dir(emitters[0].name)
	for _, e := range emitters[1:] {
		root = commonDirectory(root, filepath.Dir(e.name))
	}
	relative := map[string][]*emitter{}
	for dir, group := range groups {
		rel, err := filepath.Rel(root, filepath.FromSlash(dir))
		if err != nil {
			return
		}
		relative[filepath.ToSlash(rel)] = group
	}
	prefixes := map[string]bool{}
	seen := map[string]bool{}
	for _, e := range emitters {
		for imported := range e.imports {
			if seen[imported] || groups[imported] != nil {
				continue
			}
			seen[imported] = true
			if _, err := standard.Import(imported); err == nil {
				continue
			}
			for rel := range relative {
				if rel != "." && (imported == rel || strings.HasSuffix(imported, "/"+rel)) {
					prefixes[strings.TrimSuffix(strings.TrimSuffix(imported, rel), "/")] = true
				}
			}
		}
	}
	if len(prefixes) != 1 {
		return
	}
	for prefix := range prefixes {
		for rel, group := range relative {
			alias := filepath.ToSlash(filepath.Join(prefix, rel))
			if groups[alias] == nil {
				groups[alias] = group
			}
		}
	}
}

func commonDirectory(root, dir string) string {
	for {
		rel, err := filepath.Rel(root, dir)
		if err != nil || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return root
		}
		root = filepath.Dir(root)
	}
}

type emitter struct {
	functionFrame      *functionFrame
	assignmentGuard    *goast.Ident
	shiftCount         *goast.Ident
	resultAdapters     []goast.Decl
	flowHelpers        []goast.Decl
	zeroFunctions      map[int]*goast.Ident
	zeroPointers       map[int]*goast.Ident
	traits             *traitProgram
	symbolNames        map[*resolve.Symbol]string
	primitiveAliases   map[string]*goast.Ident
	packageTypeAliases map[string]*goast.Ident
	functionAliases    map[string]*goast.Ident
	localTypes         map[types.Type]*localTypeName
	matchDepth         int
	breakTarget        *breakTarget
	info               *check.Info
	pending            []goast.Stmt
	replacements       map[syntax.Expr]goast.Expr
	tempIndex          int
	base               gotoken.Pos
	tokenFile          *gotoken.File
	goFile             *goast.File
	memberSymbols      map[*resolve.Symbol]bool
	memberNames        map[*goast.Ident]string
	name               string
	tree               *syntax.File
	file               *resolve.File
	result             *resolve.Result
	err                *Diagnostic
	imports            map[string]*importUse
	reserved           map[string]bool
	externalTest       bool
	lineDirectives     bool
	original           *syntax.File
}

func (e *emitter) samePackage(file string) bool {
	other := e.result.Files[file]
	return other != nil && other.Scope.Parent == e.file.Scope.Parent
}

func (e *emitter) localTypePackage(pkg *types.Package) bool {
	return pkg != nil && pkg.Path() == filepath.ToSlash(filepath.Dir(e.name)) && pkg.Name() == e.tree.Package.Name
}

func (e *emitter) fail(n syntax.Node, message string) {
	if e.err != nil {
		return
	}
	span := syntax.Span{}
	if n != nil && !reflect.ValueOf(n).IsNil() {
		span = n.Span()
	}
	e.err = &Diagnostic{File: e.name, Span: span, Message: message, Origin: resolve.Origin{File: e.name, Span: span}}
	if e.result != nil && n != nil && !reflect.ValueOf(n).IsNil() {
		if origin, ok := e.result.Origins[resolve.Ref{File: e.name, ID: n.ID()}]; ok {
			e.err.Origin = origin
			if origin.File != "" {
				e.err.File, e.err.Span = origin.File, origin.Span
			}
			if expansion, ok := e.result.Expansions[origin.Expn]; ok {
				e.err.Definition, e.err.Call = expansion.Definition, expansion.Call
			}
		}
	}
}
func (e *emitter) pos(n syntax.Node) gotoken.Pos {
	if n == nil || reflect.ValueOf(n).IsNil() {
		return gotoken.NoPos
	}
	return gotoken.Pos(e.sourceSpan(n).Start) + e.base
}
func (e *emitter) end(n syntax.Node) gotoken.Pos {
	if n == nil || reflect.ValueOf(n).IsNil() {
		return gotoken.NoPos
	}
	return gotoken.Pos(e.sourceSpan(n).End) + e.base
}

// Keep line attribution local to the generated file. Captured macro fragments
// retain their caller origin; templates defined elsewhere use the nearest call
// in this file rather than introducing a cross-file source mapping.
func (e *emitter) sourceSpan(n syntax.Node) syntax.Span {
	if e.lineDirectives && e.result != nil {
		if origin, ok := e.result.Origins[resolve.Ref{File: e.name, ID: n.ID()}]; ok {
			if origin.File == e.name {
				return origin.Span
			}
			for index := len(origin.Stack) - 1; index >= 0; index-- {
				call := e.result.Expansions[origin.Stack[index]].Call
				if call.File == e.original {
					return call.Span
				}
			}
		}
	}
	return n.Span()
}

func (e *emitter) ident(id *syntax.Ident) *goast.Ident {
	if id == nil {
		return nil
	}
	name := id.Name
	if symbol := e.file.Definitions[id.ID()]; symbol != nil && symbol.GoName != "" {
		name = symbol.GoName
	} else if reference := e.file.References[id.ID()]; reference.Symbol != nil && reference.Symbol.GoName != "" {
		name = reference.Symbol.GoName
	} else if mapped := e.file.GoNames[id.ID()]; mapped != "" {
		name = mapped
	}
	symbol := e.file.Definitions[id.ID()]
	if symbol == nil {
		symbol = e.file.References[id.ID()].Symbol
	}
	if mapped := e.symbolNames[symbol]; mapped != "" {
		name = mapped
	}
	if !gotoken.IsIdentifier(name) {
		e.fail(id, fmt.Sprintf("名字 %q 无法作为 Go 标识符输出", name))
	}
	out := &goast.Ident{NamePos: e.pos(id), Name: name}
	if sym := e.file.Definitions[id.ID()]; e.memberSymbols[sym] && name != id.Name {
		out.Name = id.Name
		e.memberNames[out] = name
	}
	return out
}

func tokenKind(kind token.Kind) gotoken.Token {
	for t := gotoken.ILLEGAL; t <= gotoken.TILDE; t++ {
		if t.String() == kind.String() {
			return t
		}
	}
	return gotoken.ILLEGAL
}

// 使用源文件行表保留独立注释与行尾注释的相对位置。
func (e *emitter) positions(f *goast.File, fset *gotoken.FileSet) {
	oldBase := e.base
	var boundaries []int
	spacing := 0
	offset := func(source int) int {
		return source + spacing*sort.SearchInts(boundaries, source+1)
	}
	if len(e.tree.Comments) > 0 {
		// Generated statements may occupy more bytes than their source span.
		// Reserve enough virtual offset space between source tokens so the Go
		// printer cannot advance through a later comment while printing them.
		var code bytes.Buffer
		if err := format.Node(&code, fset, f); err == nil {
			spacing = code.Len() + e.tokenFile.Size() + 1
		}
		for _, group := range e.tree.Comments {
			for _, comment := range group.List {
				boundaries = append(boundaries, int(comment.Span().Start))
			}
		}
		sort.Ints(boundaries)
		e.tokenFile = fset.AddFile(e.tokenFile.Name(), -1, offset(e.tokenFile.Size()))
		e.base = gotoken.Pos(e.tokenFile.Base())
		seen := map[goast.Node]bool{}
		goast.Inspect(f, func(node goast.Node) bool {
			if node == nil || seen[node] {
				return false
			}
			seen[node] = true
			value := reflect.ValueOf(node).Elem()
			for i := 0; i < value.NumField(); i++ {
				field := value.Field(i)
				if field.Type() == reflect.TypeOf(gotoken.NoPos) && field.Int() >= int64(oldBase) {
					field.SetInt(int64(e.base) + int64(offset(int(field.Int())-int(oldBase))))
				}
			}
			return true
		})
	}
	lines := []int{0}
	if len(e.tree.LineStarts) > 0 {
		lines = lines[:0]
		for _, source := range e.tree.LineStarts {
			lines = append(lines, offset(int(source)))
		}
		lines[0] = 0
	}
	e.tokenFile.SetLines(lines)
	for _, group := range e.tree.Comments {
		cg := &goast.CommentGroup{}
		for _, comment := range group.List {
			cg.List = append(cg.List, &goast.Comment{Slash: e.base + gotoken.Pos(offset(int(comment.Span().Start))), Text: comment.Text})
		}
		f.Comments = append(f.Comments, cg)
	}
}
