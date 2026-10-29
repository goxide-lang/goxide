package macros

import (
	"strconv"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/syn"
	"github.com/goxide-lang/syntax/token"
)

func fragment(ctx pm.Context, text string) token.TokenStream {
	stream, err := token.ParseText(text, ctx.DefSite())
	if err != nil {
		panic(err)
	}
	tokens := stream.Tokens()
	for len(tokens) > 0 && tokens[len(tokens)-1].Kind() == token.SemicolonKind {
		tokens = tokens[:len(tokens)-1]
	}
	return token.New(tokens...)
}

func expandTwice(ctx pm.Context, input token.TokenStream) pm.Output {
	if input.IsEmpty() {
		return pm.Failure("twice requires an expression", ctx.CallSpan())
	}
	group, err := token.NewGroup(token.Parenthesis, input, ctx.DefSite())
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	return pm.Success(token.New(group).Append(fragment(ctx, "* 2").Tokens()...))
}

func expandConstant(ctx pm.Context, args, item token.TokenStream) pm.Output {
	if args.IsEmpty() {
		return pm.Failure("constant requires a return expression", ctx.CallSpan())
	}
	tokens := item.Tokens()
	if len(tokens) == 0 || tokens[0].Text() != "func" {
		return pm.Failure("constant requires a function", ctx.CallSpan())
	}
	body := fragment(ctx, "return").Append(args.Tokens()...)
	group, err := token.NewGroup(token.Brace, body, ctx.DefSite())
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	for index := len(tokens) - 1; index >= 0; index-- {
		if tokens[index].Delimiter() == token.Brace {
			tokens[index] = group
			return pm.Success(token.New(tokens...))
		}
	}
	return pm.Failure("constant requires a function body", ctx.CallSpan())
}

func expandTypeName(ctx pm.Context, input token.TokenStream) pm.Output {
	item, err := syn.Parse(input)
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	if item.Alias() || !item.Generics().IsEmpty() || !item.Where().IsEmpty() {
		return pm.Failure("TypeName requires a concrete, non-alias type", item.Span())
	}
	// Preserve the caller's type token; generated keywords and the method body
	// keep their definition-site context.
	output := fragment(ctx, "impl").Append(item.Name().ToTokens().Tokens()...)
	output = output.Append(fragment(ctx, "{ pub func typeName(self Self) string { return "+strconv.Quote(item.Name().Text())+" } }").Tokens()...)
	return pm.Success(output)
}
