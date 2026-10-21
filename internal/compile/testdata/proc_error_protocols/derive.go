package macros

import (
	"fmt"
	"strconv"
	"strings"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/syn"
	"github.com/goxide-lang/syntax/token"
)

func errorFragment(ctx pm.Context, source string) token.TokenStream {
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

func errorSplice(ctx pm.Context, stream token.TokenStream, bindings map[string]token.TokenStream) token.TokenStream {
	result := token.New()
	for _, node := range stream.Tokens() {
		if value, ok := bindings[node.Text()]; ok && node.Kind() == token.IdentKind {
			result = result.Append(value.Tokens()...)
			continue
		}
		if node.Kind() == token.GroupKind {
			var err error
			node, err = token.NewGroup(node.Delimiter(), errorSplice(ctx, node.Children(), bindings), ctx.DefSite())
			if err != nil {
				panic(err)
			}
		}
		result = result.Append(node)
	}
	return result
}

func errorAttribute(attribute syn.Attribute, name string) bool {
	path := attribute.Path().Tokens()
	return len(path) == 1 && path[0].Text() == name
}

func errorMessage(message string, fields map[string]string) (string, error) {
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
		// An exact ordinary Go type-set enforces the builtin-scalar boundary.
		// The macro neither examines resolved types nor recognizes aliases.
		parts = append(parts, "__runtime::Scalar(self."+field+")")
		i = end + 1
	}
	flush()
	if len(parts) == 0 {
		return strconv.Quote(""), nil
	}
	return strings.Join(parts, "+"), nil
}

func deriveError(ctx pm.Context, input token.TokenStream) pm.Output {
	item, err := syn.Parse(input)
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	fields, ok := item.Type().StructFields()
	if !ok || item.Alias() || !item.Generics().IsEmpty() || !item.Where().IsEmpty() {
		return pm.Failure("LibraryError requires a concrete struct", item.Span())
	}
	message, found := "", false
	for _, attribute := range item.Attributes() {
		if !errorAttribute(attribute, "library_error") {
			return pm.Failure("message belongs to the struct; source belongs to a field", attribute.Span())
		}
		if found {
			return pm.Failure("duplicate library_error", attribute.Span())
		}
		found = true
		arguments := attribute.Arguments().Tokens()
		if len(arguments) != 1 || arguments[0].Kind() != token.GroupKind || arguments[0].Delimiter() != token.Parenthesis {
			return pm.Failure("message requires one string literal", attribute.Span())
		}
		parts := arguments[0].Children().Tokens()
		if len(parts) != 1 || parts[0].Kind() != token.LiteralKind || parts[0].LiteralType() != token.String {
			return pm.Failure("message requires one string literal", attribute.Span())
		}
		message, err = strconv.Unquote(parts[0].Text())
		if err != nil {
			return pm.Failure("message requires one string literal", attribute.Span())
		}
	}
	if !found {
		return pm.Failure("LibraryError requires a message", item.Span())
	}
	root, err := ctx.RuntimeRoot("example.org/error-runtime")
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	bindings := map[string]token.TokenStream{"__target": item.Name().ToTokens(), "__runtime": root}
	names := map[string]string{}
	source := ""
	for _, field := range fields {
		if len(field.Names()) == 0 {
			return pm.Failure("embedded fields have no Error derive protocol", field.Span())
		}
		isSource := false
		for _, attribute := range field.Attributes() {
			if !errorAttribute(attribute, "library_source") {
				return pm.Failure("message belongs to the struct; source belongs to a field", attribute.Span())
			}
			if isSource {
				return pm.Failure("duplicate library_source", attribute.Span())
			}
			if !attribute.Arguments().IsEmpty() {
				return pm.Failure("library_source takes no arguments", attribute.Span())
			}
			isSource = true
		}
		for _, name := range field.Names() {
			if name.Text() == "_" {
				return pm.Failure("blank fields have no Error derive protocol", name.Span())
			}
			key := fmt.Sprintf("__field%d", len(names))
			names[name.Text()] = key
			bindings[key] = name.ToTokens()
			if isSource {
				if source != "" {
					return pm.Failure("Error requires at most one source field", field.Span())
				}
				source = key
			}
		}
	}
	body, err := errorMessage(message, names)
	if err != nil {
		return pm.Failure(err.Error(), item.Span())
	}
	output := "impl __target {pub func Error(self Self)string{return " + body + "}"
	if source != "" {
		output += ";pub func Unwrap(self Self)error{return __runtime::Source(&self." + source + ")}"
	}
	output += "}"
	return pm.Success(errorSplice(ctx, errorFragment(ctx, output), bindings))
}
