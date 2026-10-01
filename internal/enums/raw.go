package enums

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strconv"
	"strings"
)

func rawBase(name string) bool {
	switch name {
	case "string", "int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune":
		return true
	}
	return false
}
func (l *lowerer) parseRawDecl(i int) (Decl, int, error) {
	d := Decl{Name: l.ts[i+1].text, RawType: l.ts[i+2].text, Position: l.position(l.ts[i].start)}
	if d.Name == "Option" || d.Name == "Result" {
		return d, 0, l.err(l.ts[i+1].start, "reserved builtin sum type name")
	}
	if !rawBase(d.RawType) {
		return d, 0, l.err(l.ts[i+2].start, "raw enum requires an explicit builtin integer or string underlying type")
	}
	d.Marker = "__hgo_enum_marker_" + d.Name
	end, err := l.close(i + 3)
	if err != nil {
		return d, 0, err
	}
	seen := map[string]bool{}
	for j := i + 4; j < end; {
		if l.ts[j].text == "," || l.ts[j].kind == token.SEMICOLON {
			j++
			continue
		}
		if l.ts[j].kind != token.IDENT || l.ts[j].text == "_" {
			return d, 0, l.err(l.ts[j].start, "raw enum requires a named member with an explicit constant value; member attributes are unsupported")
		}
		t := l.ts[j]
		if seen[t.text] {
			return d, 0, l.err(t.start, "duplicate raw enum member name")
		}
		seen[t.text] = true
		if j+1 >= end || l.ts[j+1].text != "=" {
			return d, 0, l.err(t.start, "raw enum members require explicit constant values; implicit iota is unsupported")
		}
		start := j + 2
		j = start
		for j < end && l.ts[j].text != "," && l.ts[j].kind != token.SEMICOLON {
			if l.ts[j].kind == token.IDENT && l.ts[j].text == "iota" {
				return d, 0, l.err(l.ts[j].start, "iota is unsupported in explicit raw enum values")
			}
			if l.ts[j].text == "(" || l.ts[j].text == "[" || l.ts[j].text == "{" {
				last, err := l.close(j)
				if err != nil {
					return d, 0, err
				}
				j = last
			}
			j++
		}
		if start == j {
			return d, 0, l.err(t.start, "raw enum member requires a constant expression")
		}
		for k := start; k < j; k++ {
			if l.ts[k].kind == token.IDENT && l.ts[k].text == "iota" {
				return d, 0, l.err(l.ts[k].start, "iota is unsupported in explicit raw enum values")
			}
		}
		a, b := l.ts[start].start, l.ts[j-1].end
		// The region renderer resolves Enum::Member inside constant expressions.
		d.Variants = append(d.Variants, Variant{Name: t.text, RawValue: string(l.src[a:b]), ValueStart: a, ValueEnd: b, Constructor: "__hgo_enum_const_" + strconv.Itoa(len(d.Name)) + "_" + d.Name + "_" + t.text, Position: l.position(t.start)})
	}
	if len(d.Variants) == 0 {
		return d, 0, l.err(d.Position.Offset, "empty raw enum unsupported")
	}
	return d, end, nil
}
func RawExportName(d Decl, v Variant) string {
	return fmt.Sprintf("HgoConst%d_%s_%s", len(d.Name), d.Name, v.Name)
}
func (l *lowerer) rawDeclaration(d Decl) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "type %s %s; type %s = %s;\n", d.Name, d.RawType, d.Marker, d.Name)
	for _, v := range d.Variants {
		value, err := l.region(v.ValueStart, v.ValueEnd, nil)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "const %s %s = %s;\n", v.Constructor, d.Name, value)
		if d.Public {
			fmt.Fprintf(&b, "const %s %s = %s;\n", RawExportName(d, v), d.Name, v.Constructor)
		}
	}
	return b.String(), nil
}

// ValidateRaw uses Go's evaluated typed constants. It never compares expression
// spellings or treats a raw value as a closed variant representation.
func ValidateRaw(path string, files map[string][]byte, decls []Decl, imp types.Importer) error {
	active := false
	for _, d := range decls {
		active = active || d.RawType != ""
	}
	if !active {
		return nil
	}
	fs := token.NewFileSet()
	var trees []*ast.File
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		f, err := parser.ParseFile(fs, n, files[n], parser.AllErrors)
		if err != nil {
			return err
		}
		trees = append(trees, f)
	}
	conf := types.Config{Importer: imp}
	pkg, err := conf.Check(path, fs, trees, nil)
	if err != nil {
		return err
	}
	for _, d := range decls {
		if d.RawType == "" || d.PackagePath != "" {
			continue
		}
		obj, ok := pkg.Scope().Lookup(d.Name).(*types.TypeName)
		if !ok {
			return fmt.Errorf("%s: missing raw enum type metadata", d.Position)
		}
		if !types.Identical(obj.Type().Underlying(), types.Universe.Lookup(d.RawType).Type().Underlying()) {
			return fmt.Errorf("%s: raw enum underlying type must resolve to builtin %s", d.Position, d.RawType)
		}
		values := []*types.Const{}
		for _, v := range d.Variants {
			c, ok := pkg.Scope().Lookup(v.Constructor).(*types.Const)
			if !ok {
				return fmt.Errorf("%s: raw enum value is not a typed constant", v.Position)
			}
			for _, prior := range values {
				if constant.Compare(c.Val(), token.EQL, prior.Val()) {
					return fmt.Errorf("%s: same-value aliases pending user decision; unsupported in current subset", v.Position)
				}
			}
			values = append(values, c)
		}
	}
	return nil
}
