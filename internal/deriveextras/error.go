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
			value := "v." + name
			if id.Name != "string" {
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
	name := spec.Name.Name
	code := "func (v " + name + ") Error() string { return " + strings.Join(parts, " + ") + " }\n"
	if sourceField != "" {
		code += "func (v " + name + ") Unwrap() error { return v." + sourceField + " }\n"
	}
	if fromField != "" {
		code += "func From" + name + "(err error) " + name + " { return " + name + "{" + fromField + ": err} }\n"
	}
	return code, g.imports, nil
}

// GenerateEnumError implements explicitly attributed value cases.
func GenerateEnumError(d enums.Decl) (string, map[string]string, error) {
	var b strings.Builder
	imports := map[string]string{}
	for _, v := range d.Variants {
		fields := &ast.FieldList{}
		if v.Type != nil {
			fields.List = append(fields.List, &ast.Field{Names: []*ast.Ident{ast.NewIdent("Value")}, Type: v.Type})
		}
		spec := &ast.TypeSpec{Name: ast.NewIdent(v.CaseName), Type: &ast.StructType{Fields: fields}}
		source := ""
		if v.Source || v.From {
			source = "Value"
		}
		code, im, e := GenerateError(spec, v.Message, source, "")
		if e != nil {
			return "", nil, e
		}
		b.WriteString(code)
		if source == "" {
			fmt.Fprintf(&b, "func(v %s) Unwrap()error{return nil};", v.CaseName)
		}
		if v.From {
			fmt.Fprintf(&b, "func From%s(err error)%s{return %s(err)};", d.Name, d.Name, v.Constructor)
		}
		for k, x := range im {
			imports[k] = x
		}
	}
	return b.String(), imports, nil
}
