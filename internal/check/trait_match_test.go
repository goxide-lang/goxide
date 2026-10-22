package check

import (
	"go/types"
	"testing"
)

func TestMatchApplicationSourceHead(t *testing.T) {
	x, y := implMatchParam("X"), implMatchParam("Y")
	integer, text := types.Typ[types.Int], types.Typ[types.String]
	pair := implMatchNamed("Pair", 2)
	pairOf := func(a, b types.Type) types.Type { return implMatchInstance(t, pair, a, b) }
	alias := types.NewAlias(types.NewTypeName(0, nil, "Integer", nil), integer)
	namedSlice := types.NewNamed(types.NewTypeName(0, nil, "Slice", nil), types.NewSlice(integer), nil)
	otherPair := implMatchNamed("OtherPair", 2)
	sameNamePair := implMatchNamed("Pair", 2)
	structure := func(field string, typ types.Type, tag string) types.Type {
		return types.NewStruct([]*types.Var{types.NewField(0, nil, field, typ, false)}, []string{tag})
	}
	function := func(typ types.Type) types.Type {
		return types.NewSignatureType(nil, nil, nil, types.NewTuple(types.NewVar(0, nil, "", typ)), nil, false)
	}
	methodSet := func(typ types.Type) types.Type {
		return types.NewInterfaceType([]*types.Func{types.NewFunc(0, nil, "Accept", function(typ).(*types.Signature))}, nil).Complete()
	}
	tests := []struct {
		name            string
		pattern, target types.Type
		parameters      []*types.TypeParam
		want            map[*types.TypeParam]types.Type
	}{
		{"concrete", integer, integer, nil, map[*types.TypeParam]types.Type{}},
		{"slice", types.NewSlice(x), types.NewSlice(integer), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: integer}},
		{"pointer", types.NewPointer(x), types.NewPointer(integer), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: integer}},
		{"map", types.NewMap(x, y), types.NewMap(text, integer), []*types.TypeParam{x, y}, map[*types.TypeParam]types.Type{x: text, y: integer}},
		{"array", types.NewArray(x, 3), types.NewArray(integer, 3), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: integer}},
		{"array length", types.NewArray(x, 3), types.NewArray(integer, 4), []*types.TypeParam{x}, nil},
		{"channel", types.NewChan(types.RecvOnly, x), types.NewChan(types.RecvOnly, integer), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: integer}},
		{"channel direction", types.NewChan(types.RecvOnly, x), types.NewChan(types.SendRecv, integer), []*types.TypeParam{x}, nil},
		{"repeated equal", pairOf(x, x), pairOf(integer, alias), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: integer}},
		{"repeated unequal", pairOf(x, x), pairOf(integer, text), []*types.TypeParam{x}, nil},
		{"named origin", pairOf(x, y), implMatchInstance(t, otherPair, integer, text), []*types.TypeParam{x, y}, nil},
		{"same name distinct origin", pairOf(x, y), implMatchInstance(t, sameNamePair, integer, text), []*types.TypeParam{x, y}, nil},
		{"named underlying", types.NewSlice(x), namedSlice, []*types.TypeParam{x}, nil},
		{"named to underlying", namedSlice, types.NewSlice(integer), nil, nil},
		{"unconstrained parameter", types.NewSlice(x), types.NewSlice(integer), []*types.TypeParam{x, y}, nil},
		{"target parameter", types.NewSlice(x), types.NewSlice(y), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: y}},
		{"rigid parameter", types.NewSlice(x), types.NewSlice(integer), nil, nil},
		{"identical parameter", types.NewSlice(x), types.NewSlice(x), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: x}},
		{"alias", types.NewAlias(types.NewTypeName(0, nil, "Pattern", nil), types.NewSlice(x)), types.NewSlice(alias), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: integer}},
		{"struct", structure("Value", x, "tag"), structure("Value", integer, "tag"), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: integer}},
		{"struct field", structure("Value", x, "tag"), structure("Other", integer, "tag"), []*types.TypeParam{x}, nil},
		{"struct tag", structure("Value", x, "tag"), structure("Value", integer, "other"), []*types.TypeParam{x}, nil},
		{"function", function(x), function(integer), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: integer}},
		{"interface", methodSet(x), methodSet(integer), []*types.TypeParam{x}, map[*types.TypeParam]types.Type{x: integer}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := &checker{out: &Info{}}
			got, _, _, ok := c.matchApplicationHead(&Impl{Target: test.pattern, TypeParams: test.parameters}, &TraitBound{Target: test.target})
			if ok != (test.want != nil) {
				t.Fatalf("matchApplicationHead(%s, %s) = %v, %v; want %v", test.pattern, test.target, got, ok, test.want)
			}
			if len(got) != len(test.want) {
				t.Fatalf("bindings = %v, want %v", got, test.want)
			}
			for parameter, want := range test.want {
				if !types.Identical(got[parameter], want) {
					t.Errorf("binding for %s = %v, want %s", parameter, got[parameter], want)
				}
			}
		})
	}
}

