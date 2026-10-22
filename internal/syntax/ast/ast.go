// Package ast defines the immutable syntax tree of a Goxide source file.
package ast

import (
	"github.com/goxide-lang/goxide/internal/syntax/token"
	"reflect"
)

type NodeID uint32
type ExpnID uint32
type SyntaxCtx uint32
type Span struct{ Start, End uint32 }
type Node interface {
	ID() NodeID
	Span() Span
}
type Base struct {
	NodeID NodeID
	Loc    Span
	Expn   ExpnID
}

func (b *Base) SetID(id NodeID) { b.NodeID = id }
func (b Base) ID() NodeID       { return b.NodeID }
func (b Base) Span() Span       { return b.Loc }

type Item interface {
	Node
	itemNode()
}
type Stmt interface {
	Node
	stmtNode()
}
type Expr interface {
	Node
	exprNode()
}
type Type interface {
	Node
	typeNode()
}
type Pat interface {
	Node
	patNode()
}
type TraitItem interface {
	Item
	traitItemNode()
}
type ItemBase struct {
	Base
	Attrs      []*Attr
	Vis        Visibility
	Doc        *CommentGroup
	Directives []*Directive
}
type StmtBase struct {
	Base
	Attrs []*Attr
}
type VisKind uint8

const (
	VisNone VisKind = iota
	VisPub
	VisScoped
)

type Visibility struct {
	Kind  VisKind
	Span  Span
	Scope *Path
}
type FuncMods struct{ Unsafe, Async, Const bool }
type TypeForm uint8

const (
	GoType TypeForm = iota
	StructSugar
)

type FuncForm uint8

const (
	GoFunc FuncForm = iota
	Bar
)

type VariantKind uint8

const (
	Unit VariantKind = iota
	Tuple
	Struct
)

type PathRoot uint8

const (
	NoRoot PathRoot = iota
	SuperRoot
)

type Delim uint8

const (
	NoDelim Delim = iota
	Paren
	Bracket
	Brace
)

type ChanDir uint8

const (
	SendRecv ChanDir = iota
	SendOnly
	RecvOnly
)

type File struct {
	Base
	NativeGo   bool // Read-only Go declaration view; function bodies remain with the Go compiler.
	Package    *Ident
	Attrs      []*Attr
	Items      []Item
	Comments   []*CommentGroup
	Directives []*Directive
	LineStarts []uint32      // Source line byte offsets, for preserving comment placement.
	Tokens     []token.Token // Original lexical units without comments, including EOF.
}
type Ident struct {
	Base
	Name string
	Raw  bool
	Ctx  SyntaxCtx
}
type Comment struct {
	Base
	Text string
}
type CommentGroup struct {
	Base
	List []*Comment
}
type Directive struct {
	Base
	Name, Text string
}
type Attr struct {
	Base
	Inner bool
	Path  *Path
	Args  *TokenTree
}
type TokenTree struct {
	Base
	Delim  Delim
	Tokens []TokenOrTree
}
type TokenOrTree struct {
	Token *token.Token
	Tree  *TokenTree
}
type Path struct {
	Base
	Root     PathRoot
	Segments []*PathSegment
}
type PathSegment struct {
	Base
	Name *Ident
	Args *GenericArgs
}
type GenericArgs struct {
	Base
	Args []*GenericArg
}
type GenericArg struct {
	Base
	Name  *Ident
	Type  Type // Bare names remain ambiguous until the parameter kind is known.
	Value Expr // Value arguments are mutually exclusive with Type.
}
type GenericParams struct {
	Base
	Params []*GenericParam
}
type GenericParam struct {
	Base
	Names      []*Ident
	Constraint Type
	Const      bool
}
type WhereClause struct {
	Base
	Predicates []*WherePredicate
}
type WherePredicate struct {
	Base
	Type   Type
	Bounds []Type
}
type Field struct {
	Base
	Attrs []*Attr
	Vis   Visibility
	Names []*Ident
	Type  Type
	Tag   *BasicLit
	Doc   *CommentGroup
}
type Param struct {
	Base
	Attrs    []*Attr
	Names    []*Ident
	Type     Type
	Variadic bool
}
type Variant struct {
	Base
	Attrs  []*Attr
	Name   *Ident
	Kind   VariantKind
	Fields []*Field
	Disc   Expr
}
type UseTree struct {
	Base
	Path     *Path
	Alias    *Ident
	Glob     bool
	Children []*UseTree
}

