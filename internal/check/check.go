// Package check checks the language rules that are not expressed by Go syntax.
package check

import (
	"fmt"
	"go/constant"
	"go/types"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hxsf/goxide/internal/resolve"
	"github.com/hxsf/goxide/internal/syntax/ast"
)

type Severity uint8

const Error Severity = 0

const (
	NonExhaustive       = "match-non-exhaustive"
	UnreachableArm      = "match-unreachable-arm"
	IncomparablePayload = "match-incomparable-payload"
	ArmTypeMismatch     = "match-arm-type"
	TryReturnType       = "try-return-type"
	GenericConstraint   = "generic-constraint"
	VariantMismatch     = "match-variant"
	EnumGoNameConflict  = "enum-go-name-conflict"
)

type Diagnostic struct {
	File          string
	Span          ast.Span
	Code, Message string
	Severity      Severity
	Origin        resolve.Origin
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", d.File, d.Span.Start, d.Span.End, d.Message)
}

type Field struct {
	Name, GoName string
	Type         types.Type
}

type Variant struct {
	Name, GoName string
	Decl         *ast.Variant
	Fields       []Field
}

type Enum struct {
	Name, GoName string
	Decl         *ast.EnumDecl
	Type         *types.Named
	Variants     []*Variant
	Builtin      bool
}

type Instance struct {
	Enum *Enum
	Args []types.Type
}

