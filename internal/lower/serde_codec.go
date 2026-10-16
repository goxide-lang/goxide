package lower

import (
	"fmt"
	"go/types"
)

func (e *emitter) serdeWrite(t types.Type, value, serializer string) (string, error) {
	t = types.Unalias(t)
	a := e.serdePackage()
	if d := e.deriveFor(t); d != nil {
		if !d.Requests["Serialize"] {
			return "", fmt.Errorf("%s lacks registered Serialize", t)
		}
		return "return " + deriveExprText(e.deriveHelper(d, "Serialize")) + "(" + serializer + ",&(" + value + "))", nil
	}
	if option := e.info.EnumOf(t); option != nil && option.Enum.Builtin && option.Enum.Name == "Option" {
		callback, payload := e.fresh("Serializer").Name, e.fresh("Payload").Name
		body, err := e.serdeWrite(option.Args[0], payload, callback)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("switch %s(%s){case 0:return %s.None();case 1:%s:=%s(%s);return %s.Some(func(%s %s.Serializer)error{%s});default:return %s.NewError(%s.Type,\"invalid Option\")}", deriveExprText(e.sumMember("TagOption")), value, serializer, payload, deriveExprText(e.sumMember("ValueSome")), value, serializer, callback, a, body, a, a), nil
	}
	switch t := t.(type) {
	case *types.Basic:
		var method, conversion string
		switch {
		case t.Kind() == types.Bool:
			method = "Bool"
		case t.Kind() == types.String:
			method = "String"
		case t.Info()&types.IsInteger != 0 && t.Kind() != types.Uintptr:
			method, conversion = "Int64", "int64"
			if t.Info()&types.IsUnsigned != 0 {
				method, conversion = "Uint64", "uint64"
			}
		case t.Info()&types.IsFloat != 0:
			method, conversion = "Float64", "float64"
		default:
			return "", fmt.Errorf("%s has no registered serde codec", t)
		}
		if conversion != "" {
			value = conversion + "(" + value + ")"
		}
		return "return " + serializer + "." + method + "(" + value + ")", nil
	case *types.Pointer:
		callback := e.fresh("Serializer").Name
		body, err := e.serdeWrite(t.Elem(), "*("+value+")", callback)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("if %s==nil{return %s.None()};return %s.Some(func(%s %s.Serializer)error{%s})", value, serializer, serializer, callback, a, body), nil
	case *types.Array:
		return e.serdeWriteSequence(t.Elem(), value, serializer)
	case *types.Slice:
		return e.serdeWriteSequence(t.Elem(), value, serializer)
	case *types.Map:
		keyType, ok := types.Unalias(t.Key()).(*types.Basic)
		if !ok || keyType.Kind() != types.String {
			return "", fmt.Errorf("serde map keys require string")
		}
		writer, failure, key, item, callback := e.fresh("Writer").Name, e.fresh("Error").Name, e.fresh("Key").Name, e.fresh("Item").Name, e.fresh("Serializer").Name
		body, err := e.serdeWrite(t.Elem(), item, callback)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s,%s:=%s.Map(len(%s));if %s!=nil{return %s};for %s,%s:=range %s{if %s:=%s.Entry(func(%s %s.Serializer)error{return %s.String(%s)},func(%s %s.Serializer)error{%s});%s!=nil{return %s}};return %s.End()", writer, failure, serializer, value, failure, failure, key, item, value, failure, writer, callback, a, callback, key, callback, a, body, failure, failure, writer), nil
	}
	return "", fmt.Errorf("%s has no registered serde codec", t)
}

func (e *emitter) serdeWriteSequence(element types.Type, value, serializer string) (string, error) {
	a := e.serdePackage()
	writer, failure, index, callback := e.fresh("Writer").Name, e.fresh("Error").Name, e.fresh("Index").Name, e.fresh("Serializer").Name
	body, err := e.serdeWrite(element, "("+value+")["+index+"]", callback)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s,%s:=%s.Seq(len(%s));if %s!=nil{return %s};for %s:=range %s{if %s:=%s.Element(func(%s %s.Serializer)error{%s});%s!=nil{return %s}};return %s.End()", writer, failure, serializer, value, failure, failure, index, value, failure, writer, callback, a, body, failure, failure, writer), nil
}

