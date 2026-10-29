package check

import (
	"go/types"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestTypeStateKeepsArraySubstitutionsPrivate(t *testing.T) {
	parameter := types.NewTypeParam(types.NewTypeName(0, nil, "T", nil), anyType)
	length := &ConstParam{Name: &ast.Ident{Name: "N"}}
	info := &Info{}
	original := info.ArrayType(parameter, ConstValue{Param: length})
	state := &TypeState{}
	sub := map[*types.TypeParam]types.Type{parameter: types.NewSlice(types.Typ[types.Int])}
	private := state.Substitute(info, original, sub, nil)
	array, ok := state.ArrayOf(info, private)
	if !ok || array.Length.Param != length || !types.Identical(array.Elem, sub[parameter]) {
		t.Fatalf("private array = %#v", array)
	}
	if _, ok := info.ArrayOf(private); ok || len(info.ArrayPatterns) != 1 {
		t.Fatal("private array was published in checked info")
	}
	if state.Comparable(info, private) {
		t.Fatal("array with slice elements became comparable")
	}
	if again := state.Substitute(info, original, sub, nil); again != private {
		t.Fatal("one consumer did not reuse its array representative")
	}
	if unchanged := state.Substitute(info, original, nil, nil); unchanged != original {
		t.Fatal("unchanged checked representative was replaced")
	}
	other := &TypeState{}
	if other.Substitute(info, original, sub, nil) == private {
		t.Fatal("independent consumers shared a mutable representative registry")
	}
	concrete := state.Substitute(info, private, nil, map[*ConstParam]ConstValue{length: {Value: 3}})
	if array, ok := concrete.(*types.Array); !ok || array.Len() != 3 || !types.Identical(array.Elem(), sub[parameter]) {
		t.Fatalf("concrete array = %v", concrete)
	}
	if got, _ := info.ArrayOf(original); got.Elem != parameter || got.Length.Param != length {
		t.Fatalf("original pattern changed: %#v", got)
	}
}

func TestTypeStateNestedFunctionBindersRemainPrivate(t *testing.T) {
	parameter := types.NewTypeParam(types.NewTypeName(0, nil, "T", nil), anyType)
	outer := types.NewTypeParam(types.NewTypeName(0, nil, "Outer", nil), anyType)
	length := &ConstParam{Name: &ast.Ident{Name: "N"}}
	info := &Info{}
	array := info.ArrayType(parameter, ConstValue{Param: length})
	signature := types.NewSignatureType(nil, nil, []*types.TypeParam{parameter},
		types.NewTuple(types.NewVar(0, nil, "value", array)),
		types.NewTuple(types.NewVar(0, nil, "", outer)), false)
	state := &TypeState{}
	result := state.Substitute(info, signature, map[*types.TypeParam]types.Type{outer: types.Typ[types.Int]}, nil).(*types.Signature)
	if result.TypeParams().Len() != 1 || result.TypeParams().At(0) == parameter {
		t.Fatal("nested generic signature did not retain a fresh local binder")
	}
	pattern, ok := state.ArrayOf(info, result.Params().At(0).Type())
	if !ok || pattern.Elem != result.TypeParams().At(0) || pattern.Length.Param != length || result.Results().At(0).Type() != types.Typ[types.Int] {
		t.Fatalf("nested substitution = %v, array = %#v", result, pattern)
	}
	if len(info.ArrayPatterns) != 1 || signature.TypeParams().At(0) != parameter {
		t.Fatal("nested signature substitution changed checked data")
	}
}

func TestTypeStateUsesEnumAndBoundDefinitionContexts(t *testing.T) {
	parameter := types.NewTypeParam(types.NewTypeName(0, nil, "T", nil), anyType)
	length := &ConstParam{Name: &ast.Ident{Name: "N"}}
	info := &Info{}
	array := info.ArrayType(parameter, ConstValue{Param: length})
	named := types.NewNamed(types.NewTypeName(0, nil, "Box", nil), types.NewStruct(nil, nil), nil)
	named.SetTypeParams([]*types.TypeParam{parameter})
	variant := &Variant{Fields: []Field{{Name: "payload", Type: array}}}
	instance := &Instance{Enum: &Enum{info: info, Type: named}, Args: []types.Type{types.Typ[types.Int]}}
	state := &TypeState{}
	fields := state.Fields(instance, variant)
	pattern, ok := state.ArrayOf(nil, fields[0].Type)
	if !ok || pattern.Elem != types.Typ[types.Int] || pattern.Length.Param != length {
		t.Fatalf("enum payload substitution = %#v", pattern)
	}
	member := &Member{Signature: types.NewSignatureType(nil, nil, nil, nil,
		types.NewTuple(types.NewVar(0, nil, "", array)), false)}
	bound := &TraitBound{info: info, Trait: &Trait{Self: parameter}, Target: types.Typ[types.String]}
	signature := state.MethodSignature(bound, member)
	pattern, ok = state.ArrayOf(nil, signature.Results().At(0).Type())
	if !ok || pattern.Elem != types.Typ[types.String] || pattern.Length.Param != length {
		t.Fatalf("bound method substitution = %#v", pattern)
	}
	if len(info.ArrayPatterns) != 1 || variant.Fields[0].Type != array || member.Signature.Results().At(0).Type() != array {
		t.Fatal("hidden definition context was changed")
	}
}
