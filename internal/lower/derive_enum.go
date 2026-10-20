package lower

import (
	"fmt"
	"go/types"
	"strconv"
	"strings"

	"github.com/goxide-lang/goxide/internal/check"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (e *emitter) deriveEnumValue(kind string, instance *check.Instance, x string) (string, error) {
	typ := deriveExprText(e.instanceType(instance))
	if kind == "Default" {
		if instance.Enum.Builtin {
			if instance.Enum.Name != "Option" {
				return "", fmt.Errorf("Result has no Default implementation")
			}
			return deriveExprText(enumIndices(e.sumMember("None"), e.enumArgs(instance))) + "()", nil
		}
		v, err := deriveDefaultVariant(instance)
		if err != nil {
			return "", err
		}
		var fields []string
		for _, f := range instance.Fields(v) {
			value, err := e.deriveValue(kind, f.Type, "")
			if err != nil {
				return "", err
			}
			fields = append(fields, f.GoName+": "+value)
		}
		return "&" + deriveExprText(e.variantType(instance, v)) + "{" + strings.Join(fields, ", ") + "}", nil
	}
	src, active := e.fresh("Enum").Name, e.fresh("Variant").Name
	result := typ
	if kind == "Debug" {
		result = "string"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "func(%s %s) %s {", src, typ, result)
	if instance.Enum.Builtin {
		fmt.Fprintf(&b, "switch %s(%s) {", deriveExprText(e.sumMember("Tag"+instance.Enum.Name)), src)
	} else {
		fmt.Fprintf(&b, "switch %s := %s.(type) {", active, src)
	}
	for _, v := range instance.Enum.Variants {
		if instance.Enum.Builtin {
			fmt.Fprintf(&b, "case %d:", variantTag(v.Name))
		} else {
			fmt.Fprintf(&b, "case *%s: if %s == nil {panic(%q)};", deriveExprText(e.variantType(instance, v)), active, "goxide: invalid enum "+instance.Enum.Name)
		}
		fields := instance.Fields(v)
		values := []string{}
		for _, f := range fields {
			field := active + "." + f.GoName
			if instance.Enum.Builtin {
				field = e.fresh("Payload").Name
				fmt.Fprintf(&b, "%s := %s(%s);", field, deriveExprText(e.sumMember("Value"+v.Name)), src)
			}
			value, err := e.deriveValue(kind, f.Type, field)
			if err != nil {
				return "", err
			}
			if kind == "Clone" && !instance.Enum.Builtin {
				value = f.GoName + ": " + value
			}
			values = append(values, value)
		}
		if kind == "Clone" {
			if instance.Enum.Builtin {
				constructor := "New" + v.Name
				if v.Name == "None" {
					constructor = "None"
				}
				fmt.Fprintf(&b, "return %s(%s);", deriveExprText(enumIndices(e.sumMember(constructor), e.enumArgs(instance))), strings.Join(values, ", "))
			} else {
				fmt.Fprintf(&b, "return &%s{%s};", deriveExprText(e.variantType(instance, v)), strings.Join(values, ", "))
			}
		} else {
			prefix := instance.Enum.Name + "::" + v.Name
			parts := []string{strconv.Quote(prefix)}
			if len(fields) > 0 {
				left, right := "(", ")"
				if v.Decl != nil && v.Decl.Kind == ast.Struct {
					left, right = " { ", " }"
				}
				parts = append(parts, strconv.Quote(left))
				for i, value := range values {
					if i > 0 {
						parts = append(parts, `", "`)
					}
					if v.Decl != nil && v.Decl.Kind == ast.Struct {
						parts = append(parts, strconv.Quote(fields[i].Name+": "))
					}
					parts = append(parts, value)
				}
				parts = append(parts, strconv.Quote(right))
			}
			fmt.Fprintf(&b, "return %s;", strings.Join(parts, " + "))
		}
	}
	fmt.Fprintf(&b, "default:panic(%q)}}(%s)", "goxide: invalid enum "+instance.Enum.Name, x)
	return b.String(), nil
}

// Validate enum operands before equality, including their active payloads.
func (e *emitter) deriveValidation(t types.Type, x string, seen map[types.Type]bool) string {
	t = types.Unalias(t)
	if seen[t] {
		return ""
	}
	seen[t] = true
	defer delete(seen, t)
	if instance := e.info.EnumOf(t); instance != nil {
		var b strings.Builder
		active := e.fresh("Validated").Name
		if instance.Enum.Builtin {
			fmt.Fprintf(&b, "switch %s(%s) {", deriveExprText(e.sumMember("Tag"+instance.Enum.Name)), x)
		} else {
			fmt.Fprintf(&b, "switch %s := (%s).(type) {", active, x)
		}
		for _, v := range instance.Enum.Variants {
			if instance.Enum.Builtin {
				fmt.Fprintf(&b, "case %d:", variantTag(v.Name))
			} else {
				fmt.Fprintf(&b, "case *%s:if %s==nil{panic(%q)};", deriveExprText(e.variantType(instance, v)), active, "goxide: invalid enum "+instance.Enum.Name)
			}
			for _, f := range instance.Fields(v) {
				field := active + "." + f.GoName
				if instance.Enum.Builtin {
					field = deriveExprText(e.sumMember("Value"+v.Name)) + "(" + x + ")"
				}
				b.WriteString(e.deriveValidation(f.Type, field, seen))
			}
		}
		fmt.Fprintf(&b, "default:panic(%q)};", "goxide: invalid enum "+instance.Enum.Name)
		return b.String()
	}
	if n, ok := t.(*types.Named); ok {
		t = n.Underlying()
	}
	switch t := t.(type) {
	case *types.Struct:
		var b strings.Builder
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			b.WriteString(e.deriveValidation(f.Type(), "("+x+")."+f.Name(), seen))
		}
		return b.String()
	case *types.Array:
		return e.deriveValidateItems(t.Elem(), x, seen)
	case *types.Slice:
		return e.deriveValidateItems(t.Elem(), x, seen)
	case *types.Map:
		return e.deriveValidateItems(t.Elem(), x, seen)
	}
	return ""
}

