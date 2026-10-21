package macros

import (
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
	if !ok || item.Alias() || !item.Generics().IsEmpty() || !item.Where().IsEmpty() {
		return pm.Failure("Library protocol proof requires a concrete struct", item.Span())
	}
	root, err := ctx.RuntimeRoot("example.org/library-protocols")
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	protocol := root.Append(fragment(ctx, "::Library"+kind).Tokens()...)
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