type UseDecl struct {
	ItemBase
	Tree *UseTree
}
type ModDecl struct {
	ItemBase
	Name   *Ident
	Items  []Item
	Inline bool
}
type FuncDecl struct {
	ItemBase
	Name        *Ident
	Path        *Path
	Recv        []*Param
	Params      []*Param
	Results     []*Param
	ResultParen bool
	Mods        FuncMods
	Generics    *GenericParams
	Where       *WhereClause
	Body        *Block
}
type TypeDecl struct {
	ItemBase
	Name     *Ident
	Type     Type
	Alias    bool
	Form     TypeForm
	Generics *GenericParams
	Where    *WhereClause
	Grouped  bool
	Specs    []*TypeSpec
}
type TypeSpec struct {
	ItemBase
	Name     *Ident
	Type     Type
	Alias    bool
	Form     TypeForm
	Generics *GenericParams
	Where    *WhereClause
}
type EnumDecl struct {
	ItemBase
	Name     *Ident
	Generics *GenericParams
	Where    *WhereClause
	Variants []*Variant
}
type TraitDecl struct {
	ItemBase
	Name     *Ident
	Generics *GenericParams
	Where    *WhereClause
	Items    []TraitItem
}
type ImplDecl struct {
	ItemBase
	Trait    Type
	Type     Type
	Generics *GenericParams
	Where    *WhereClause
	Items    []Item
}
type DeclareDecl struct {
	ItemBase
	Decl Item
}
type MacroRulesDecl struct {
	ItemBase
	Name  *Ident
	Rules []*MacroRule
}
type MacroRule struct {
	Base
	Matcher  *TokenTree
	Template *TokenTree
}
type MacroItem struct {
	ItemBase
	Path *Path
	Args *TokenTree
}
type ConstDecl struct {
	ItemBase
	Grouped bool
	Specs   []*ValueSpec
}
type VarDecl struct {
	ItemBase
	Grouped bool
	Specs   []*ValueSpec
}
type ValueSpec struct {
	ItemBase
	Names  []*Ident
	Type   Type
	Values []Expr
}
type BadItem struct {
	ItemBase
	Text string
}

type PathType struct {
	Base
	Path *Path
}
type RefType struct {
	Base
	Elem Type
	Mut  bool
}
type PointerType struct {
	Base
	Elem Type
}
type SliceType struct {
	Base
	Elem Type
}
type ArrayType struct {
	Base
	Len      Expr
	Elem     Type
	Ellipsis bool
}
type MapType struct {
	Base
	Key, Value Type
}
type ChanType struct {
	Base
	Dir  ChanDir
	Elem Type
}
type FuncType struct {
	Base
	Params      []*Param
	Results     []*Param
	ResultParen bool
}
type StructType struct {
	Base
	Fields []*Field
}
type InterfaceType struct {
	Base
	Methods []*InterfaceMethod
}
type InterfaceMethod struct {
	Base
	Attrs       []*Attr
	Name        *Ident
	Params      []*Param
	Results     []*Param
	ResultParen bool
	Type        Type
}
type UnionType struct {
	Base
	Terms []Type
}
type ApproxType struct {
	Base
	Type Type
}
type SelfType struct{ Base }
type ProjectionType struct {
	Base
	X    Type
	Item *Ident
	Args *GenericArgs
	Dot  bool
}
type ParenType struct {
	Base
	Type Type
}
type MacroType struct {
	Base
	Path *Path
	Args *TokenTree
}
type BadType struct {
	Base
	Text string
}

type BasicLit struct {
	Base
	Kind  token.Kind
	Value string
}

// BoolLit is a compiler-produced boolean constant, independent of name lookup.
type BoolLit struct {
	Base
	Value bool
}
type CompositeLit struct {
	Base
	Type Type
	Elts []Expr
}
type KeyValue struct {
	Base
	Key, Value Expr
}
type ParenExpr struct {
	Base
	X Expr
}
type SelectorExpr struct {
	Base
	X      Expr
	Sel    *Ident
	Static bool
}
type IndexExpr struct {
	Base
	X       Expr
	Indices []Expr
}
type SliceExpr struct {
	Base
	X, Low, High, Max Expr
	Slice3            bool
}
type TypeAssertExpr struct {
	Base
	X    Expr
	Type Type
}
type CallExpr struct {
	Base
	Fun      Expr
	Args     []Expr
	Ellipsis bool
}
type StarExpr struct {
	Base
	X Expr
}
type UnaryExpr struct {
	Base
	Op token.Kind
	X  Expr
}
type BinaryExpr struct {
	Base
	X  Expr
	Op token.Kind
	Y  Expr
}
type FuncLit struct {
	Base
	Form        FuncForm
	Mods        FuncMods
	Params      []*Param
	Result      Type
	Results     []*Param
	ResultParen bool
	Body        *Block
	Expr        Expr
}
type PathExpr struct {
	Base
	Path *Path
}
type MatchExpr struct {
	Base
	X    Expr
	Arms []*MatchArm
}
type MatchArm struct {
	Base
	Attrs []*Attr
	Pat   Pat
	Guard Expr
	Body  Expr
	Block *Block
}
type TryExpr struct {
	Base
	X Expr
}
type RefExpr struct {
	Base
	X   Expr
	Mut bool
}
type UnsafeExpr struct {
	Base
	X Expr
}
type MacroExpr struct {
	Base
	Path *Path
	Args *TokenTree
}
type QualifiedSelectorExpr struct {
	Base
	X      Expr
	Sel    *Ident
	Trait  Type
	Static bool
}
type AwaitExpr struct {
	Base
	X Expr
}
type TypeExpr struct {
	Base
	Type Type
}
type BadExpr struct {
	Base
	Text string
}