func (e *emitter) deriveValidateItems(elem types.Type, x string, seen map[types.Type]bool) string {
	item := e.fresh("CheckedItem").Name
	body := e.deriveValidation(elem, item, seen)
	if body == "" {
		return ""
	}
	return "for _, " + item + " := range " + x + " {" + body + "};"
}

func (e *emitter) deriveEqual(t types.Type, x, y string, root bool) (string, error) {
	t = types.Unalias(t)
	if !root {
		if d := e.deriveFor(t); deriveHas(d, "PartialEq") {
			return deriveExprText(e.deriveHelper(d, "PartialEq")) + "(" + x + ", " + y + ")", nil
		}
	}
	if instance := e.info.EnumOf(t); instance != nil {
		return e.deriveEnumEqual(instance, x, y)
	}
	typ := deriveExprText(e.goType(t))
	if n, ok := t.(*types.Named); ok {
		t = n.Underlying()
	}
	switch t := t.(type) {
	case *types.Basic:
		return "(" + x + " == " + y + ")", nil
	case *types.Struct:
		var terms []string
		for i := 0; i < t.NumFields(); i++ {
			f := t.Field(i)
			term, err := e.deriveEqual(f.Type(), "("+x+")."+f.Name(), "("+y+")."+f.Name(), false)
			if err != nil {
				return "", err
			}
			terms = append(terms, term)
		}
		if len(terms) == 0 {
			return "(0 == 0)", nil
		}
		return strings.Join(terms, " && "), nil
	case *types.Array:
		return e.deriveSequenceEqual(typ, t.Elem(), x, y, false)
	case *types.Slice:
		return e.deriveSequenceEqual(typ, t.Elem(), x, y, true)
	case *types.Map:
		left, right, key, item, other, ok := e.fresh("Left").Name, e.fresh("Right").Name, e.fresh("Key").Name, e.fresh("Item").Name, e.fresh("Other").Name, e.fresh("Found").Name
		cmp, err := e.deriveEqual(t.Elem(), item, other, false)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("func(%s,%s %s)bool{if (%s==nil)!=(%s==nil)||len(%s)!=len(%s){return 0!=0};for %s,%s:=range %s{%s,%s:=%s[%s];if !%s||!(%s){return 0!=0}};return 0==0}(%s,%s)", left, right, typ, left, right, left, right, key, item, left, other, ok, right, key, ok, cmp, x, y), nil
	}
	return "", fmt.Errorf("%s has no explicit PartialEq implementation", t)
}

