package macros

import (
	"fmt"
	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/syn"
	"github.com/goxide-lang/syntax/token"
	"strconv"
)

func fragment(ctx pm.Context, source string) token.TokenStream {
	stream, err := token.ParseText(source, ctx.DefSite())
	if err != nil {
		panic(err)
	}
	nodes := stream.Tokens()
	for len(nodes) != 0 && nodes[len(nodes)-1].Kind() == token.SemicolonKind {
		nodes = nodes[:len(nodes)-1]
	}
	return token.New(nodes...)
}
func group(ctx pm.Context, delimiter token.Delimiter, body token.TokenStream) token.Token {
	value, err := token.NewGroup(delimiter, body, ctx.DefSite())
	if err != nil {
		panic(err)
	}
	return value
}
func derive(ctx pm.Context, input token.TokenStream, kind string) pm.Output {
	item, err := syn.Parse(input)
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	fields, ok := item.Type().StructFields()
	if (!ok && item.Kind() != syn.EnumItem) || item.Alias() || !item.Generics().IsEmpty() || !item.Where().IsEmpty() {
		return pm.Failure("Library protocol proof requires a concrete struct or enum", item.Span())
	}
	root, err := ctx.RuntimeRoot("example.org/library-protocols")
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	protocol := root.Append(fragment(ctx, "::Library"+kind).Tokens()...)
	if item.Kind() == syn.EnumItem {
		return deriveEnum(ctx, item, root, protocol, kind)
	}
	var predicates, values, body token.TokenStream
	if kind == "Debug" {
		body = fragment(ctx, "return "+strconv.Quote(item.Name().Text()+" { "))
	}
	count := 0
	for _, field := range fields {
		if len(field.Names()) == 0 {
			return pm.Failure("named fields required", field.Span())
		}
		if !predicates.IsEmpty() {
			predicates = predicates.Append(fragment(ctx, ",").Tokens()...)
		}
		predicates = predicates.Append(field.Type().ToTokens().Tokens()...).Append(fragment(ctx, ":").Tokens()...).Append(protocol.Tokens()...)
		for _, name := range field.Names() {
			if name.Text() == "_" {
				return pm.Failure("nonblank fields required", name.Span())
			}
			var call token.TokenStream
			if kind == "Default" {
				call = root.Append(fragment(ctx, "::Make").Tokens()...).Append(group(ctx, token.Bracket, field.Type().ToTokens()), group(ctx, token.Parenthesis, token.New()))
			} else if kind == "Clone" || kind == "Debug" {
				member := "clone"
				if kind == "Debug" {
					member = "debug"
				}
				arg := fragment(ctx, "&self.").Append(name.ToTokens().Tokens()...)
				call = protocol.Append(fragment(ctx, "::"+member).Tokens()...).Append(group(ctx, token.Parenthesis, arg))
			} else if kind == "PartialEq" {
				args := fragment(ctx, "self.").Append(name.ToTokens().Tokens()...).Append(fragment(ctx, ",other.").Tokens()...).Append(name.ToTokens().Tokens()...)
				call = protocol.Append(fragment(ctx, "::equals").Tokens()...).Append(group(ctx, token.Parenthesis, args))
			}
			switch kind {
			case "Default", "Clone":
				values = values.Append(name.ToTokens().Tokens()...).Append(fragment(ctx, ":").Tokens()...).Append(call.Tokens()...).Append(fragment(ctx, ",").Tokens()...)
			case "Debug":
				label := name.Text() + ": "
				if count != 0 {
					label = ", " + label
				}
				body = body.Append(fragment(ctx, "+"+strconv.Quote(label)+"+").Tokens()...).Append(call.Tokens()...)
			case "PartialEq":
				if count == 0 {
					body = fragment(ctx, "return")
				} else {
					body = body.Append(fragment(ctx, "&&").Tokens()...)
				}
				body = body.Append(call.Tokens()...)
			}
			count++
		}
	}
	var members token.TokenStream
	switch kind {
	case "Default", "Clone":
		signature := "func r#default() Self"
		if kind == "Clone" {
			signature = "func clone(self &Self) Self"
		}
		body = fragment(ctx, "return Self").Append(group(ctx, token.Brace, values))
		members = fragment(ctx, signature).Append(group(ctx, token.Brace, body))
	case "Debug":
		body = body.Append(fragment(ctx, "+\" }\"").Tokens()...)
		members = fragment(ctx, "func debug(self &Self) string").Append(group(ctx, token.Brace, body))
	case "PartialEq":
		if count == 0 {
			body = fragment(ctx, "return true")
		}
		members = fragment(ctx, "func equals(self Self,other Self) bool").Append(group(ctx, token.Brace, body))
	case "Eq":
		// This only proves ordinary trait prerequisites. It does not replace the
		// compiler's deferred distinction between derived and manual PartialEq.
	}
	output := fragment(ctx, "impl").Append(protocol.Tokens()...).Append(fragment(ctx, "for").Tokens()...).Append(item.Name().ToTokens().Tokens()...)
	if !predicates.IsEmpty() {
		output = output.Append(fragment(ctx, "where").Tokens()...).Append(predicates.Tokens()...)
	}
	return pm.Success(output.Append(group(ctx, token.Brace, members)))
}

