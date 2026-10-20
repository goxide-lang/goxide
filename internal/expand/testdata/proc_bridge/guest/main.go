package main

import (
	"fmt"
	"os"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

func duplicate(ctx pm.Context, input token.TokenStream) pm.Output {
	if input.Len() != 1 || input.Tokens()[0].Kind() != token.FragmentKind {
		return pm.Failure("expected a captured expression", ctx.CallSpan())
	}
	plus, err := token.NewPunct("+", ctx.DefSite())
	if err != nil {
		return pm.Failure(err.Error(), ctx.DefSpan())
	}
	definition, err := token.NewIdent("bonus", false, ctx.DefSite())
	if err != nil {
		return pm.Failure(err.Error(), ctx.DefSpan())
	}
	caller, err := token.NewIdent("bonus", false, ctx.CallSite())
	if err != nil {
		return pm.Failure(err.Error(), ctx.CallSpan())
	}
	// Associate the generated occurrences with a diagnostic source position.
	// Both copies retain the captured expression's original binding identity.
	captured := input.Tokens()[0].WithSpan(ctx.DefSpan())
	return pm.Success(token.New(captured, plus, captured, plus,
		definition.WithSpan(ctx.CallSpan()), plus, caller.WithSpan(ctx.DefSpan())))
}

func run() error {
	request, err := pm.ReadRequest(os.Stdin)
	if err != nil {
		return err
	}
	output, err := pm.Dispatch(request, []pm.Entry{{Name: "duplicate", Function: duplicate}})
	if err != nil {
		return err
	}
	return pm.WriteResponse(os.Stdout, request, output)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
