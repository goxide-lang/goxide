package lower

import (
	"fmt"
	goast "go/ast"
	"go/types"
	"sort"
	"strconv"
	"strings"

	"github.com/hxsf/goxide/internal/check"
	"github.com/hxsf/goxide/internal/resolve"
	syntax "github.com/hxsf/goxide/internal/syntax/ast"
	"github.com/hxsf/goxide/internal/syntax/token"
)

type errorAttributes struct {
	message      string
	hasMessage   bool
	source, from bool
}

func readErrorAttributes(attrs []*syntax.Attr) (errorAttributes, error) {
	var out errorAttributes
	seen := map[string]bool{}
	for _, attr := range attrs {
		name := resolve.AttributeName(attr)
		if name != "error" && name != "source" && name != "from" {
			continue
		}
		if seen[name] {
			return out, fmt.Errorf("duplicate %s attribute", name)
		}
		seen[name] = true
		if name == "error" {
			if attr.Args == nil || len(attr.Args.Tokens) != 1 || attr.Args.Tokens[0].Token == nil || attr.Args.Tokens[0].Token.Kind != token.STRING {
				return out, fmt.Errorf("error attribute requires one string literal")
			}
			var err error
			out.message, err = strconv.Unquote(attr.Args.Tokens[0].Token.Text)
			if err != nil {
				return out, err
			}
			out.hasMessage = true
			continue
		}
		if attr.Args != nil {
			return out, fmt.Errorf("%s attribute takes no arguments", name)
		}
		if name == "source" {
			out.source = true
		} else {
			out.from = true
		}
	}
	return out, nil
}

func (e *emitter) errorDeclarations() []goast.Decl {
	var plans []*check.Derive
	for _, d := range e.info.Derives {
		if d.File == e.name && d.Requests["Error"] {
			plans = append(plans, d)
		}
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].Node.Span().Start != plans[j].Node.Span().Start {
			return plans[i].Node.Span().Start < plans[j].Node.Span().Start
		}
		return plans[i].Type.Obj().Name() < plans[j].Type.Obj().Name()
	})
	var declarations []goast.Decl
	for _, d := range plans {
		var text string
		var err error
		if enum := e.info.EnumOf(d.Type); enum != nil {
			text, err = e.enumError(d, enum)
		} else {
			text, err = e.structError(d)
		}
		if err != nil {
			e.fail(d.Node, err.Error())
			return nil
		}
		declarations = append(declarations, e.deriveParse(d.Node, text)...)
	}
	return declarations
}

func (e *emitter) enumError(d *check.Derive, enum *check.Instance) (string, error) {
	if d.Type.TypeParams().Len() != 0 {
		return "", fmt.Errorf("Error requires a concrete enum")
	}
	var text strings.Builder
	from := false
	for _, variant := range enum.Enum.Variants {
		attrs, err := readErrorAttributes(variant.Decl.Attrs)
		if err != nil {
			return "", err
		}
		if !attrs.hasMessage {
			return "", fmt.Errorf("Error variant %s requires an error message", variant.Name)
		}
		if attrs.from && from {
			return "", fmt.Errorf("Error requires at most one from variant")
		}
		from = from || attrs.from
		if attrs.source || attrs.from {
			if len(variant.Fields) != 1 || !types.Identical(types.Unalias(variant.Fields[0].Type), types.Universe.Lookup("error").Type()) {
				return "", fmt.Errorf("source/from variant %s requires one error payload", variant.Name)
			}
		}
		receiver := e.fresh("Error").Name
		fields := map[string]check.Field{}
		for _, field := range variant.Fields {
			fields[field.Name] = field
		}
		message, err := e.errorMessage(attrs.message, receiver, fields)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&text, "func (%s *%s) Error() string { if %s == nil { panic(%q) }; return %s }\n", receiver, variant.GoName, receiver, "goxide: Error on nil "+enum.Enum.Name, message)
		source := "nil"
		if attrs.source || attrs.from {
			source = receiver + "." + variant.Fields[0].GoName
		}
		fmt.Fprintf(&text, "func (%s *%s) Unwrap() error { if %s == nil { panic(%q) }; return %s }\n", receiver, variant.GoName, receiver, "goxide: Unwrap on nil "+enum.Enum.Name, source)
		if attrs.from {
			argument := e.fresh("Cause").Name
			fmt.Fprintf(&text, "func %s(%s error) %s { return &%s{%s: %s} }\n", deriveExprText(e.deriveHelper(d, "Error")), argument, deriveExprText(e.goType(d.Type)), variant.GoName, variant.Fields[0].GoName, argument)
		}
	}
	return text.String(), nil
}