// Enum generation reads each actual variant and field. Only Default's selected
// variant contributes prerequisites; Clone and Debug check every variant.
func deriveEnum(ctx pm.Context, item syn.Item, root, protocol token.TokenStream, kind string) pm.Output {
	if kind != "Default" && kind != "Clone" && kind != "Debug" {
		return pm.Failure("enum equality is outside this library protocol proof", item.Span())
	}
	variants := item.Variants()
	if kind == "Default" {
		var selected []syn.Variant
		for _, variant := range variants {
			for _, attr := range variant.Attributes() {
				path := attr.Path().Tokens()
				if len(path) != 1 || path[0].Text() != "library_default" {
					continue
				}
				arguments := attr.Arguments().Tokens()
				if len(arguments) != 0 && !(len(arguments) == 1 && arguments[0].Kind() == token.GroupKind && arguments[0].Delimiter() == token.Parenthesis && arguments[0].Children().IsEmpty()) {
					return pm.Failure("library_default takes no arguments", attr.Span())
				}
				selected = append(selected, variant)
			}
		}
		if len(selected) != 1 {
			return pm.Failure("LibraryDefault requires exactly one library_default variant", item.Span())
		}
		variants = selected
	}
	var predicates, arms, body token.TokenStream
	for _, variant := range variants {
		var fieldTypes []token.TokenStream
		var fieldNames []syn.Ident
		for _, field := range variant.Fields() {
			if !predicates.IsEmpty() {
				predicates = predicates.Append(fragment(ctx, ",").Tokens()...)
			}
			predicates = predicates.Append(field.Type().ToTokens().Tokens()...).Append(fragment(ctx, ":").Tokens()...).Append(protocol.Tokens()...)
			if variant.Kind() == syn.TupleVariant {
				fieldTypes = append(fieldTypes, field.Type().ToTokens())
				fieldNames = append(fieldNames, syn.Ident{})
			} else {
				if len(field.Names()) == 0 {
					return pm.Failure("named enum fields required", field.Span())
				}
				for _, name := range field.Names() {
					if name.Text() == "_" {
						return pm.Failure("nonblank enum fields required", name.Span())
					}
					fieldTypes = append(fieldTypes, field.Type().ToTokens())
					fieldNames = append(fieldNames, name)
				}
			}
		}
		path := item.Name().ToTokens().Append(fragment(ctx, "::").Tokens()...).Append(variant.Name().ToTokens().Tokens()...)
		var bindings, values token.TokenStream
		expression := fragment(ctx, strconv.Quote(item.Name().Text()+"::"+variant.Name().Text()))
		if kind == "Debug" && len(fieldTypes) > 0 {
			opening := "("
			if variant.Kind() == syn.StructVariant {
				opening = " { "
			}
			expression = expression.Append(fragment(ctx, "+"+strconv.Quote(opening)).Tokens()...)
		}
		for i, fieldType := range fieldTypes {
			if i != 0 {
				bindings = bindings.Append(fragment(ctx, ",").Tokens()...)
				values = values.Append(fragment(ctx, ",").Tokens()...)
				if kind == "Debug" {
					expression = expression.Append(fragment(ctx, "+"+strconv.Quote(", ")).Tokens()...)
				}
			}
			if variant.Kind() == syn.StructVariant {
				key := fieldNames[i].ToTokens().Append(fragment(ctx, ":").Tokens()...)
				bindings = bindings.Append(key.Tokens()...)
				values = values.Append(key.Tokens()...)
				if kind == "Debug" {
					expression = expression.Append(fragment(ctx, "+"+strconv.Quote(fieldNames[i].Text()+": ")).Tokens()...)
				}
			}
			binding := fragment(ctx, fmt.Sprintf("variantField%d", i))
			bindings = bindings.Append(binding.Tokens()...)
			var call token.TokenStream
			if kind == "Default" {
				call = root.Append(fragment(ctx, "::Make").Tokens()...).Append(group(ctx, token.Bracket, fieldType), group(ctx, token.Parenthesis, token.New()))
			} else {
				member := "clone"
				if kind == "Debug" {
					member = "debug"
				}
				call = protocol.Append(fragment(ctx, "::"+member).Tokens()...).Append(group(ctx, token.Parenthesis, binding))
			}
			values = values.Append(call.Tokens()...)
			if kind == "Debug" {
				expression = expression.Append(fragment(ctx, "+").Tokens()...).Append(call.Tokens()...)
			}
		}
		constructor, pattern := path, path
		switch variant.Kind() {
		case syn.TupleVariant:
			constructor = constructor.Append(group(ctx, token.Parenthesis, values))
			pattern = pattern.Append(group(ctx, token.Parenthesis, bindings))
		case syn.StructVariant:
			constructor = constructor.Append(group(ctx, token.Brace, values))
			pattern = pattern.Append(group(ctx, token.Brace, bindings))
		}
		if kind == "Default" {
			body = fragment(ctx, "return").Append(constructor.Tokens()...)
			continue
		}
		if kind == "Debug" && len(fieldTypes) > 0 {
			closing := ")"
			if variant.Kind() == syn.StructVariant {
				closing = " }"
			}
			expression = expression.Append(fragment(ctx, "+"+strconv.Quote(closing)).Tokens()...)
		}
		if kind == "Clone" {
			expression = constructor
		}
		arms = arms.Append(pattern.Tokens()...).Append(fragment(ctx, "=>").Tokens()...).Append(expression.Tokens()...).Append(fragment(ctx, ",").Tokens()...)
	}
	signature := "func r#default() Self"
	if kind != "Default" {
		signature = "func clone(self &Self) Self"
		if kind == "Debug" {
			signature = "func debug(self &Self) string"
		}
		body = fragment(ctx, "return match &*self").Append(group(ctx, token.Brace, arms))
	}
	members := fragment(ctx, signature).Append(group(ctx, token.Brace, body))
	output := fragment(ctx, "impl").Append(protocol.Tokens()...).Append(fragment(ctx, "for").Tokens()...).Append(item.Name().ToTokens().Tokens()...)
	if !predicates.IsEmpty() {
		output = output.Append(fragment(ctx, "where").Tokens()...).Append(predicates.Tokens()...)
	}
	return pm.Success(output.Append(group(ctx, token.Brace, members)))
}
