package macros

import (
	"fmt"
	"strconv"
	"strings"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/syn"
	"github.com/goxide-lang/syntax/token"
)

func serdeFragment(ctx pm.Context, source string) token.TokenStream {
	stream, err := token.ParseText(source, ctx.DefSite())
	if err != nil {
		panic(err)
	}
	nodes := stream.Tokens()
	for len(nodes) > 0 && nodes[len(nodes)-1].Kind() == token.SemicolonKind {
		nodes = nodes[:len(nodes)-1]
	}
	return token.New(nodes...)
}
func serdeGroup(ctx pm.Context, delimiter token.Delimiter, stream token.TokenStream) token.Token {
	result, err := token.NewGroup(delimiter, stream, ctx.DefSite())
	if err != nil {
		panic(err)
	}
	return result
}
func serdeSplice(ctx pm.Context, stream token.TokenStream, bindings map[string]token.TokenStream) token.TokenStream {
	result := token.New()
	for _, node := range stream.Tokens() {
		if value, ok := bindings[node.Text()]; ok && node.Kind() == token.IdentKind {
			result = result.Append(value.Tokens()...)
			continue
		}
		if node.Kind() == token.GroupKind {
			node = serdeGroup(ctx, node.Delimiter(), serdeSplice(ctx, node.Children(), bindings))
		}
		result = result.Append(node)
	}
	return result
}
func serdeWire(field syn.Field, name syn.Ident) (string, error) {
	wire := name.Text()
	for _, attribute := range field.Attributes() {
		path := attribute.Path().Tokens()
		if len(path) != 1 || path[0].Text() != "serde" {
			return "", fmt.Errorf("unsupported attribute")
		}
		arguments := attribute.Arguments().Tokens()
		if len(arguments) != 1 || arguments[0].Delimiter() != token.Parenthesis {
			return "", fmt.Errorf("invalid serde attribute")
		}
		parts := arguments[0].Children().Tokens()
		if len(parts) != 3 || parts[0].Text() != "rename" || parts[1].Text() != "=" {
			return "", fmt.Errorf("only rename is used by this fixture")
		}
		var err error
		wire, err = strconv.Unquote(parts[2].Text())
		if err != nil {
			return "", err
		}
	}
	return wire, nil
}
func deriveSerde(ctx pm.Context, input token.TokenStream, write bool) pm.Output {
	item, err := syn.Parse(input)
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	if item.Alias() || !item.Where().IsEmpty() {
		return pm.Failure("fixture derives require a struct or enum declaration", item.Span())
	}
	root, err := ctx.RuntimeRoot("example.org/serde-codecs")
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	bindings := map[string]token.TokenStream{"__runtime": root, "__generics": item.Generics(), "__target": item.Name().ToTokens()}
	if !item.Generics().IsEmpty() {
		group := item.Generics().Tokens()[0]
		parameters := group.Children().Tokens()
		if len(parameters) != 2 || parameters[0].Kind() != token.IdentKind || parameters[1].Text() != "any" {
			return pm.Failure("fixture supports one unconstrained generic parameter", item.Span())
		}
		bindings["__target"] = bindings["__target"].Append(serdeGroup(ctx, token.Bracket, token.New(parameters[0])))
	}
	protocol := "Deserialize"
	if write {
		protocol = "Serialize"
	}
	var body strings.Builder
	bounds := []string{}
	fields, record := item.Type().StructFields()
	if record {
		names, wires, types := []string{}, []string{}, []string{}
		seen := map[string]bool{}
		for _, field := range fields {
			if len(field.Names()) == 0 {
				return pm.Failure("embedded fields are not supported", field.Span())
			}
			for _, name := range field.Names() {
				if name.Text() == "_" {
					return pm.Failure("blank fields are not supported", field.Span())
				}
				wire, err := serdeWire(field, name)
				if err != nil {
					return pm.Failure(err.Error(), field.Span())
				}
				if seen[wire] {
					return pm.Failure("duplicate wire name", field.Span())
				}
				seen[wire] = true
				n := len(names)
				fieldName, fieldType := fmt.Sprintf("__field%d", n), fmt.Sprintf("__type%d", n)
				names = append(names, fieldName)
				types = append(types, fieldType)
				wires = append(wires, strconv.Quote(wire))
				bindings[fieldName], bindings[fieldType] = name.ToTokens(), field.Type().ToTokens()
				bounds = append(bounds, fieldType+":__runtime::"+protocol)
			}
		}
		if write {
			fmt.Fprintf(&body, "func serialize(self &Self,serializer __runtime::Serializer)error { writer,err:=serializer.Struct(%q,%d);if err!=nil{return err};", item.Name().Text(), len(names))
			for i, name := range names {
				fmt.Fprintf(&body, "if err:=writer.Field(%s,func(child __runtime::Serializer)error{return __runtime::Write(&self.%s,child)});err!=nil{return __runtime::AtField(err,%s)};", wires[i], name, wires[i])
			}
			body.WriteString("return writer.End() }")
		} else {
			body.WriteString("func deserialize(deserializer __runtime::Deserializer)(Self,error){var result Self;")
			for i := range names {
				fmt.Fprintf(&body, "seen%d:=false;", i)
			}
			fmt.Fprintf(&body, "err:=deserializer.Struct(%q,[]string{%s},__runtime::VisitorFuncs{OnMap:func(fields __runtime::MapAccess)error{for{var name string;more,err:=fields.NextKey(func(child __runtime::Deserializer)error{value,err:=__runtime::ReadString(child);name=value;return err});if err!=nil{return err};if !more{break};switch name{", item.Name().Text(), strings.Join(wires, ","))
			for i, name := range names {
				fmt.Fprintf(&body, "case %s:if seen%d{return __runtime::NewError(__runtime::DuplicateField,name)};seen%d=true;if err:=fields.NextValue(func(child __runtime::Deserializer)error{value,err:=__runtime::Read[%s](child);result.%s=value;return __runtime::AtField(err,%s)});err!=nil{return err};", wires[i], i, i, types[i], name, wires[i])
			}
			body.WriteString("default:if err:=fields.NextValue(func(child __runtime::Deserializer)error{return child.Skip()});err!=nil{return err}}};return nil}});if err!=nil{return result,err};")
			for i, name := range names {
				fmt.Fprintf(&body, "if !seen%d{value,optional:=__runtime::Missing[%s]();if !optional{return result,__runtime::NewError(__runtime::MissingField,%s)};result.%s=value};", i, types[i], wires[i], name)
			}
			body.WriteString("return result,nil};func missing()(Self,bool){var zero Self;return zero,false}")
		}
	} else if item.Kind() == syn.EnumItem {
		variants := item.Variants()
		wires := []string{}
		for i, variant := range variants {
			name := fmt.Sprintf("__variant%d", i)
			bindings[name] = variant.Name().ToTokens()
			wires = append(wires, strconv.Quote(variant.Name().Text()))
			if variant.Kind() != syn.UnitVariant && (variant.Kind() != syn.TupleVariant || len(variant.Fields()) != 1) {
				return pm.Failure("fixture enums require unit or newtype variants", variant.Span())
			}
			if len(variant.Fields()) == 1 {
				typ := fmt.Sprintf("__payload%d", i)
				bindings[typ] = variant.Fields()[0].Type().ToTokens()
				bounds = append(bounds, typ+":__runtime::"+protocol)
			}
		}
		if write {
			body.WriteString("func serialize(self &Self,serializer __runtime::Serializer)error{if !enum_valid(*self){return __runtime::NewError(__runtime::Type,\"invalid enum\")};return match &*self{")
			for i, variant := range variants {
				if variant.Kind() == syn.UnitVariant {
					fmt.Fprintf(&body, "__target::__variant%d=>serializer.UnitVariant(%q,%s),", i, item.Name().Text(), wires[i])
				} else {
					fmt.Fprintf(&body, "__target::__variant%d(value)=>serializer.NewtypeVariant(%q,%s,func(child __runtime::Serializer)error{return __runtime::Write(value,child)}),", i, item.Name().Text(), wires[i])
				}
			}
			body.WriteString("}}")
		} else {
			fmt.Fprintf(&body, "func deserialize(deserializer __runtime::Deserializer)(Self,error){var result Self;err:=deserializer.Enum(%q,[]string{%s},__runtime::VisitorFuncs{OnEnum:func(access __runtime::EnumAccess)error{name,variant,err:=access.Variant();if err!=nil{return err};switch name{", item.Name().Text(), strings.Join(wires, ","))
			for i, variant := range variants {
				if variant.Kind() == syn.UnitVariant {
					fmt.Fprintf(&body, "case %s:if err:=variant.Unit();err!=nil{return err};result=__target::__variant%d;return nil;", wires[i], i)
				} else {
					fmt.Fprintf(&body, "case %s:return variant.Newtype(func(child __runtime::Deserializer)error{value,err:=__runtime::Read[__payload%d](child);if err!=nil{return __runtime::AtVariant(err,%s)};result=__target::__variant%d(value);return nil});", wires[i], i, wires[i], i)
				}
			}
			body.WriteString("default:return __runtime::NewError(__runtime::UnknownVariant,name)}}});return result,err};func missing()(Self,bool){var zero Self;return zero,false}")
		}
	} else {
		return pm.Failure("fixture derives require structs and enums", item.Span())
	}
	where := ""
	if len(bounds) > 0 {
		where = "where " + strings.Join(bounds, ",")
	}
	source := "impl __generics __runtime::" + protocol + " for __target " + where + " {" + body.String() + "};"
	if item.Generics().IsEmpty() {
		if write {
			source += "pub func Serialize" + item.Name().Text() + "(serializer __runtime::Serializer,value *__target)error{if value==nil{return __runtime::NewError(__runtime::Type,\"nil value\")};return __runtime::Write(&*value,serializer)}"
		} else {
			source += "pub func Deserialize" + item.Name().Text() + "(deserializer __runtime::Deserializer)(__target,error){return __runtime::Read[__target](deserializer)}"
		}
	}
	return pm.Success(serdeSplice(ctx, serdeFragment(ctx, source), bindings))
}