func TestCoherenceHeadUnification(t *testing.T) {
	x, y := implMatchParam("X"), implMatchParam("Y")
	integer, text := types.Typ[types.Int], types.Typ[types.String]
	pair := implMatchNamed("Pair", 2)
	pairOf := func(a, b types.Type) types.Type { return implMatchInstance(t, pair, a, b) }
	box := implMatchNamed("Box", 1)
	boxOf := func(typ types.Type) types.Type { return implMatchInstance(t, box, typ) }
	otherBox := implMatchNamed("OtherBox", 1)
	tests := []struct {
		name                    string
		left, right             types.Type
		leftParams, rightParams []*types.TypeParam
		want                    bool
	}{
		{"generic concrete", boxOf(x), boxOf(integer), []*types.TypeParam{x}, nil, true},
		{"generic generic", boxOf(x), boxOf(y), []*types.TypeParam{x}, []*types.TypeParam{y}, true},
		{"concrete distinct", boxOf(integer), boxOf(text), nil, nil, false},
		{"repeated mismatch", pairOf(x, x), pairOf(integer, text), []*types.TypeParam{x}, nil, false},
		{"both substitutions", pairOf(x, integer), pairOf(text, y), []*types.TypeParam{x}, []*types.TypeParam{y}, true},
		{"nested substitutions", pairOf(x, types.NewSlice(x)), pairOf(types.NewPointer(y), types.NewSlice(types.NewPointer(integer))), []*types.TypeParam{x}, []*types.TypeParam{y}, true},
		{"named isolation", boxOf(x), implMatchInstance(t, otherBox, integer), []*types.TypeParam{x}, nil, false},
		{"array lengths", types.NewArray(x, 3), types.NewArray(y, 4), []*types.TypeParam{x}, []*types.TypeParam{y}, false},
		{"channel directions", types.NewChan(types.SendOnly, x), types.NewChan(types.RecvOnly, y), []*types.TypeParam{x}, []*types.TypeParam{y}, false},
		{"map", types.NewMap(x, integer), types.NewMap(text, y), []*types.TypeParam{x}, []*types.TypeParam{y}, true},
		{"rigid parameter", boxOf(x), boxOf(integer), nil, nil, false},
		{"occurs directly", x, types.NewSlice(x), []*types.TypeParam{x}, nil, false},
		{"occurs through binding", pairOf(x, x), pairOf(y, types.NewSlice(y)), []*types.TypeParam{x}, []*types.TypeParam{y}, false},
		{"occurs through both bindings", pairOf(x, types.NewSlice(x)), pairOf(types.NewSlice(y), y), []*types.TypeParam{x}, []*types.TypeParam{y}, false},
		{"occurs named argument", pairOf(x, x), pairOf(y, boxOf(y)), []*types.TypeParam{x}, []*types.TypeParam{y}, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := unifyApplicationHeads(test.left, test.right, test.leftParams, test.rightParams); got != test.want {
				t.Fatalf("unifyApplicationHeads(%s, %s) = %v, want %v", test.left, test.right, got, test.want)
			}
			if got := unifyApplicationHeads(test.right, test.left, test.rightParams, test.leftParams); got != test.want {
				t.Fatalf("reverse unifyApplicationHeads(%s, %s) = %v, want %v", test.right, test.left, got, test.want)
			}
		})
	}
}

func implMatchParam(name string) *types.TypeParam {
	return types.NewTypeParam(types.NewTypeName(0, nil, name, nil), types.NewInterfaceType(nil, nil).Complete())
}

func implMatchNamed(name string, arity int) *types.Named {
	named := types.NewNamed(types.NewTypeName(0, nil, name, nil), types.NewStruct(nil, nil), nil)
	parameters := make([]*types.TypeParam, arity)
	for i := range parameters {
		parameters[i] = implMatchParam(string(rune('A' + i)))
	}
	named.SetTypeParams(parameters)
	return named
}

func implMatchInstance(t *testing.T, named *types.Named, arguments ...types.Type) types.Type {
	t.Helper()
	instance, err := types.Instantiate(nil, named, arguments, false)
	if err != nil {
		t.Fatal(err)
	}
	return instance
}

// Both heads share the same substitution store used by coherence conditions.
func unifyApplicationHeads(left, right types.Type, leftParams, rightParams []*types.TypeParam) bool {
	c := &checker{out: &Info{}}
	state := newCoherenceConstraints(c)
	state.addParameters(leftParams, nil)
	state.addParameters(rightParams, nil)
	return state.unify(state.source(left), state.source(right)) && !state.exhausted && !state.uncertain
}