type AssignStmt struct {
	StmtBase
	Lhs []Expr
	Tok token.Kind
	Rhs []Expr
}
type IncDecStmt struct {
	StmtBase
	X   Expr
	Tok token.Kind
}
type ExprStmt struct {
	StmtBase
	X Expr
}
type SendStmt struct {
	StmtBase
	Chan, Value Expr
}
type GoStmt struct {
	StmtBase
	Call Expr
}
type DeferStmt struct {
	StmtBase
	Call Expr
}
type ReturnStmt struct {
	StmtBase
	Results []Expr
}
type BranchStmt struct {
	StmtBase
	Tok   token.Kind
	Label *Ident
}
type Block struct {
	StmtBase
	List []Stmt
}
type IfStmt struct {
	StmtBase
	Init Stmt
	Cond Expr
	Body *Block
	Else Stmt
}
type SwitchStmt struct {
	StmtBase
	Init  Stmt
	Tag   Expr
	Cases []*CaseClause
}
type TypeSwitchStmt struct {
	StmtBase
	Init   Stmt
	Assign Stmt
	Cases  []*CaseClause
}
type CaseClause struct {
	StmtBase
	List    []Expr
	Types   []Type
	Body    []Stmt
	Default bool
}
type SelectStmt struct {
	StmtBase
	Cases []*CommClause
}
type CommClause struct {
	StmtBase
	Comm    Stmt
	Body    []Stmt
	Default bool
}
type ForStmt struct {
	StmtBase
	Init Stmt
	Cond Expr
	Post Stmt
	Body *Block
}
type RangeStmt struct {
	StmtBase
	Key, Value Expr
	Tok        token.Kind
	X          Expr
	Body       *Block
}
type LabeledStmt struct {
	StmtBase
	Label *Ident
	Stmt  Stmt
}
type DeclStmt struct {
	StmtBase
	Decl Item
}
type EmptyStmt struct {
	StmtBase
	Implicit bool
}
type LetStmt struct {
	StmtBase
	Pat   Pat
	Type  Type
	Value Expr
}
type LoopStmt struct {
	StmtBase
	Body *Block
}
type WhileStmt struct {
	StmtBase
	Cond Expr
	Body *Block
}
type BadStmt struct {
	StmtBase
	Text string
}

type WildPat struct{ Base }
type BindPat struct {
	Base
	Name     *Ident
	Ref, Mut bool
	Sub      Pat
}
type LitPat struct {
	Base
	Value Expr
}
type PathPat struct {
	Base
	Path *Path
}
type TuplePat struct {
	Base
	Path *Path
	Elts []Pat
}
type StructPat struct {
	Base
	Path   *Path
	Fields []*PatField
	Rest   bool
}
type PatField struct {
	Base
	Name *Ident
	Pat  Pat
}
type OrPat struct {
	Base
	Alts []Pat
}
type RangePat struct {
	Base
	Low, High Expr
	Inclusive bool
}
type BadPat struct {
	Base
	Text string
}

