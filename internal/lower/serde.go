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

type serdeAttributes struct {
	rename                   string
	renamed, skip, def, deny bool
	number                   *int64
}

func readSerdeAttributes(attrs []*syntax.Attr, allowed string) (serdeAttributes, error) {
	var out serdeAttributes
	seen := map[string]bool{}
	for _, attr := range attrs {
		if resolve.AttributeName(attr) != "serde" {
			continue
		}
		if attr.Args == nil || len(attr.Args.Tokens) == 0 {
			return out, fmt.Errorf("serde requires an attribute argument")
		}
		tokens := attr.Args.Tokens
		for i := 0; i < len(tokens); {
			if tokens[i].Token == nil {
				return out, fmt.Errorf("invalid serde attribute")
			}
			name := tokens[i].Token.Text
			i++
			if !strings.Contains(" "+allowed+" ", " "+name+" ") {
				return out, fmt.Errorf("serde %s is not supported at this position", name)
			}
			if seen[name] {
				return out, fmt.Errorf("duplicate serde %s attribute", name)
			}
			seen[name] = true
			switch name {
			case "skip":
				out.skip = true
			case "default":
				out.def = true
			case "deny_unknown_fields":
				out.deny = true
			case "rename", "value":
				if i+1 >= len(tokens) || tokens[i].Token == nil || tokens[i].Token.Kind != token.ASSIGN || tokens[i+1].Token == nil {
					return out, fmt.Errorf("serde %s requires a literal value", name)
				}
				i++
				value := tokens[i].Token
				i++
				if name == "rename" {
					if value.Kind != token.STRING {
						return out, fmt.Errorf("serde rename requires a string literal")
					}
					var err error
					out.rename, err = strconv.Unquote(value.Text)
					if err != nil {
						return out, err
					}
					out.renamed = true
				} else {
					literal := value.Text
					if value.Kind == token.SUB && i < len(tokens) && tokens[i].Token != nil {
						literal += tokens[i].Token.Text
						i++
					}
					number, err := strconv.ParseInt(literal, 10, 64)
					if err != nil {
						return out, fmt.Errorf("serde value requires a signed int64 decimal literal")
					}
					out.number = &number
				}
			}
			if i < len(tokens) {
				if tokens[i].Token == nil || tokens[i].Token.Kind != token.COMMA {
					return out, fmt.Errorf("serde attribute arguments must be comma-separated")
				}
				i++
			}
		}
	}
	return out, nil
}

func (e *emitter) serdePackage() string {
	return e.importName([]string{"github.com", "goxide-lang", "serde"}, "serde", 0).Name
}

func (e *emitter) serdeDeclarations() []goast.Decl {
	var plans []*check.Derive
	for _, d := range e.info.Derives {
		if d.File == e.name && (d.Requests["Serialize"] || d.Requests["Deserialize"]) {
			plans = append(plans, d)
		}
	}
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].Node.Span().Start != plans[j].Node.Span().Start {
			return plans[i].Node.Span().Start < plans[j].Node.Span().Start
		}
		return plans[i].Type.Obj().Name() < plans[j].Type.Obj().Name()
	})
	var out []goast.Decl
	for _, d := range plans {
		var text string
		var err error
		if enum := e.info.EnumOf(d.Type); enum != nil {
			text, err = e.serdeEnum(d, enum)
		} else {
			text, err = e.serdeStruct(d)
		}
		if err != nil {
			e.fail(d.Node, err.Error())
			return nil
		}
		out = append(out, e.deriveParse(d.Node, text)...)
	}
	return out
}

type serdeField struct {
	name, wire string
	typ        types.Type
	attrs      serdeAttributes
}

func deriveStructShape(node syntax.Node) (*syntax.Ident, *syntax.StructType, []*syntax.Attr) {
	var name *syntax.Ident
	var typ syntax.Type
	var attrs []*syntax.Attr
	switch n := node.(type) {
	case *syntax.TypeDecl:
		name, typ, attrs = n.Name, n.Type, n.Attrs
	case *syntax.TypeSpec:
		name, typ, attrs = n.Name, n.Type, n.Attrs
	}
	shape, _ := typ.(*syntax.StructType)
	return name, shape, attrs
}

