package macros

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"
)

func goResultName(name string) bool { return name == "go_result" || name == "go_result_keep" }

// Keep the adapter opaque to template hygiene until caller captures have been
// substituted. The eventual function literal receives the call's results as
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
	name := "__hgo_" + call.text
	if comma < 0 {
		name += "_unit"
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

func (e *engine) finishGoResults(ts []tok) ([]tok, error) {
	var out []tok
	for i := 0; i < len(ts); i++ {
		call := ts[i]
		if !strings.HasPrefix(call.text, "__hgo_go_result") {
			out = append(out, call)
			continue
		}
		if e.callerNames()["error"] || e.imports["error"] {
			return nil, at(call, "Go result macros require the predeclared error type; package/import shadowing of error is unsupported")
		}
		unit := strings.HasSuffix(call.text, "_unit")
		keep := strings.Contains(call.text, "_keep")
		j := i + 1
		var typ []tok
		if !unit {
			end, err := closeGroup(ts, j)
			if err != nil {
				return nil, err
			}
			typ = ts[j+1 : end]
			j = end + 1
			if _, err := parser.ParseFile(token.NewFileSet(), "", "package p;type X "+render(typ), 0); err != nil {
				return nil, at(call, "invalid Go result value type: "+err.Error())
			}
		}
		end, err := closeGroup(ts, j)
		if err != nil {
			return nil, err
		}
		args, err := e.finishGoResults(ts[j+1 : end])
		if err != nil {
			return nil, err
		}
		errType := e.alias("type error")
		value := "__hgo_go_result_value"
		errName := "__hgo_go_result_error"
		zero := "__hgo_go_result_zero"
		typeSlot, argSlot := e.fresh(), e.fresh()
		if unit {
			typeSlot = "struct{}"
		}
		payload := errType
		if keep {
			payload = "struct{Value " + typeSlot + "; Err " + errType + "}"
		}
		result := "Result[" + typeSlot + "," + payload + "]"
		// Bind the nominal family outside caller locals named Result. The
		// constructor occurrences are separately lowered to reserved functions.
		concreteResult := result
		if !unit {
			concreteResult = strings.ReplaceAll(result, typeSlot, render(typ))
		}
		resultType := e.alias("type " + concreteResult)
		params := value + " " + typeSlot + "," + errName + " " + errType
		prefix := ""
		if unit {
			params = errName + " " + errType
			prefix = value + ":=struct{}{};"
		}
		failure := errName
		if keep {
			failure = payload + "{Value:" + value + ",Err:" + errName + "}"
		}
		text := fmt.Sprintf("(func(%s)%s{%svar %s %s;if %s!=%s{return %s::Err(%s)};return %s::Ok(%s)})(%s)", params, resultType, prefix, zero, errType, errName, zero, result, failure, result, value, argSlot)
		body, _, err := lex(e.file, []byte(text))
		if err != nil {
			return nil, err
		}
		for k, t := range body {
			t.pos = call.pos
			t.sourceEnd = call.sourceEnd
			t.original = call.original
			t.stack = call.stack
			if k == 0 {
				t.before = call.before
			}
			switch t.text {
			case typeSlot:
				copyType := append([]tok(nil), typ...)
				if len(copyType) > 0 {
					copyType[0].before = t.before + " " + copyType[0].before
				}
				out = append(out, copyType...)
			case argSlot:
				out = append(out, args...)
			default:
				out = append(out, t)
			}
		}
		i = end
	}
	if len(out) > maxTokens {
		return nil, fmt.Errorf("%s: Go result macro output token budget exceeded", e.file)
	}
	return out, nil
}
