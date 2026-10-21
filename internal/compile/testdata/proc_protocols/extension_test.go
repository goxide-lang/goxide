package main

import (
	model "example.org/library-protocol-model"
	"reflect"
	"testing"
)

func TestExtendedLibraryProtocols(t *testing.T) {
	model.CheckExtended()
	if got := model.DefaultCases(); got != "1 2 2 true true" {
		t.Fatalf("default cases = %q", got)
	}
	want := []string{
		`State::Empty`,
		`State::Pair(Piece { Text: "quoted\n\"", Values: [1, 2] }, [])`,
		`State::Named { Name: "quoted\n\"", Count: 3 }`,
		`Option::None`,
		`Option::Some(nil)`,
		`Option::Some([])`,
		`Result::Ok([Piece { Text: "quoted\n\"", Values: [1, 2] }])`,
		`Result::Err({"a\n": [Piece { Text: "quoted\n\"", Values: [1, 2] }], "b": []})`,
		`Option::Some(Result::Err("nested\n"))`,
		`nil`,
		`{}`,
		`{"a\n": "first\"", "z": "last"}`,
	}
	for i := 0; i < 10; i++ {
		if got := model.DebugCases(); !reflect.DeepEqual(got, want) {
			t.Fatalf("debug pass %d\ngot: %#v\nwant:%#v", i, got, want)
		}
	}
	for name, run := range map[string]func(){
		"nil-enum-clone":       func() { _ = model.CloneState(nil) },
		"typed-nil-enum-clone": func() { var value *model.Pair; _ = model.CloneState(value) },
		"typed-nil-enum-debug": func() { var value *model.Pair; _ = model.DebugState(value) },
		"nil-enum-debug":       func() { _ = model.DebugState(nil) },
		"invalid-result-clone": model.InvalidResultClone,
		"invalid-result-debug": model.InvalidResultDebug,
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid value accepted")
				}
			}()
			run()
		})
	}
}