func serdeFields(d *check.Derive) ([]serdeField, serdeAttributes, error) {
	var attrs serdeAttributes
	_, shape, declarationAttrs := deriveStructShape(d.Node)
	if shape == nil || d.Type.TypeParams().Len() != 0 {
		return nil, attrs, fmt.Errorf("serde derives require a concrete struct or enum")
	}
	attrs, err := readSerdeAttributes(declarationAttrs, "deny_unknown_fields")
	if err != nil {
		return nil, attrs, err
	}
	typ := d.Type.Underlying().(*types.Struct)
	var fields []serdeField
	seen := map[string]bool{}
	for _, field := range shape.Fields {
		if len(field.Names) == 0 {
			return nil, attrs, fmt.Errorf("embedded fields have no serde derive protocol")
		}
		options, err := readSerdeAttributes(field.Attrs, "rename default skip")
		if err != nil {
			return nil, attrs, err
		}
		for _, name := range field.Names {
			if name.Name == "_" {
				return nil, attrs, fmt.Errorf("blank fields have no serde derive protocol")
			}
			wire := name.Name
			if options.renamed {
				wire = options.rename
			}
			if seen[wire] {
				return nil, attrs, fmt.Errorf("duplicate serde field name %q", wire)
			}
			seen[wire] = true
			member := typ.Field(len(fields))
			fields = append(fields, serdeField{name: member.Name(), wire: wire, typ: member.Type(), attrs: options})
		}
	}
	return fields, attrs, nil
}

func (e *emitter) serdeStruct(d *check.Derive) (string, error) {
	fields, attrs, err := serdeFields(d)
	if err != nil {
		return "", err
	}
	a, typ := e.serdePackage(), deriveExprText(e.goType(d.Type))
	declarationName, _, _ := deriveStructShape(d.Node)
	name := declarationName.Name
	var out strings.Builder
	if d.Requests["Serialize"] {
		s, value, writer, failure := e.fresh("Serializer").Name, e.fresh("Value").Name, e.fresh("Writer").Name, e.fresh("Error").Name
		count := 0
		for _, field := range fields {
			if !field.attrs.skip {
				count++
			}
		}
		fmt.Fprintf(&out, "func %s(%s %s.Serializer, %s *%s) error { if %s == nil { return %s.NewError(%s.Type,\"nil struct pointer\") }; %s,%s:=%s.Struct(%q,%d); if %s!=nil{return %s};", deriveExprText(e.deriveHelper(d, "Serialize")), s, a, value, typ, value, a, a, writer, failure, s, name, count, failure, failure)
		for _, field := range fields {
			if field.attrs.skip {
				continue
			}
			callback := e.fresh("Serializer").Name
			body, err := e.serdeWrite(field.typ, value+"."+field.name, callback)
			if err != nil {
				return "", fmt.Errorf("field %s: %w", field.name, err)
			}
			fmt.Fprintf(&out, "if %s:=%s.Field(%q,func(%s %s.Serializer) error{%s});%s!=nil{return %s};", failure, writer, field.wire, callback, a, body, failure, failure)
		}
		fmt.Fprintf(&out, "return %s.End()}\n", writer)
	}
	if !d.Requests["Deserialize"] {
		return out.String(), nil
	}
	deserializer, value, failure, access := e.fresh("Deserializer").Name, e.fresh("Value").Name, e.fresh("Error").Name, e.fresh("Access").Name
	key, more := e.fresh("Key").Name, e.fresh("More").Name
	seen := make([]string, len(fields))
	var names []string
	fmt.Fprintf(&out, "func %s(%s %s.Deserializer)(%s %s,%s error){", deriveExprText(e.deriveHelper(d, "Deserialize")), deserializer, a, value, typ, failure)
	for i, field := range fields {
		if !field.attrs.skip {
			seen[i] = e.fresh("Seen").Name
			fmt.Fprintf(&out, "var %s bool;", seen[i])
			names = append(names, strconv.Quote(field.wire))
		}
	}
	fmt.Fprintf(&out, "%s=%s.Struct(%q,[]string{%s},%s.VisitorFuncs{OnMap:func(%s %s.MapAccess)error{for{var %s string; %s,%s:=%s.NextKey(func(%s %s.Deserializer)error{var %s error; %s,%s=%s.ReadString(%s);return %s});if %s!=nil{return %s};if !%s{break};switch %s{", failure, deserializer, name, strings.Join(names, ","), a, access, a, key, more, failure, access, deserializer, a, failure, key, failure, a, deserializer, failure, failure, failure, more, key)
	for i, field := range fields {
		fmt.Fprintf(&out, "case %q:", field.wire)
		if field.attrs.skip {
			fmt.Fprintf(&out, "if %s:=%s.NextValue(func(%s %s.Deserializer)error{return %s.Skip()});%s!=nil{return %s};", failure, access, deserializer, a, deserializer, failure, failure)
			continue
		}
		read, err := e.serdeRead(field.typ, deserializer)
		if err != nil {
			return "", fmt.Errorf("field %s: %w", field.name, err)
		}
		fmt.Fprintf(&out, "if %s{return %s.AtField(%s.NewError(%s.DuplicateField,\"duplicate field\"),%s)};%s=(0==0);if %s:=%s.NextValue(func(%s %s.Deserializer)error{var %s error;%s.%s,%s=%s;return %s});%s!=nil{return %s};", seen[i], a, a, a, key, seen[i], failure, access, deserializer, a, failure, value, field.name, failure, read, failure, failure, failure)
	}
	if attrs.deny {
		fmt.Fprintf(&out, "default:return %s.AtField(%s.NewError(%s.UnknownField,\"unknown field\"),%s);", a, a, a, key)
	} else {
		fmt.Fprintf(&out, "default:if %s:=%s.NextValue(func(%s %s.Deserializer)error{return %s.Skip()});%s!=nil{return %s};", failure, access, deserializer, a, deserializer, failure, failure)
	}
	out.WriteString("}};")
	for i, field := range fields {
		option := e.info.EnumOf(field.typ)
		optional := option != nil && option.Enum.Builtin && option.Enum.Name == "Option"
		if !field.attrs.skip && !field.attrs.def && !optional {
			fmt.Fprintf(&out, "if !%s{return %s.AtField(%s.NewError(%s.MissingField,\"required field missing\"),%q)};", seen[i], a, a, a, field.wire)
		}
	}
	for i, field := range fields {
		if !field.attrs.skip && !field.attrs.def {
			continue
		}
		if err := e.deriveEligible("Default", field.typ, map[types.Type]bool{}, false); err != nil {
			return "", fmt.Errorf("serde default/skip field %s: %w", field.name, err)
		}
		defaultValue, err := e.deriveValue("Default", field.typ, "")
		if err != nil {
			return "", err
		}
		if field.attrs.skip {
			fmt.Fprintf(&out, "%s.%s=%s;", value, field.name, defaultValue)
		} else {
			fmt.Fprintf(&out, "if !%s{%s.%s=%s};", seen[i], value, field.name, defaultValue)
		}
	}
	fmt.Fprintf(&out, "return nil}});return %s,%s}\n", value, failure)
	return out.String(), nil
}

