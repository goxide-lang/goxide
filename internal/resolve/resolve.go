// Package resolve 在只读 AST 上解析名字，并按文件、NodeID 保存语义旁表。
// 所有作用域均以 (名字, Ident.Ctx) 为键；宏展开器可以在调用 Resolve 前
// 注入带 SyntaxCtx 的标识符及文件内唯一的 NodeID，无需修改查找接口。
package resolve

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/hxsf/goxide/internal/syntax/ast"
)

type Key struct {
	Name string
	Ctx  ast.SyntaxCtx
}

// Ref 包含文件身份，因为 NodeID 只在文件内唯一。
type Ref struct {
	File string
	ID   ast.NodeID
}

type Kind uint8

const (
	Variable Kind = iota
	Constant
	Function
	Type
	TypeParam
	Enum
	Trait
	Variant
	Module
	Macro
	Import
	Field
	Label
	Builtin
)

type Visibility uint8

const (
	Private Visibility = iota
	Public
)

type Symbol struct {
	Name       Key
	Kind       Kind
	Decl       Ref
	Visibility Visibility
	GoName     string
	Target     []string
	Members    *Scope
	AliasOf    *Symbol // 已知的本地 use 目标；外部目标由 Target 表示。
}

type ScopeKind uint8

const (
	PreludeScope ScopeKind = iota
	PackageScope
	FileScope
	FunctionScope
	BlockScope
	MemberScope
)

type Scope struct {
	Kind     ScopeKind
	Parent   *Scope
	Bindings map[Key]*Symbol
	Aliases  map[Key][]*Symbol
	Globs    []*UseBinding
	Labels   map[Key]*Symbol
}

// Resolution 的 Deferred 表示成员需要外部包或类型信息。
// Candidates 非空表示歧义，Symbol 为空表示尚无唯一绑定。
type Resolution struct {
	Symbol     *Symbol
	Path       []string
	Deferred   bool
	Candidates []*Symbol
}

type UseBinding struct {
	Decl                Ref
	Name                Key
	Target              []string
	Glob, Blank, Public bool
	Symbol              *Symbol
}

type File struct {
	Name        string
	AST         *ast.File
	Scope       *Scope
	Definitions map[ast.NodeID]*Symbol
	References  map[ast.NodeID]Resolution
	Visibility  map[ast.NodeID]Visibility
	GoNames     map[ast.NodeID]string
	Scopes      map[ast.NodeID]*Scope
	Uses        []*UseBinding
}

type Package struct {
	Dir, Name string
	Files     []*File
	Scope     *Scope
}

type Diagnostic struct {
	File          string
	Span          ast.Span
	Code, Message string
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.File, d.Span.Start, d.Span.End, d.Message)
}

type Result struct {
	Packages    []*Package
	Files       map[string]*File
	Diagnostics []Diagnostic
}

// External 是调用方提供的公开成员；查找不到返回空切片，多项表示歧义。
type External struct {
	Name       Key
	Kind       Kind
	GoName     string
	Visibility Visibility
}

// Importer 只查询显式路径或 glob 成员，不加载文件或实现跨包解析。
// nil Importer 保留外部路径为 Deferred；具体模块路径始终保存在 Uses 中。
type Importer interface {
	Lookup(path []string, name Key) []External
}

// Resolve 按目录和 package 子句分组输入。调用方提供选中的 .gox AST；
// 返回值的每个文件旁表独立，输入树不会被修改。
func Resolve(files map[string]*ast.File, importer Importer) *Result {
	out := &Result{Files: make(map[string]*File)}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	type packageKey struct{ dir, name string }
	packages := make(map[packageKey]*Package)
	for _, name := range names {
		tree := files[name]
		if tree == nil {
			continue
		}
		pn := ""
		if tree.Package != nil {
			pn = tree.Package.Name
		}
		k := packageKey{filepath.Dir(name), pn}
		p := packages[k]
		if p == nil {
			p = &Package{Dir: k.dir, Name: pn, Scope: newScope(PackageScope, prelude())}
			packages[k] = p
			out.Packages = append(out.Packages, p)
		}
		f := &File{Name: name, AST: tree, Scope: newScope(FileScope, p.Scope),
			Definitions: make(map[ast.NodeID]*Symbol), References: make(map[ast.NodeID]Resolution),
			Visibility: make(map[ast.NodeID]Visibility), GoNames: make(map[ast.NodeID]string), Scopes: make(map[ast.NodeID]*Scope)}
		f.Scopes[tree.ID()] = f.Scope
		p.Files = append(p.Files, f)
		out.Files[name] = f
	}
	for _, p := range out.Packages {
		r := &resolver{result: out, pkg: p, importer: importer, useStates: make(map[*UseBinding]uint8), useNodes: make(map[*UseBinding]*ast.UseTree), useFiles: make(map[*UseBinding]*File), symbolUses: make(map[*Symbol]*UseBinding)}
		r.collectPackage()
		r.collectUses()
		r.attachMembers()
		for _, f := range p.Files {
			r.file = f
			r.validateIdents()
			r.walkFile()
		}
	}
	sort.SliceStable(out.Diagnostics, func(i, j int) bool {
		a, b := out.Diagnostics[i], out.Diagnostics[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Span.Start < b.Span.Start
	})
	return out
}

type resolver struct {
	result     *Result
	pkg        *Package
	file       *File
	importer   Importer
	useStates  map[*UseBinding]uint8
	useNodes   map[*UseBinding]*ast.UseTree
	useFiles   map[*UseBinding]*File
	symbolUses map[*Symbol]*UseBinding
}

func (r *resolver) ref(n ast.Node) Ref { return Ref{r.file.Name, n.ID()} }
func (r *resolver) diag(n ast.Node, code, msg string) {
	r.result.Diagnostics = append(r.result.Diagnostics, Diagnostic{File: r.file.Name, Span: n.Span(), Code: code, Message: msg})
}