func (*Ident) exprNode()                 {}
func (*UseDecl) itemNode()               {}
func (*ModDecl) itemNode()               {}
func (*FuncDecl) itemNode()              {}
func (*TypeDecl) itemNode()              {}
func (*EnumDecl) itemNode()              {}
func (*TraitDecl) itemNode()             {}
func (*ImplDecl) itemNode()              {}
func (*DeclareDecl) itemNode()           {}
func (*MacroRulesDecl) itemNode()        {}
func (*MacroItem) itemNode()             {}
func (*ConstDecl) itemNode()             {}
func (*VarDecl) itemNode()               {}
func (*BadItem) itemNode()               {}
func (*PathType) typeNode()              {}
func (*RefType) typeNode()               {}
func (*PointerType) typeNode()           {}
func (*SliceType) typeNode()             {}
func (*ArrayType) typeNode()             {}
func (*MapType) typeNode()               {}
func (*ChanType) typeNode()              {}
func (*FuncType) typeNode()              {}
func (*StructType) typeNode()            {}
func (*InterfaceType) typeNode()         {}
func (*UnionType) typeNode()             {}
func (*ApproxType) typeNode()            {}
func (*SelfType) typeNode()              {}
func (*ProjectionType) typeNode()        {}
func (*ParenType) typeNode()             {}
func (*MacroType) typeNode()             {}
func (*BadType) typeNode()               {}
func (*BasicLit) exprNode()              {}
func (*BoolLit) exprNode()               {}
func (*CompositeLit) exprNode()          {}
func (*KeyValue) exprNode()              {}
func (*ParenExpr) exprNode()             {}
func (*SelectorExpr) exprNode()          {}
func (*IndexExpr) exprNode()             {}
func (*SliceExpr) exprNode()             {}
func (*TypeAssertExpr) exprNode()        {}
func (*CallExpr) exprNode()              {}
func (*StarExpr) exprNode()              {}
func (*UnaryExpr) exprNode()             {}
func (*BinaryExpr) exprNode()            {}
func (*FuncLit) exprNode()               {}
func (*PathExpr) exprNode()              {}
func (*MatchExpr) exprNode()             {}
func (*TryExpr) exprNode()               {}
func (*RefExpr) exprNode()               {}
func (*UnsafeExpr) exprNode()            {}
func (*MacroExpr) exprNode()             {}
func (*QualifiedSelectorExpr) exprNode() {}
func (*AwaitExpr) exprNode()             {}
func (*TypeExpr) exprNode()              {}
func (*BadExpr) exprNode()               {}
func (*AssignStmt) stmtNode()            {}
func (*IncDecStmt) stmtNode()            {}
func (*ExprStmt) stmtNode()              {}
func (*SendStmt) stmtNode()              {}
func (*GoStmt) stmtNode()                {}
func (*DeferStmt) stmtNode()             {}
func (*ReturnStmt) stmtNode()            {}
func (*BranchStmt) stmtNode()            {}
func (*Block) stmtNode()                 {}
func (*IfStmt) stmtNode()                {}
func (*SwitchStmt) stmtNode()            {}
func (*TypeSwitchStmt) stmtNode()        {}
func (*CaseClause) stmtNode()            {}
func (*SelectStmt) stmtNode()            {}
func (*CommClause) stmtNode()            {}
func (*ForStmt) stmtNode()               {}
func (*RangeStmt) stmtNode()             {}
func (*LabeledStmt) stmtNode()           {}
func (*DeclStmt) stmtNode()              {}
func (*EmptyStmt) stmtNode()             {}
func (*LetStmt) stmtNode()               {}
func (*LoopStmt) stmtNode()              {}
func (*WhileStmt) stmtNode()             {}
func (*BadStmt) stmtNode()               {}
func (*WildPat) patNode()                {}
func (*BindPat) patNode()                {}
func (*LitPat) patNode()                 {}
func (*PathPat) patNode()                {}
func (*TuplePat) patNode()               {}
func (*StructPat) patNode()              {}
func (*OrPat) patNode()                  {}
func (*RangePat) patNode()               {}
func (*BadPat) patNode()                 {}
func (*FuncDecl) traitItemNode()         {}
func (*TypeDecl) traitItemNode()         {}
func (*MacroItem) traitItemNode()        {}

// Walk visits nodes in field order. Returning false skips a node's children.
func Walk(n Node, visit func(Node) bool) {
	seen := make(map[Node]bool)
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		if !v.IsValid() {
			return
		}
		if v.Kind() == reflect.Interface {
			if !v.IsNil() {
				walk(v.Elem())
			}
			return
		}
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return
			}
			if node, ok := v.Interface().(Node); ok {
				if seen[node] {
					return
				}
				seen[node] = true
				if !visit(node) {
					return
				}
			}
			walk(v.Elem())
			return
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if v.Field(i).Type() == reflect.TypeOf(Base{}) {
					continue
				}
				walk(v.Field(i))
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		}
	}
	walk(reflect.ValueOf(n))
}
func (*BadItem) traitItemNode() {}