func (e *emitter) serdeEnum(d *check.Derive, enum *check.Instance) (string, error) {
	declaration := enum.Enum.Decl
	if _, err := readSerdeAttributes(declaration.Attrs, ""); err != nil {
		return "", err
	}
	options := make([]serdeAttributes, len(enum.Enum.Variants))
	wires := make([]string, len(options))
	numeric, mapped := false, false
	for i, variant := range enum.Enum.Variants {
		attrs, err := readSerdeAttributes(variant.Decl.Attrs, "rename value")
		if err != nil {
			return "", err
		}
		if len(variant.Fields) > 1 {
			return "", fmt.Errorf("serde enum variant %s requires a unit or single payload", variant.Name)
		}
		options[i] = attrs
		numeric, mapped = numeric || attrs.number != nil, mapped || attrs.number != nil || attrs.renamed
		wires[i] = variant.Name
		if attrs.renamed {
			wires[i] = attrs.rename
		}
	}
	seen := map[string]bool{}
	for i, variant := range enum.Enum.Variants {
		attrs := options[i]
		if mapped && len(variant.Fields) > 0 {
			return "", fmt.Errorf("explicit enum serde mapping requires unit variants")
		}
		if numeric {
			if attrs.number == nil || attrs.renamed {
				return "", fmt.Errorf("numeric enum serde mapping requires value on every variant and cannot mix rename")
			}
			wires[i] = strconv.FormatInt(*attrs.number, 10)
		}
		if seen[wires[i]] {
			return "", fmt.Errorf("duplicate enum serde mapping %q", wires[i])
		}
		seen[wires[i]] = true
	}
	a, typ := e.serdePackage(), deriveExprText(e.goType(d.Type))
	var out strings.Builder
	if d.Requests["Serialize"] {
		s, value, active := e.fresh("Serializer").Name, e.fresh("Value").Name, e.fresh("Variant").Name
		binding := active + ":="
		if len(enum.Enum.Variants) == 0 {
			binding = ""
		}
		fmt.Fprintf(&out, "func %s(%s %s.Serializer,%s *%s)error{if %s==nil{return %s.NewError(%s.Type,\"nil enum pointer\")};switch %s(*%s).(type){", deriveExprText(e.deriveHelper(d, "Serialize")), s, a, value, typ, value, a, a, binding, value)
		for i, variant := range enum.Enum.Variants {
			fmt.Fprintf(&out, "case *%s:if %s==nil{return %s.NewError(%s.Type,\"nil enum variant\")};", variant.GoName, active, a, a)
			if numeric {
				fmt.Fprintf(&out, "return %s.Int64(%s);", s, wires[i])
			} else if len(variant.Fields) == 0 {
				fmt.Fprintf(&out, "return %s.UnitVariant(%q,%q);", s, enum.Enum.Name, wires[i])
			} else {
				callback := e.fresh("Serializer").Name
				body, err := e.serdeWrite(variant.Fields[0].Type, active+"."+variant.Fields[0].GoName, callback)
				if err != nil {
					return "", fmt.Errorf("variant %s: %w", variant.Name, err)
				}
				fmt.Fprintf(&out, "return %s.NewtypeVariant(%q,%q,func(%s %s.Serializer)error{%s});", s, enum.Enum.Name, wires[i], callback, a, body)
			}
		}
		fmt.Fprintf(&out, "default:return %s.NewError(%s.Type,%q)}}\n", a, a, "invalid enum "+enum.Enum.Name)
	}
	if !d.Requests["Deserialize"] {
		return out.String(), nil
	}
	input, value, failure := e.fresh("Deserializer").Name, e.fresh("Value").Name, e.fresh("Error").Name
	fmt.Fprintf(&out, "func %s(%s %s.Deserializer)(%s %s,%s error){", deriveExprText(e.deriveHelper(d, "Deserialize")), input, a, value, typ, failure)
	if numeric {
		number := e.fresh("Number").Name
		fmt.Fprintf(&out, "%s,%s:=%s.ReadInt64(%s);if %s!=nil{return %s,%s};switch %s{", number, failure, a, input, failure, value, failure, number)
		for i, variant := range enum.Enum.Variants {
			fmt.Fprintf(&out, "case %s:return &%s{},nil;", wires[i], variant.GoName)
		}
		fmt.Fprintf(&out, "default:return %s,%s.NewError(%s.UnknownVariant,\"unknown numeric variant\")}}\n", value, a, a)
		return out.String(), nil
	}
	var wireLiterals []string
	for _, wire := range wires {
		wireLiterals = append(wireLiterals, strconv.Quote(wire))
	}
	access, name, payload := e.fresh("Access").Name, e.fresh("Name").Name, e.fresh("Payload").Name
	if len(enum.Enum.Variants) == 0 {
		payload = "_"
	}
	fmt.Fprintf(&out, "%s=%s.Enum(%q,[]string{%s},%s.VisitorFuncs{OnEnum:func(%s %s.EnumAccess)error{%s,%s,%s:=%s.Variant();if %s!=nil{return %s};switch %s{", failure, input, enum.Enum.Name, strings.Join(wireLiterals, ","), a, access, a, name, payload, failure, access, failure, failure, name)
	for i, variant := range enum.Enum.Variants {
		fmt.Fprintf(&out, "case %q:", wires[i])
		if len(variant.Fields) == 0 {
			fmt.Fprintf(&out, "if %s:=%s.Unit();%s!=nil{return %s};%s=&%s{};return nil;", failure, payload, failure, failure, value, variant.GoName)
			continue
		}
		callback, decoded := e.fresh("Deserializer").Name, e.fresh("Payload").Name
		read, err := e.serdeRead(variant.Fields[0].Type, callback)
		if err != nil {
			return "", fmt.Errorf("variant %s: %w", variant.Name, err)
		}
		fmt.Fprintf(&out, "return %s.Newtype(func(%s %s.Deserializer)error{%s,%s:=%s;if %s!=nil{return %s};%s=&%s{%s:%s};return nil});", payload, callback, a, decoded, failure, read, failure, failure, value, variant.GoName, variant.Fields[0].GoName, decoded)
	}
	fmt.Fprintf(&out, "default:return %s.NewError(%s.UnknownVariant,\"unknown variant \"+%s)}}});return %s,%s}\n", a, a, name, value, failure)
	return out.String(), nil
}
