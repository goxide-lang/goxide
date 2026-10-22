package lower

import (
	"fmt"
	goast "go/ast"
	gotoken "go/token"
	"reflect"
	"sort"

	"github.com/goxide-lang/goxide/internal/resolve"

	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func lineOf(file *syntax.File, offset uint32) int {
	if file == nil {
		return 1
	}
	return max(1, sort.Search(len(file.LineStarts), func(index int) bool {
		return file.LineStarts[index] > offset
	}))
}

func (e *emitter) sourceLocation(node syntax.Node) string {
	name, file, offset := e.name, e.tree, node.Span().Start
	if e.result != nil {
		if origin, ok := e.result.Origins[resolve.Ref{File: e.name, ID: node.ID()}]; ok {
			if source := e.result.Files[origin.File]; source != nil {
				name, file, offset = origin.File, source.AST, origin.Span.Start
			}
		}
	}
	return fmt.Sprintf("%s:%d", name, lineOf(file, offset))
}

// Generated tokens have no source offsets of their own. Attribute them to the
// construct that produced them while retaining positions on ordinary children.
// Optional positions that affect Go syntax must remain absent.
func (e *emitter) anchor(node goast.Node, source syntax.Node) {
	if (!e.lineDirectives && e.validation == nil) || node == nil || source == nil {
		return
	}
	position := e.pos(source)
	var visit func(reflect.Value)
	visit = func(value reflect.Value) {
		if !value.IsValid() {
			return
		}
		if value.Kind() == reflect.Slice {
			for index := 0; index < value.Len(); index++ {
				visit(value.Index(index))
			}
			return
		}
		if (value.Kind() != reflect.Pointer && value.Kind() != reflect.Interface) || value.IsNil() {
			return
		}
		node, ok := value.Interface().(goast.Node)
		if !ok {
			return
		}
		// Temporary identifiers are shared by declarations and later uses.
		// Give each occurrence a position without relocating the shared name.
		if id, ok := node.(*goast.Ident); ok && !id.NamePos.IsValid() && value.CanSet() {
			copy := *id
			value.Set(reflect.ValueOf(&copy))
		}
		if value.Kind() == reflect.Interface {
			value = value.Elem()
		}
		value = value.Elem()
		for index := 0; index < value.NumField(); index++ {
			field := value.Field(index)
			if field.Type() != reflect.TypeOf(gotoken.NoPos) {
				visit(field)
				continue
			}
			name := value.Type().Field(index).Name
			switch node.(type) {
			case *goast.CallExpr:
				if name == "Ellipsis" {
					continue
				}
			case *goast.TypeSpec:
				if name == "Assign" {
					continue
				}
			case *goast.GenDecl:
				if name == "Lparen" || name == "Rparen" {
					continue
				}
			case *goast.FieldList, *goast.ImportSpec:
				continue
			}
			if field.Int() == 0 {
				field.SetInt(int64(position))
			}
		}
	}
	visit(reflect.ValueOf(node))
}

func (e *emitter) anchorStatements(statements []goast.Stmt, source syntax.Node) {
	for _, statement := range statements {
		e.anchor(statement, source)
	}
}

// A macro body can originate on another line even when its caller wrote a
// one-line function. Keep that body on separate output lines so //line can
// express its attribution without changing the source position of the close.
func multilineSourceBodies(file *goast.File, set *gotoken.FileSet) {
	goast.Inspect(file, func(node goast.Node) bool {
		var body *goast.BlockStmt
		switch node := node.(type) {
		case *goast.FuncDecl:
			body = node.Body
		case *goast.FuncLit:
			body = node.Body
		}
		if body == nil || set.Position(body.Lbrace).Line != set.Position(body.Rbrace).Line {
			return true
		}
		for _, statement := range body.List {
			if position := statement.Pos(); position.IsValid() && set.Position(position).Line != set.Position(body.Rbrace).Line {
				body.Lbrace = position
				break
			}
		}
		return true
	})
}
