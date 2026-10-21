package macros

import (
	"fmt"
	"strconv"
	"strings"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/syn"
	"github.com/goxide-lang/syntax/token"
)

func fragment(ctx pm.Context, source string) token.TokenStream {
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
func group(ctx pm.Context, kind token.Delimiter, stream token.TokenStream) token.Token {
	node, err := token.NewGroup(kind, stream, ctx.DefSite())
	if err != nil {
		panic(err)
	}
	return node
}

type part struct {
	literal     string
	slot, debug bool
}

func parts(source string) ([]part, error) {
	var result []part
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			result = append(result, part{literal: literal.String()})
			literal.Reset()
		}
	}
	for i := 0; i < len(source); {
		switch {
		case strings.HasPrefix(source[i:], "{{"), strings.HasPrefix(source[i:], "}}"):
			literal.WriteByte(source[i])
			i += 2
		case strings.HasPrefix(source[i:], "{}"):
			flush()
			result = append(result, part{slot: true})
			i += 2
		case strings.HasPrefix(source[i:], "{:?}"):
			flush()
			result = append(result, part{slot: true, debug: true})
			i += 4
		case source[i] == '{' || source[i] == '}':
			return nil, fmt.Errorf("unsupported or unmatched format brace")
		default:
			literal.WriteByte(source[i])
			i++
		}
	}
	flush()
	return result, nil
}
func printTokens(ctx pm.Context, input token.TokenStream, newline bool) pm.Output {
	fail := func(message string) pm.Output { return pm.Failure(message, ctx.CallSpan()) }
	var arguments []token.TokenStream
	var current []token.Token
	for _, node := range input.Tokens() {
		if node.Kind() == token.SemicolonKind {
			return fail("format arguments cannot contain top-level semicolons")
		}
		if node.Kind() == token.PunctKind && node.Text() == "..." {
			return fail("variadic print arguments are unsupported")
		}
		if node.Kind() == token.PunctKind && node.Text() == "," {
			if len(current) == 0 {
				return fail("empty format argument")
			}
			arguments = append(arguments, token.New(current...))
			current = nil
			continue
		}
		current = append(current, node)
	}
	if len(current) > 0 {
		arguments = append(arguments, token.New(current...))
	}
	format := ""
	if len(arguments) == 0 {
		if !newline {
			return fail("print requires a format string literal")
		}
	} else {
		tokens := arguments[0].Tokens()
		for len(tokens) == 1 && tokens[0].Kind() == token.FragmentKind {
			tokens = tokens[0].Children().Tokens()
		}
		if len(tokens) != 1 || tokens[0].Kind() != token.LiteralKind || tokens[0].LiteralType() != token.String {
			return fail("print format must be a string literal")
		}
		var err error
		format, err = strconv.Unquote(tokens[0].Text())
		if err != nil {
			return fail(err.Error())
		}
		arguments = arguments[1:]
	}
	plan, err := parts(format)
	if err != nil {
		return fail(err.Error())
	}
	count := 0
	for _, part := range plan {
		if part.slot {
			count++
		}
	}
	if count != len(arguments) {
		return fail(fmt.Sprintf("format has %d placeholders but %d arguments", count, len(arguments)))
	}
	root, err := ctx.RuntimeRoot("example.org/printing-protocols")
	if err != nil {
		return fail(err.Error())
	}
	// Declarations keep this output in statement slots, and evaluate every
	// argument before running any formatter. No closure changes ? or recover.
	output := fragment(ctx, "var printingText string").Append(token.NewSemicolon(ctx.DefSite()))
	for i, argument := range arguments {
		output = output.Append(fragment(ctx, fmt.Sprintf("var printingArgument%d =", i)).Tokens()...).Append(argument.Tokens()...).Append(token.NewSemicolon(ctx.DefSite()))
	}
	index := 0
	for _, part := range plan {
		output = output.Append(fragment(ctx, "printingText +=").Tokens()...)
		if !part.slot {
			output = output.Append(fragment(ctx, strconv.Quote(part.literal)).Tokens()...)
		} else {
			member := "Display"
			if part.debug {
				member = "Debug"
			}
			call := root.Append(fragment(ctx, "::"+member).Tokens()...).Append(group(ctx, token.Parenthesis, fragment(ctx, fmt.Sprintf("&printingArgument%d", index))))
			output = output.Append(call.Tokens()...)
			index++
		}
		output = output.Append(token.NewSemicolon(ctx.DefSite()))
	}
	if newline {
		output = output.Append(fragment(ctx, "printingText += \"\\n\"").Tokens()...).Append(token.NewSemicolon(ctx.DefSite()))
	}
	output = output.Append(root.Tokens()...).Append(fragment(ctx, "::Write(printingText)").Tokens()...).Append(token.NewSemicolon(ctx.DefSite()))
	return pm.Success(output)
}

// The struct reader observes field names and replays the original syntax tokens.
// Its implementations use the same ordinary protocol as the print macro.
func deriveDebug(ctx pm.Context, input token.TokenStream) pm.Output {
	item, err := syn.Parse(input)
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	fields, ok := item.Type().StructFields()
	if !ok || item.Alias() || !item.Generics().IsEmpty() || !item.Where().IsEmpty() {
		return pm.Failure("LibraryDebug proof requires a concrete struct", item.Span())
	}
	root, err := ctx.RuntimeRoot("example.org/printing-protocols")
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	protocol := root.Append(fragment(ctx, "::LibraryDebug").Tokens()...)
	body := fragment(ctx, "return "+strconv.Quote(item.Name().Text()+" { "))
	var predicates token.TokenStream
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
			label := name.Text() + ": "
			if count != 0 {
				label = ", " + label
			}
			arg := fragment(ctx, "&self.").Append(name.ToTokens().Tokens()...)
			call := protocol.Append(fragment(ctx, "::debug").Tokens()...).Append(group(ctx, token.Parenthesis, arg))
			body = body.Append(fragment(ctx, "+"+strconv.Quote(label)+"+").Tokens()...).Append(call.Tokens()...)
			count++
		}
	}
	body = body.Append(fragment(ctx, "+\" }\"").Tokens()...)
	members := fragment(ctx, "func debug(self &Self) string").Append(group(ctx, token.Brace, body))
	output := fragment(ctx, "impl").Append(protocol.Tokens()...).Append(fragment(ctx, "for").Tokens()...).Append(item.Name().ToTokens().Tokens()...)
	if !predicates.IsEmpty() {
		output = output.Append(fragment(ctx, "where").Tokens()...).Append(predicates.Tokens()...)
	}
	return pm.Success(output.Append(group(ctx, token.Brace, members)))
}