func (e *emitter) structError(d *check.Derive) (string, error) {
	_, shape, declarationAttrs := deriveStructShape(d.Node)
	if shape == nil || d.Type.TypeParams().Len() != 0 {
		return "", fmt.Errorf("Error requires a concrete struct or enum")
	}
	attrs, err := readErrorAttributes(declarationAttrs)
	if err != nil {
		return "", err
	}
	if !attrs.hasMessage || attrs.source || attrs.from {
		return "", fmt.Errorf("Error struct requires a message; source/from belong to a field")
	}
	typ := d.Type.Underlying().(*types.Struct)
	fields := map[string]check.Field{}
	source, from := "", false
	index := 0
	for _, field := range shape.Fields {
		if len(field.Names) == 0 {
			return "", fmt.Errorf("embedded fields have no Error derive protocol")
		}
		fieldAttrs, err := readErrorAttributes(field.Attrs)
		if err != nil {
			return "", err
		}
		if fieldAttrs.hasMessage {
			return "", fmt.Errorf("error message belongs to the struct, not a field")
		}
		for _, name := range field.Names {
			if name.Name == "_" {
				return "", fmt.Errorf("blank fields have no Error derive protocol")
			}
			member := typ.Field(index)
			index++
			fields[name.Name] = check.Field{Name: name.Name, GoName: member.Name(), Type: member.Type()}
			if !fieldAttrs.source && !fieldAttrs.from {
				continue
			}
			if source != "" {
				return "", fmt.Errorf("Error requires at most one source field")
			}
			if !types.Identical(types.Unalias(member.Type()), types.Universe.Lookup("error").Type()) {
				return "", fmt.Errorf("source/from field %s must have type error", name.Name)
			}
			source, from = member.Name(), fieldAttrs.from
		}
	}
	if from && typ.NumFields() != 1 {
		return "", fmt.Errorf("from requires exactly one error field")
	}
	receiver := e.fresh("Error").Name
	message, err := e.errorMessage(attrs.message, receiver, fields)
	if err != nil {
		return "", err
	}
	name := deriveExprText(e.goType(d.Type))
	text := fmt.Sprintf("func (%s %s) Error() string { return %s }\n", receiver, name, message)
	if source != "" {
		text += fmt.Sprintf("func (%s %s) Unwrap() error { return %s.%s }\n", receiver, name, receiver, source)
	}
	if from {
		argument := e.fresh("Cause").Name
		text += fmt.Sprintf("func %s(%s error) %s { return %s{%s: %s} }\n", deriveExprText(e.deriveHelper(d, "Error")), argument, name, name, source, argument)
	}
	return text, nil
}

func (e *emitter) errorMessage(message, receiver string, fields map[string]check.Field) (string, error) {
	var parts []string
	var literal strings.Builder
	flush := func() {
		if literal.Len() != 0 {
			parts = append(parts, strconv.Quote(literal.String()))
			literal.Reset()
		}
	}
	for i := 0; i < len(message); {
		if strings.HasPrefix(message[i:], "{{") || strings.HasPrefix(message[i:], "}}") {
			literal.WriteByte(message[i])
			i += 2
			continue
		}
		if message[i] == '}' {
			return "", fmt.Errorf("unmatched closing brace in error message")
		}
		if message[i] != '{' {
			literal.WriteByte(message[i])
			i++
			continue
		}
		flush()
		end := strings.IndexByte(message[i+1:], '}')
		if end < 0 {
			return "", fmt.Errorf("unclosed error placeholder")
		}
		end += i + 1
		name := message[i+1 : end]
		field, ok := fields[name]
		if !ok {
			return "", fmt.Errorf("unknown error placeholder %q", name)
		}
		if basic, ok := types.Unalias(field.Type).(*types.Basic); !ok || basic.Info()&types.IsComplex != 0 {
			return "", fmt.Errorf("error placeholder %s requires a built-in scalar", name)
		}
		value, err := e.printFormat(field.Type, &goast.SelectorExpr{X: goast.NewIdent(receiver), Sel: goast.NewIdent(field.GoName)}, false)
		if err != nil {
			return "", err
		}
		parts = append(parts, deriveExprText(value))
		i = end + 1
	}
	flush()
	if len(parts) == 0 {
		return strconv.Quote(""), nil
	}
	return strings.Join(parts, " + "), nil
}

func errorInterfaceMethods() []*goast.Field {
	var methods []*goast.Field
	for _, method := range []struct{ name, result string }{{"Error", "string"}, {"Unwrap", "error"}} {
		methods = append(methods, &goast.Field{Names: []*goast.Ident{goast.NewIdent(method.name)}, Type: &goast.FuncType{
			Params: &goast.FieldList{}, Results: &goast.FieldList{List: []*goast.Field{{Type: goast.NewIdent(method.result)}}},
		}})
	}
	return methods
}
