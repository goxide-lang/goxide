package macros

import (
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
	for len(tokens) != 0 && tokens[len(tokens)-1].Kind() == token.SemicolonKind {
		tokens = tokens[:len(tokens)-1]
	}
	return token.New(tokens...)
}

func generate(ctx pm.Context, input token.TokenStream, keepItem, overlapping bool) pm.Output {
	item, err := syn.Parse(input)
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	parameters := item.Generics().Tokens()
	if item.Alias() || len(parameters) != 1 || parameters[0].Delimiter() != token.Bracket || len(parameters[0].Children().Tokens()) != 2 {
		return pm.Failure("Classify requires one unconstrained type parameter", item.Span())
	}
	root, err := ctx.RuntimeRoot("example.org/coherence-protocol")
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	protocol := root.Append(fragment(ctx, "::Source").Tokens()...)
	batch, err := token.ParseText("Batch", ctx.CallSite())
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	batch = token.New(batch.Tokens()[0])
	values := "return self.Values"
	if item.Kind() == syn.EnumItem {
		values = "return match self { Self::Data(values) => values, Self::Empty => nil }"
	}
	var output token.TokenStream
	if keepItem {
		output = item.ToTokens().Append(token.NewSemicolon(ctx.DefSite()))
	}
	for _, branch := range []struct{ constraint, label, size string }{
		{"~int", "number", "N"},
		{"~string", "text", "N+100"},
	} {
		if overlapping {
			branch.constraint = "~int"
		}
		impl := fragment(ctx, "impl[T "+branch.constraint+"]").Append(protocol.Tokens()...)
		impl = impl.Append(fragment(ctx, "for").Tokens()...).Append(item.Name().ToTokens().Tokens()...)
		impl = impl.Append(fragment(ctx, `[T] {
type Item = T
func values(self Self) []Self::Item { `+values+` }
func label(self Self) string { return "`+branch.label+`" }
}`).Tokens()...)
		output = output.Append(impl.Tokens()...).Append(token.NewSemicolon(ctx.DefSite()))
		impl = fragment(ctx, "impl[T "+branch.constraint+", const N int]").Append(batch.Tokens()...)
		impl = impl.Append(fragment(ctx, "for [N]").Tokens()...).Append(item.Name().ToTokens().Tokens()...)
		impl = impl.Append(fragment(ctx, "[T]").Tokens()...)
		body := fragment(ctx, "type Item =").Append(item.Name().ToTokens().Tokens()...)
		body = body.Append(fragment(ctx, "[T]; func first(self Self) Self::Item { return self[0] }; func size(self Self) int { return "+branch.size+" }").Tokens()...)
		group, err := token.NewGroup(token.Brace, body, ctx.DefSite())
		if err != nil {
			return pm.Failure(err.Error(), ctx.CallSpan())
		}
		impl = impl.Append(group)
		output = output.Append(impl.Tokens()...).Append(token.NewSemicolon(ctx.DefSite()))
	}
	return pm.Success(output)
}
