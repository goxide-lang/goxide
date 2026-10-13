// Package lower 将检查后的语法树转换为格式化的 Go 源码。
package lower

import (
	"bytes"
	"fmt"
	goast "go/ast"
	"go/format"
	"go/importer"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

type Severity uint8

const Error Severity = 0

type Diagnostic struct {
	Severity Severity
	File     string
	Span     syntax.Span
	Message  string
	Origin   resolve.Origin
}

func (d *Diagnostic) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.File, d.Span.Start, d.Span.End, d.Message)
}

// Lower 的输出键沿用输入文件名；调用方写盘时将 .gox 后缀换成 .go。
// files 和 info 必须描述同一批 AST。失败时返回 nil，不返回部分输出。
func Lower(files map[string]*syntax.File, info *check.Info) (map[string][]byte, error) {
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
	fset := gotoken.NewFileSet()
	var emitters []*emitter
	memberSymbols := make(map[*resolve.Symbol]bool)
	if res != nil {
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
		e := &emitter{info: info, name: name, tree: tree, result: res, imports: make(map[string]*importUse), reserved: make(map[string]bool), memberSymbols: memberSymbols, memberNames: make(map[*goast.Ident]string)}
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
		e.tokenFile = fset.AddFile(name, -1, int(tree.Span().End)+256)
		e.base = gotoken.Pos(e.tokenFile.Base())
		syntax.Walk(tree, func(n syntax.Node) bool {
			if a, ok := n.(*syntax.Attr); ok {
				e.fail(a, "属性与派生尚未降低")
				return false
			}
			return true
		})
		e.reserveNames()
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
		if e.err != nil {
			return nil, e.err
		}
		for _, diagnostic := range res.Diagnostics {
			if diagnostic.File == name && diagnostic.Severity == resolve.Error {
				return nil, &Diagnostic{File: name, Span: diagnostic.Span, Message: diagnostic.Message, Origin: diagnostic.Origin}
			}
		}
		e.addImports(f)
		e.positions(f)
		e.goFile = f
		emitters = append(emitters, e)
	}
	bindMembers(fset, emitters)
	out := make(map[string][]byte, len(files))
	for _, e := range emitters {
		var buf bytes.Buffer
		if err := format.Node(&buf, fset, e.goFile); err != nil {
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
	needed := false
	for _, e := range emitters {
		key := filepath.ToSlash(filepath.Dir(e.name))
		groups[key] = append(groups[key], e)
		needed = needed || len(e.memberNames) > 0
	}
	if !needed {
		return
	}
	var renamer memberRenamer
	loaded := map[string]*types.Package{}
	loadedGroups := map[*emitter]*types.Package{}
	loading := map[*emitter]bool{}
	standard := importer.Default()
	indexSourcePackages(groups, emitters, standard)
	var load goImporter
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
		var files []*goast.File
		names := map[*goast.Ident]string{}
		for _, e := range group {
			files = append(files, e.goFile)
			for id, name := range e.memberNames {
				names[id] = name
			}
		}
		p := renamer.rename(path, fset, files, names, load)
		loaded[path] = p
		loadedGroups[identity] = p
		loading[identity] = false
		return p, nil
	}
	for _, e := range emitters {
		_, _ = load(filepath.ToSlash(filepath.Dir(e.name)))
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
	matchDepth    int
	breakTarget   *breakTarget
	info          *check.Info
	pending       []goast.Stmt
	replacements  map[syntax.Expr]goast.Expr
	tempIndex     int
	base          gotoken.Pos
	tokenFile     *gotoken.File
	goFile        *goast.File
	memberSymbols map[*resolve.Symbol]bool
	memberNames   map[*goast.Ident]string
	name          string
	tree          *syntax.File
	file          *resolve.File
	result        *resolve.Result
	err           *Diagnostic
	imports       map[string]*importUse
	reserved      map[string]bool
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
}
func (e *emitter) pos(n syntax.Node) gotoken.Pos {
	if n == nil || reflect.ValueOf(n).IsNil() {
		return gotoken.NoPos
	}
	return gotoken.Pos(n.Span().Start) + e.base
}
func (e *emitter) end(n syntax.Node) gotoken.Pos {
	if n == nil || reflect.ValueOf(n).IsNil() {
		return gotoken.NoPos
	}
	return gotoken.Pos(n.Span().End) + e.base
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
func (e *emitter) positions(f *goast.File) {
	lines := []int{0}
	if len(e.tree.LineStarts) > 0 {
		lines = lines[:0]
		for _, offset := range e.tree.LineStarts {
			lines = append(lines, int(offset))
		}
	}
	e.tokenFile.SetLines(lines)
	for _, group := range e.tree.Comments {
		cg := &goast.CommentGroup{}
		for _, comment := range group.List {
			cg.List = append(cg.List, &goast.Comment{Slash: e.pos(comment), Text: comment.Text})
		}
		f.Comments = append(f.Comments, cg)
	}
}
