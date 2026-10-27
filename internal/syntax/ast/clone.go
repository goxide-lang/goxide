package ast

import "reflect"

// CloneFile makes a working tree for consumers that must retain the original
// syntax. Node IDs, spans, lexical tokens and shared comment references survive
// unchanged; no mutable pointer or slice is shared with the input.
func CloneFile(file *File) *File {
	if file == nil {
		return nil
	}
	seen := map[any]reflect.Value{}
	var clone func(reflect.Value) reflect.Value
	clone = func(value reflect.Value) reflect.Value {
		switch value.Kind() {
		case reflect.Pointer:
			if value.IsNil() {
				return reflect.Zero(value.Type())
			}
			key := value.Interface()
			if copy, ok := seen[key]; ok {
				return copy
			}
			copy := reflect.New(value.Type().Elem())
			seen[key] = copy
			copy.Elem().Set(clone(value.Elem()))
			return copy
		case reflect.Interface:
			if value.IsNil() {
				return reflect.Zero(value.Type())
			}
			copy := reflect.New(value.Type()).Elem()
			copy.Set(clone(value.Elem()))
			return copy
		case reflect.Slice:
			if value.IsNil() {
				return reflect.Zero(value.Type())
			}
			copy := reflect.MakeSlice(value.Type(), value.Len(), value.Len())
			for i := 0; i < value.Len(); i++ {
				copy.Index(i).Set(clone(value.Index(i)))
			}
			return copy
		case reflect.Struct:
			copy := reflect.New(value.Type()).Elem()
			for i := 0; i < value.NumField(); i++ {
				copy.Field(i).Set(clone(value.Field(i)))
			}
			return copy
		default:
			return value
		}
	}
	return clone(reflect.ValueOf(file)).Interface().(*File)
}
