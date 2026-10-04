package macros

import (
	"fmt"
	"strings"
)

func goResultName(name string) bool { return name == "go_result" || name == "go_result_keep" }

// Keep the adapter opaque to template hygiene until caller captures have been
// substituted. The eventual typed helper receives the call's results as
// arguments; the caller expression is never put inside its body.
func (e *engine) goResultMarker(call tok, args []tok, depth int) ([]tok, error) {
	var comma = -1
	for i := 0; i < len(args); i++ {
		switch args[i].text {
		case "(", "[", "{":
			end, err := closeGroup(args, i)
			if err != nil {
				return nil, err
			}
			i = end
		case ",":
			if comma >= 0 {
				return nil, at(call, "Go result macro expects (call) or (T, call)")
			}
			comma = i
		}
	}
	name := "__goxide_" + call.text
	if comma < 0 {
		name += "_auto"
	}
	makeToken := func(s string) tok { t := call; t.text = s; t.before = " "; return t }
	marker := makeToken(name)
	marker.before = call.before
	out := []tok{marker}
	if comma >= 0 {
		if comma == 0 {
			return nil, at(call, "missing Go result value type")
		}
		out = append(out, makeToken("["))
		out = append(out, args[:comma]...)
		out = append(out, makeToken("]"))
		args = args[comma+1:]
	}
	if len(args) == 0 {
		return nil, at(call, "missing Go result call")
	}
	inner, err := e.expand(args, depth+1)
	if err != nil {
		return nil, err
	}
	out = append(out, makeToken("("))
	out = append(out, inner...)
	out = append(out, makeToken(")"))
	return out, nil
}

// Only reserve the nominal family here. Actual arity and types belong to the
// typed fixed point, after caller captures and native generic inference.
func (e *engine) finishGoResults(ts []tok) ([]tok, error) {
	count := 0
	for _, t := range ts {
		if strings.HasPrefix(t.text, "__goxide_go_result") {
			count++
		}
	}
	if count != 0 {
		e.alias("type Result[struct{},struct{}]")
		// Charge the fixed adapter scaffolding before typed generation; the
		// typed pass additionally bounds its actual generated AST nodes.
		e.emitted += count * 128
		if e.emitted > maxTokens {
			return nil, fmt.Errorf("%s: Go result macro output token budget exceeded", e.file)
		}
	}
	return ts, nil
}
