package deriveextras

import (
	"fmt"
	"github.com/hxsf/goxide/internal/enums"
	"go/ast"
	"strconv"
	"strings"
)

// GenerateError supports a literal message with named scalar field placeholders,
// one explicit error source, and a From helper only for a sole error field.
// It does not infer dynamic concrete variants from Go's error interface.
func GenerateError(spec *ast.TypeSpec, message, sourceField, fromField string) (string, map[string]string, error) {
	s, err := fields(spec)
	if err != nil {
		return "", nil, err
	}
	byName := map[string]ast.Expr{}
	count := 0
	for _, f := range s.Fields.List {
		for _, n := range f.Names {
			byName[n.Name] = f.Type
			count++
		}
	}
	if fromField != "" {
		if sourceField != "" && sourceField != fromField {
			return "", nil, fmt.Errorf("from and source must identify the same field")
		}
		sourceField = fromField
		if count != 1 {
			return "", nil, fmt.Errorf("from requires exactly one field; other fields need explicit construction")
		}
	}
	if sourceField != "" {
		id, ok := byName[sourceField].(*ast.Ident)
		if !ok || id.Name != "error" {
			return "", nil, fmt.Errorf("source field %s must have type error", sourceField)
		}
	}
	formatted, imports, err := formatErrorMessage(message, byName, func(name string) string { return "v." + name })
	if err != nil {
		return "", nil, err
	}
	name := spec.Name.Name
	code := "func (v " + name + ") Error() string { return " + formatted + " }\n"
	if sourceField != "" {
		code += "func (v " + name + ") Unwrap() error { return v." + sourceField + " }\n"
	}
	if fromField != "" {
		code += "func From" + name + "(err error) " + name + " { return " + name + "{" + fromField + ": err} }\n"
	}
	return code, imports, nil
}

// formatErrorMessage shares the attribute grammar between structs and tagged enums.
func formatErrorMessage(message string, byName map[string]ast.Expr, fieldValue func(string) string) (string, map[string]string, error) {
	g := &generator{imports: map[string]string{}}
	var parts []string
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			parts = append(parts, strconv.Quote(literal.String()))
			literal.Reset()
		}
	}
	for i := 0; i < len(message); {
		if message[i] == '{' {
			if i+1 < len(message) && message[i+1] == '{' {
				literal.WriteByte('{')
				i += 2
				continue
			}
			flush()
			j := strings.IndexByte(message[i+1:], '}')
			if j < 0 {
				return "", nil, fmt.Errorf("unclosed error placeholder")
			}
			j += i + 1
			name := message[i+1 : j]
			typ, ok := byName[name]
			if !ok {
				return "", nil, fmt.Errorf("unknown error placeholder %q", name)
			}
			id, ok := typ.(*ast.Ident)
			if !ok || !scalar(id.Name) {
				return "", nil, fmt.Errorf("error placeholder %s requires a built-in scalar", name)
			}
			value := fieldValue(name)
			if id.Name != "string" {
				var err error
				value, err = g.debug(typ, value)
				if err != nil {
					return "", nil, err
				}
			}
			parts = append(parts, value)
			i = j + 1
			continue
		}
		if message[i] == '}' {
			if i+1 < len(message) && message[i+1] == '}' {
				literal.WriteByte('}')
				i += 2
				continue
			}
			return "", nil, fmt.Errorf("unmatched closing brace in error message")
		}
		literal.WriteByte(message[i])
		i++
	}
	flush()
	if len(parts) == 0 {
		parts = []string{`""`}
	}
	return strings.Join(parts, " + "), g.imports, nil
}

// GenerateEnumError dispatches on an enum value snapshot, never inactive slots.
func GenerateEnumError(d enums.Decl) (string, map[string]string, error) {
	var methods, errorBody, unwrapBody strings.Builder
	imports := map[string]string{}
	const receiver = "__goxide_enum_error_receiver"
	const payload = "__goxide_enum_error_payload"
	fmt.Fprintf(&errorBody, "%s(%s);switch %s(%s){", d.Validator, receiver, d.Tag, receiver)
	fmt.Fprintf(&unwrapBody, "%s(%s);switch %s(%s){", d.Validator, receiver, d.Tag, receiver)
	for _, v := range d.Variants {
		fields := map[string]ast.Expr{}
		if v.Type != nil {
			fields["Value"] = v.Type
		}
		if v.Source || v.From {
			id, ok := v.Type.(*ast.Ident)
			if !ok || id.Name != "error" {
				return "", nil, fmt.Errorf("source field Value must have type error")
			}
		}
		expression, im, err := formatErrorMessage(v.Message, fields, func(string) string { return payload })
		if err != nil {
			return "", nil, err
		}
		for k, path := range im {
			imports[k] = path
		}
		fmt.Fprintf(&errorBody, "case %d:", v.Tag)
		if v.Type != nil {
			fmt.Fprintf(&errorBody, "%s:=%s(%s);_=%s;", payload, v.Value, receiver, payload)
		}
		fmt.Fprintf(&errorBody, "return %s;", expression)
		fmt.Fprintf(&unwrapBody, "case %d:", v.Tag)
		if v.Source || v.From {
			fmt.Fprintf(&unwrapBody, "return %s(%s);", v.Value, receiver)
		} else {
			unwrapBody.WriteString("return nil;")
		}
		if v.From {
			fmt.Fprintf(&methods, "func From%s(err error)%s{return %s(err)};", d.Name, d.Name, v.Constructor)
		}
	}
	fmt.Fprintf(&errorBody, "default:panic(%q)}", "goxide: invalid enum "+d.Name)
	fmt.Fprintf(&unwrapBody, "default:panic(%q)}", "goxide: invalid enum "+d.Name)
	fmt.Fprintf(&methods, "func(%s %s) Error()string{%s};func(%s %s) Unwrap()error{%s};", receiver, d.Name, errorBody.String(), receiver, d.Name, unwrapBody.String())
	return methods.String(), imports, nil
}