func (i *Instance) Variant(name string) *Variant {
	for _, v := range i.Enum.Variants {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// Fields substitutes the enum's actual type arguments into a variant's payload.
func (i *Instance) Fields(v *Variant) []Field {
	fields := append([]Field(nil), v.Fields...)
	params := i.Enum.Type.TypeParams()
	sub := make(map[*types.TypeParam]types.Type)
	for j, arg := range i.Args {
		if j < params.Len() {
			sub[params.At(j)] = arg
		}
	}
	for j := range fields {
		fields[j].Type = substitute(fields[j].Type, sub)
	}
	return fields
}

type Match struct {
	Enum      *Instance
	Type      types.Type
	Statement bool
}

type Constructor struct {
	Enum    *Instance
	Variant *Variant
}

type Try struct {
	Input, Output *Instance
	Value, Error  types.Type
	Conversion    *resolve.Symbol
}

type Info struct {
	Resolve      *resolve.Result
	Diagnostics  []Diagnostic
	Types        map[ast.Expr]types.Type
	Enums        map[*ast.EnumDecl]*Enum
	Matches      map[*ast.MatchExpr]*Match
	Tries        map[*ast.TryExpr]*Try
	Constructors map[ast.Expr]*Constructor
	Symbols      map[*resolve.Symbol]types.Type
	PackagePaths map[string]string
	enums        map[*types.Named]*Enum
}

func (r *Info) EnumOf(t types.Type) *Instance {
	if t == nil {
		return nil
	}
	n, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return nil
	}
	e := r.enums[n.Origin()]
	if e == nil {
		return nil
	}
	i := &Instance{Enum: e}
	for j := 0; j < n.TypeArgs().Len(); j++ {
		i.Args = append(i.Args, n.TypeArgs().At(j))
	}
	if len(i.Args) == 0 {
		for j := 0; j < n.TypeParams().Len(); j++ {
			i.Args = append(i.Args, n.TypeParams().At(j))
		}
	}
	return i
}

// Check leaves the input trees and resolver tables unchanged.
func Check(files map[string]*ast.File, res *resolve.Result) (*Info, []Diagnostic) {
	return CheckWithConversions(files, res, nil)
}

// CheckWithConversions registers only the explicitly supplied one-step helpers.
func CheckWithConversions(files map[string]*ast.File, res *resolve.Result, helpers []resolve.Ref) (*Info, []Diagnostic) {
	r := &Info{Resolve: res, Types: make(map[ast.Expr]types.Type), Enums: make(map[*ast.EnumDecl]*Enum), Matches: make(map[*ast.MatchExpr]*Match), Tries: make(map[*ast.TryExpr]*Try), Constructors: make(map[ast.Expr]*Constructor), Symbols: make(map[*resolve.Symbol]types.Type), enums: make(map[*types.Named]*Enum)}
	if res == nil {
		return r, nil
	}
	c := &checker{out: r, files: files, named: make(map[*resolve.Symbol]*types.Named), variants: make(map[*resolve.Symbol]*Constructor), builtins: make(map[string]*Enum), packages: make(map[*resolve.Package]*types.Package), imported: make(map[string]*types.Package), constants: make(map[*resolve.Symbol]constant.Value), declarations: make(map[*resolve.Symbol]*typeDeclaration)}
	c.builtinEnums()
	r.PackagePaths = sourcePackagePaths(res)
	for _, p := range res.Packages {
		c.pkg = types.NewPackage(p.Dir, p.Name)
		c.packages[p] = c.pkg
		for _, f := range p.Files {
			c.file = f
			c.collect(f.AST.Items)
		}
	}
	for _, p := range res.Packages {
		c.pkg = c.packages[p]
		for _, f := range p.Files {
			c.file = f
			c.prepareGenerics(f.AST.Items)
		}
	}
	for _, p := range res.Packages {
		c.pkg = c.packages[p]
		for _, f := range p.Files {
			c.file = f
			c.signatures(f.AST.Items)
		}
	}
	c.enumNames()
	for _, ref := range helpers {
		if f := res.Files[ref.File]; f != nil {
			if s := f.Definitions[ref.ID]; s != nil {
				c.conversions = append(c.conversions, s)
			}
		}
	}
	for _, p := range res.Packages {
		c.pkg = c.packages[p]
		for _, f := range p.Files {
			c.file = f
			c.globals(f.AST.Items)
		}
	}
	for _, p := range res.Packages {
		c.pkg = c.packages[p]
		for _, f := range p.Files {
			c.file = f
			for _, item := range f.AST.Items {
				c.item(item)
			}
		}
	}
	for _, instance := range c.instances {
		c.validateInstance(instance)
	}
	sort.SliceStable(r.Diagnostics, func(i, j int) bool {
		a, b := r.Diagnostics[i], r.Diagnostics[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Span.Start < b.Span.Start
	})
	return r, r.Diagnostics
}

type checker struct {
	out          *Info
	files        map[string]*ast.File
	file         *resolve.File
	pkg          *types.Package
	packages     map[*resolve.Package]*types.Package
	named        map[*resolve.Symbol]*types.Named
	variants     map[*resolve.Symbol]*Constructor
	builtins     map[string]*Enum
	imported     map[string]*types.Package
	returns      []types.Type
	conversions  []*resolve.Symbol
	constants    map[*resolve.Symbol]constant.Value
	instances    []genericInstance
	declarations map[*resolve.Symbol]*typeDeclaration
}

func (c *checker) diag(n ast.Node, code, message string) {
	span := n.Span()
	for _, d := range c.out.Diagnostics {
		if d.File == c.file.Name && d.Span == span && d.Code == code && d.Message == message {
			return
		}
	}
	origin, ok := c.out.Resolve.Origins[resolve.Ref{File: c.file.Name, ID: n.ID()}]
	if !ok {
		origin = resolve.Origin{File: c.file.Name, Span: span}
	}
	c.out.Diagnostics = append(c.out.Diagnostics, Diagnostic{File: c.file.Name, Span: span, Code: code, Message: message, Origin: origin})
}

func (c *checker) symbol(n ast.Node) *resolve.Symbol {
	if n == nil {
		return nil
	}
	s := c.file.Definitions[n.ID()]
	if s == nil {
		s = c.file.References[n.ID()].Symbol
	}
	for s != nil && s.AliasOf != nil {
		s = s.AliasOf
	}
	if s != nil && len(s.Target) > 0 {
		if local := c.localSymbol(s.Target); local != nil {
			s = local
		}
	}
	if s == nil {
		s = c.localSymbol(c.file.References[n.ID()].Path)
	}
	return s
}

func (c *checker) localSymbol(target []string) *resolve.Symbol {
	if len(target) < 2 {
		return nil
	}
	joined := strings.Join(target, "/")
	var found *resolve.Symbol
	for _, p := range c.out.Resolve.Packages {
		dir := filepath.ToSlash(p.Dir)
		if !strings.HasPrefix(joined, dir+"/") {
			dir = c.out.PackagePaths[dir]
		}
		if dir == "" || !strings.HasPrefix(joined, dir+"/") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(joined, dir+"/"), "/")
		scope := p.Scope
		var symbol *resolve.Symbol
		for _, part := range parts {
			if scope == nil {
				symbol = nil
				break
			}
			symbol = scope.Bindings[resolve.Key{Name: part}]
			if symbol == nil {
				break
			}
			scope = symbol.Members
		}
		if symbol != nil {
			if found != nil && found != symbol {
				return nil
			}
			found = symbol
		}
	}
	return found
}

func valid(t types.Type) bool { return t != nil && t != types.Typ[types.Invalid] }
func defaultType(t types.Type) types.Type {
	if t == nil {
		return types.Typ[types.Invalid]
	}
	return types.Default(t)
}