func (e *emitter) serdeRead(t types.Type, deserializer string) (string, error) {
	t = types.Unalias(t)
	a := e.serdePackage()
	if d := e.deriveFor(t); d != nil {
		if !d.Requests["Deserialize"] {
			return "", fmt.Errorf("%s lacks registered Deserialize", t)
		}
		return deriveExprText(e.deriveHelper(d, "Deserialize")) + "(" + deserializer + ")", nil
	}
	typ := deriveExprText(e.goType(t))
	value, failure := e.fresh("Value").Name, e.fresh("Error").Name
	if option := e.info.EnumOf(t); option != nil && option.Enum.Builtin && option.Enum.Name == "Option" {
		callback, payload := e.fresh("Deserializer").Name, e.fresh("Payload").Name
		read, err := e.serdeRead(option.Args[0], callback)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("func()(%s,error){var %s %s;%s:=%s.Option(%s.VisitorFuncs{OnNone:func()error{return nil},OnSome:func(%s %s.Deserializer)error{%s,%s:=%s;if %s!=nil{return %s};%s=%s(%s);return nil}});return %s,%s}()", typ, value, typ, failure, deserializer, a, callback, a, payload, failure, read, failure, failure, value, deriveExprText(e.sumMember("NewSome")), payload, value, failure), nil
	}
	switch t := t.(type) {
	case *types.Basic:
		if t.Kind() == types.String || t.Kind() == types.Bool {
			method := "ReadString"
			if t.Kind() == types.Bool {
				method = "ReadBool"
			}
			return a + "." + method + "(" + deserializer + ")", nil
		}
		raw := e.fresh("Scalar").Name
		method, conversion := "", ""
		switch {
		case t.Info()&types.IsInteger != 0 && t.Kind() != types.Uintptr:
			method, conversion = "ReadInt64", "int64"
			if t.Info()&types.IsUnsigned != 0 {
				method, conversion = "ReadUint64", "uint64"
			}
		case t.Info()&types.IsFloat != 0:
			method = "ReadFloat64"
		default:
			return "", fmt.Errorf("%s has no registered serde codec", t)
		}
		body := fmt.Sprintf("var %s %s;%s,%s:=%s.%s(%s);if %s!=nil{return %s,%s};", value, typ, raw, failure, a, method, deserializer, failure, value, failure)
		if t.Kind() == types.Float32 {
			body += fmt.Sprintf("if %s>3.4028234663852886e38||%s< -3.4028234663852886e38||(%s!=0&&float32(%s)==0){return %s,%s.NewError(%s.Range,\"float32 overflow\")};", raw, raw, raw, raw, value, a, a)
		}
		body += value + "=" + typ + "(" + raw + ");"
		if conversion != "" {
			body += fmt.Sprintf("if %s(%s)!=%s{return %s,%s.NewError(%s.Range,\"integer overflow\")};", conversion, value, raw, value, a, a)
		}
		return "func()(" + typ + ",error){" + body + "return " + value + ",nil}()", nil
	case *types.Pointer:
		callback, payload := e.fresh("Deserializer").Name, e.fresh("Payload").Name
		read, err := e.serdeRead(t.Elem(), callback)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("func()(%s,error){var %s %s;%s:=%s.Option(%s.VisitorFuncs{OnNone:func()error{return nil},OnSome:func(%s %s.Deserializer)error{%s,%s:=%s;if %s!=nil{return %s};%s=&%s;return nil}});return %s,%s}()", typ, value, typ, failure, deserializer, a, callback, a, payload, failure, read, failure, failure, value, payload, value, failure), nil
	case *types.Array:
		return e.serdeReadSequence(t, t.Elem(), t.Len(), deserializer)
	case *types.Slice:
		return e.serdeReadSequence(t, t.Elem(), -1, deserializer)
	case *types.Map:
		keyType, ok := types.Unalias(t.Key()).(*types.Basic)
		if !ok || keyType.Kind() != types.String {
			return "", fmt.Errorf("serde map keys require string")
		}
		access, callback, key, more, item := e.fresh("Access").Name, e.fresh("Deserializer").Name, e.fresh("Key").Name, e.fresh("More").Name, e.fresh("Item").Name
		read, err := e.serdeRead(t.Elem(), callback)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("func()(%s,error){%s:=make(%s);%s:=%s.Map(%s.VisitorFuncs{OnMap:func(%s %s.MapAccess)error{for{var %s string;%s,%s:=%s.NextKey(func(%s %s.Deserializer)error{var %s error;%s,%s=%s.ReadString(%s);return %s});if %s!=nil{return %s};if !%s{break};if _,%s:=%s[%s];%s{return %s.AtField(%s.NewError(%s.DuplicateField,\"duplicate map key\"),%s)};if %s:=%s.NextValue(func(%s %s.Deserializer)error{%s,%s:=%s;if %s!=nil{return %s};%s[%s]=%s;return nil});%s!=nil{return %s}};return nil}});return %s,%s}()", typ, value, typ, failure, deserializer, a, access, a, key, more, failure, access, callback, a, failure, key, failure, a, callback, failure, failure, failure, more, more, value, key, more, a, a, a, key, failure, access, callback, a, item, failure, read, failure, failure, value, key, item, failure, failure, value, failure), nil
	}
	return "", fmt.Errorf("%s has no registered serde codec", t)
}

func (e *emitter) serdeReadSequence(t, element types.Type, length int64, deserializer string) (string, error) {
	a, typ := e.serdePackage(), deriveExprText(e.goType(t))
	value, failure, access, callback, index, more, item := e.fresh("Value").Name, e.fresh("Error").Name, e.fresh("Access").Name, e.fresh("Deserializer").Name, e.fresh("Index").Name, e.fresh("More").Name, e.fresh("Item").Name
	read, err := e.serdeRead(element, callback)
	if err != nil {
		return "", err
	}
	init, guard, assign, finish := value+":=make("+typ+",0);", "", value+"=append("+value+","+item+");", ""
	if length >= 0 {
		init = "var " + value + " " + typ + ";"
		guard = fmt.Sprintf("if %s>=len(%s){return %s.NewError(%s.Range,\"too many array elements\")};", index, value, a, a)
		assign = value + "[" + index + "]=" + item + ";"
		finish = fmt.Sprintf("if %s!=len(%s){return %s.NewError(%s.Range,\"wrong array length\")};", index, value, a, a)
	}
	return fmt.Sprintf("func()(%s,error){%s%s:=%s.Seq(%s.VisitorFuncs{OnSeq:func(%s %s.SeqAccess)error{%s:=0;for{%s,%s:=%s.NextElement(func(%s %s.Deserializer)error{%s%s,%s:=%s;if %s!=nil{return %s};%sreturn nil});if %s!=nil{return %s};if !%s{break};%s++};%sreturn nil}});return %s,%s}()", typ, init, failure, deserializer, a, access, a, index, more, failure, access, callback, a, guard, item, failure, read, failure, failure, assign, failure, failure, more, index, finish, value, failure), nil
}
