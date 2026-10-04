package serde

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestErrorPathAndCause(t *testing.T) {
	sentinel := errors.New("decoder cause")
	leaf := &Error{Kind: Range, Message: "integer out of range", Cause: sentinel}
	first := AtIndex(leaf, 3)
	outer := AtField(first, "a.b/\"c")
	var got *Error
	if !errors.As(outer, &got) || !errors.Is(outer, sentinel) || !errors.Is(outer, leaf) {
		t.Fatal("lost error chain")
	}
	want := []PathSegment{{Kind: "Field", Name: "a.b/\"c"}, {Kind: "Index", Index: 3}}
	if !reflect.DeepEqual(got.Path, want) {
		t.Fatalf("path: %#v", got.Path)
	}
	if len(leaf.Path) != 0 || len(first.(*Error).Path) != 1 {
		t.Fatal("mutated input error")
	}
	if got.Kind != Range || !strings.Contains(got.Error(), `$["a.b/\"c"][3]`) {
		t.Fatal(got)
	}
	got.Path[1].Index = 8
	if first.(*Error).Path[0].Index != 3 {
		t.Fatal("shared path backing array")
	}
	if AtField(nil, "x") != nil || AtIndex(nil, 0) != nil {
		t.Fatal("wrapped nil")
	}
	wrapped := AtVariant(AtMapKey(fmt.Errorf("context: %w", leaf), "key"), "V")
	if !errors.Is(wrapped, sentinel) || !strings.Contains(wrapped.Error(), "context") {
		t.Fatal(wrapped)
	}
}

func TestVisitorDefaultsAndCallbackErrors(t *testing.T) {
	v := VisitorFuncs{}
	calls := []func() error{
		func() error { return v.VisitBool(false) }, func() error { return v.VisitInt64(0) },
		func() error { return v.VisitUint64(0) }, func() error { return v.VisitFloat64(0) },
		func() error { return v.VisitString("") }, func() error { return v.VisitBytes(nil) },
		v.VisitNone, func() error { return v.VisitSome(nil) }, func() error { return v.VisitSeq(nil) },
		func() error { return v.VisitMap(nil) }, func() error { return v.VisitEnum(nil) },
	}
	for _, call := range calls {
		var e *Error
		if err := call(); !errors.As(err, &e) || e.Kind != Type {
			t.Fatalf("expected Type: %v", err)
		}
	}
	sentinel := errors.New("visitor cause")
	v.OnNone = func() error { return sentinel }
	if v.VisitNone() != sentinel {
		t.Fatal("lost callback error")
	}
}

// Embedding only supplies the unused methods; each test exercises its chosen
// protocol entry rather than using a particular format backend.
type scalarDecoder struct {
	Deserializer
	run func(Visitor) error
}

func (d scalarDecoder) Bool(v Visitor) error    { return d.run(v) }
func (d scalarDecoder) Int64(v Visitor) error   { return d.run(v) }
func (d scalarDecoder) Uint64(v Visitor) error  { return d.run(v) }
func (d scalarDecoder) Float64(v Visitor) error { return d.run(v) }
func (d scalarDecoder) String(v Visitor) error  { return d.run(v) }
func (d scalarDecoder) Bytes(v Visitor) error   { return d.run(v) }

func TestScalarReaders(t *testing.T) {
	d := func(f func(Visitor) error) Deserializer { return scalarDecoder{run: f} }
	if v, e := ReadBool(d(func(v Visitor) error { return v.VisitBool(true) })); e != nil || !v {
		t.Fatal(v, e)
	}
	if v, e := ReadInt64(d(func(v Visitor) error { return v.VisitInt64(-9223372036854775808) })); e != nil || v != -9223372036854775808 {
		t.Fatal(v, e)
	}
	if v, e := ReadUint64(d(func(v Visitor) error { return v.VisitUint64(18446744073709551615) })); e != nil || v != 18446744073709551615 {
		t.Fatal(v, e)
	}
	if v, e := ReadFloat64(d(func(v Visitor) error { return v.VisitFloat64(1.25) })); e != nil || v != 1.25 {
		t.Fatal(v, e)
	}
	if v, e := ReadString(d(func(v Visitor) error { return v.VisitString("é") })); e != nil || v != "é" {
		t.Fatal(v, e)
	}
	source := []byte{1, 2, 3}
	result, e := ReadBytes(d(func(v Visitor) error { return v.VisitBytes(source) }))
	if e != nil || !reflect.DeepEqual(source, result) {
		t.Fatal(result, e)
	}
	source[0] = 9
	if result[0] != 1 {
		t.Fatal("bytes alias input")
	}
	if _, e = ReadInt64(d(func(v Visitor) error { return v.VisitString("1") })); e == nil {
		t.Fatal("coerced string to integer")
	}
	if _, e = ReadInt64(d(func(v Visitor) error { return v.VisitNone() })); e == nil {
		t.Fatal("accepted null integer")
	}
}

func TestScalarProtocolFailures(t *testing.T) {
	sentinel := errors.New("trailing input error")
	cases := []struct {
		name  string
		run   func(Visitor) error
		kind  string
		cause error
	}{
		{"no visit", func(v Visitor) error { return nil }, State, nil},
		{"twice", func(v Visitor) error { _ = v.VisitInt64(1); return v.VisitInt64(2) }, State, nil},
		{"ignored callback error", func(v Visitor) error { _ = v.VisitInt64(1); _ = v.VisitInt64(2); return nil }, State, nil},
		{"post visit error", func(v Visitor) error { _ = v.VisitInt64(7); return sentinel }, "", sentinel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, err := ReadInt64(scalarDecoder{run: tc.run})
			if err == nil || value != 0 {
				t.Fatalf("partial success: %v %v", value, err)
			}
			if tc.cause != nil {
				if !errors.Is(err, tc.cause) {
					t.Fatal(err)
				}
			} else {
				var e *Error
				if !errors.As(err, &e) || e.Kind != tc.kind {
					t.Fatal(err)
				}
			}
		})
	}
}