func (e *emitter) deriveSequenceEqual(typ string, elem types.Type, x, y string, slice bool) (string, error) {
	left, right, index := e.fresh("Left").Name, e.fresh("Right").Name, e.fresh("Index").Name
	cmp, err := e.deriveEqual(elem, left+"["+index+"]", right+"["+index+"]", false)
	if err != nil {
		return "", err
	}
	pre := ""
	if slice {
		pre = fmt.Sprintf("if (%s==nil)!=(%s==nil)||len(%s)!=len(%s){return 0!=0};", left, right, left, right)
	}
	return fmt.Sprintf("func(%s,%s %s)bool{%s for %s:=range %s{if !(%s){return 0!=0}};return 0==0}(%s,%s)", left, right, typ, pre, index, left, cmp, x, y), nil
}

func (e *emitter) deriveEnumEqual(instance *check.Instance, x, y string) (string, error) {
	typ := deriveExprText(e.instanceType(instance))
	left, right, active, other, ok := e.fresh("Left").Name, e.fresh("Right").Name, e.fresh("Variant").Name, e.fresh("Other").Name, e.fresh("Same").Name
	var b strings.Builder
	fmt.Fprintf(&b, "func(%s,%s %s)bool{", left, right, typ)
	b.WriteString(e.deriveValidation(instanceTypeForDerive(instance), left, map[types.Type]bool{}))
	b.WriteString(e.deriveValidation(instanceTypeForDerive(instance), right, map[types.Type]bool{}))
	if instance.Enum.Builtin {
		fmt.Fprintf(&b, "if %s(%s)!=%s(%s){return 0!=0};switch %s(%s){", deriveExprText(e.sumMember("Tag"+instance.Enum.Name)), left, deriveExprText(e.sumMember("Tag"+instance.Enum.Name)), right, deriveExprText(e.sumMember("Tag"+instance.Enum.Name)), left)
	} else {
		fmt.Fprintf(&b, "switch %s:=%s.(type){", active, left)
	}
	for _, v := range instance.Enum.Variants {
		if instance.Enum.Builtin {
			fmt.Fprintf(&b, "case %d:", variantTag(v.Name))
		} else {
			fmt.Fprintf(&b, "case *%s:%s,%s:=%s.(*%s);if !%s{return 0!=0};_= %s;_= %s;", deriveExprText(e.variantType(instance, v)), other, ok, right, deriveExprText(e.variantType(instance, v)), ok, active, other)
		}
		var terms []string
		for _, f := range instance.Fields(v) {
			a, z := active+"."+f.GoName, other+"."+f.GoName
			if instance.Enum.Builtin {
				access := deriveExprText(e.sumMember("Value" + v.Name))
				a, z = access+"("+left+")", access+"("+right+")"
			}
			term, err := e.deriveEqual(f.Type, a, z, false)
			if err != nil {
				return "", err
			}
			terms = append(terms, term)
		}
		if len(terms) == 0 {
			terms = []string{"0==0"}
		}
		fmt.Fprintf(&b, "return %s;", strings.Join(terms, " && "))
	}
	fmt.Fprintf(&b, "default:panic(%q)}}(%s,%s)", "goxide: invalid enum "+instance.Enum.Name, x, y)
	return b.String(), nil
}

func instanceTypeForDerive(i *check.Instance) types.Type {
	if len(i.Args) == 0 {
		return i.Enum.Type
	}
	t, _ := types.Instantiate(nil, i.Enum.Type, i.Args, false)
	return t
}
