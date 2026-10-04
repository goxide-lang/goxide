package serdejson

import (
	"bytes"
	"github.com/goxide-lang/serde"
	"math"
	"strings"
	"testing"
)

func TestScalarsAndStrictNumbers(t *testing.T) {
	for _, s := range []string{"18446744073709551615", "9007199254740993"} {
		var n uint64
		if e := Decode([]byte(s), func(d serde.Deserializer) error { var e error; n, e = serde.ReadUint64(d); return e }); e != nil {
			t.Fatal(e)
		}
		b, e := Encode(func(s serde.Serializer) error { return s.Uint64(n) })
		if e != nil || string(b) != s {
			t.Fatalf("%s %v", b, e)
		}
	}
	for _, s := range []string{"-1", "18446744073709551616", "1.0", "1e0", "null"} {
		if e := Decode([]byte(s), func(d serde.Deserializer) error { _, e := serde.ReadUint64(d); return e }); e == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"1e999", "1e-999"} {
		if e := Decode([]byte(s), func(d serde.Deserializer) error { _, e := serde.ReadFloat64(d); return e }); e == nil {
			t.Fatal(s)
		}
	}
	if e := Decode([]byte("-0.0"), func(d serde.Deserializer) error {
		x, e := serde.ReadFloat64(d)
		if !math.Signbit(x) {
			t.Fatal("lost negative zero")
		}
		return e
	}); e != nil {
		t.Fatal(e)
	}
	for _, x := range []float64{math.Inf(1), math.NaN()} {
		if _, e := Encode(func(s serde.Serializer) error { return s.Float64(x) }); e == nil {
			t.Fatal("nonfinite accepted")
		}
	}
}
func TestStrictSyntaxEvenSkipped(t *testing.T) {
	for _, s := range []string{`{"a":1,"\u0061":2}`, `{"unknown":{"a":1,"a":2}}`, `"\ud800"`, `"\udc00"`, `"\ud800\u0041"`, `"` + string([]byte{255}) + `"`, `1 2`, `[1,]`, `{"a":}`, `"\x"`, "", strings.Repeat("[", MaxDepth+2) + "0" + strings.Repeat("]", MaxDepth+2)} {
		if e := Decode([]byte(s), func(d serde.Deserializer) error { return d.Skip() }); e == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{`"\ud83d\ude00"`, `{"a":[null,1e999,true,"x"]}`, `"escaped\\ud800"`} {
		if e := Decode([]byte(s), func(d serde.Deserializer) error { return d.Skip() }); e != nil {
			t.Fatalf("%s: %v", s, e)
		}
	}
}
func TestBytesAndPaths(t *testing.T) {
	b, e := Encode(func(s serde.Serializer) error { return s.Bytes([]byte{0, 128, 255}) })
	if e != nil || string(b) != "[0,128,255]" {
		t.Fatalf("%s %v", b, e)
	}
	var v []byte
	e = Decode(b, func(d serde.Deserializer) error { var e error; v, e = serde.ReadBytes(d); return e })
	if e != nil || !bytes.Equal(v, []byte{0, 128, 255}) {
		t.Fatal(v, e)
	}
	e = Decode([]byte(`[0,256]`), func(d serde.Deserializer) error { _, e := serde.ReadBytes(d); return e })
	if e == nil || !strings.Contains(e.Error(), "[1]") {
		t.Fatal(e)
	}
}
func TestCallbacksAndStates(t *testing.T) {
	calls := 0
	b, e := Encode(func(s serde.Serializer) error {
		c, e := s.Struct("Demo", 1)
		if e != nil {
			return e
		}
		if e = c.Field("x", func(s serde.Serializer) error { calls++; return s.String("yes") }); e != nil {
			return e
		}
		return c.End()
	})
	if e != nil || calls != 1 || string(b) != `{"x":"yes"}` {
		t.Fatal(string(b), calls, e)
	}
	badWrites := []serde.WriteValue{func(s serde.Serializer) error { return nil }, func(s serde.Serializer) error { _, e := s.Seq(0); return e }, func(s serde.Serializer) error {
		if e := s.None(); e != nil {
			return e
		}
		return s.None()
	}, func(s serde.Serializer) error { c, _ := s.Seq(1); return c.End() }, func(s serde.Serializer) error {
		c, _ := s.Map(1)
		return c.Entry(func(s serde.Serializer) error { return s.Int64(3) }, func(s serde.Serializer) error { return s.None() })
	}}
	for i, f := range badWrites {
		if _, e := Encode(f); e == nil {
			t.Fatal("accepted write", i)
		}
	}
	if e := Decode([]byte(`{"x":1}`), func(d serde.Deserializer) error {
		return d.Map(serde.VisitorFuncs{OnMap: func(a serde.MapAccess) error {
			_, e := a.NextKey(func(d serde.Deserializer) error { return d.Skip() })
			if e != nil {
				return e
			}
			_, e = a.NextKey(func(d serde.Deserializer) error { return d.Skip() })
			return e
		}})
	}); e == nil {
		t.Fatal("accepted missing value")
	}
	if e := Decode([]byte(`[1]`), func(d serde.Deserializer) error {
		return d.Seq(serde.VisitorFuncs{OnSeq: func(a serde.SeqAccess) error { return nil }})
	}); e == nil {
		t.Fatal("accepted unread sequence")
	}
}
func TestEnums(t *testing.T) {
	for _, unit := range []bool{true, false} {
		b, e := Encode(func(s serde.Serializer) error {
			if unit {
				return s.UnitVariant("E", "Idle")
			}
			return s.NewtypeVariant("E", "Count", func(s serde.Serializer) error { return s.Int64(7) })
		})
		if e != nil {
			t.Fatal(e)
		}
		e = Decode(b, func(d serde.Deserializer) error {
			return d.Enum("E", []string{"Idle", "Count"}, serde.VisitorFuncs{OnEnum: func(a serde.EnumAccess) error {
				n, v, e := a.Variant()
				if e != nil {
					return e
				}
				if n == "Idle" {
					return v.Unit()
				}
				return v.Newtype(func(d serde.Deserializer) error {
					n, e := serde.ReadInt64(d)
					if n != 7 {
						t.Fatal(n)
					}
					return e
				})
			}})
		})
		if e != nil {
			t.Fatal(string(b), e)
		}
	}
}

func TestIgnoredProtocolErrorsRemainErrors(t *testing.T) {
	for _, f := range []serde.WriteValue{
		func(s serde.Serializer) error {
			c, _ := s.Struct("X", 1)
			_ = c.Field("x", func(s serde.Serializer) error { return nil })
			return c.End()
		},
		func(s serde.Serializer) error {
			c, _ := s.Seq(1)
			_ = c.Element(func(s serde.Serializer) error { return nil })
			return c.End()
		},
		func(s serde.Serializer) error { _ = s.Bool(true); _ = s.None(); return nil },
		func(s serde.Serializer) error {
			c, _ := s.Seq(1)
			_ = c.Element(func(s serde.Serializer) error { _ = c.End(); return s.None() })
			return c.End()
		},
	} {
		if b, e := Encode(f); e == nil {
			t.Fatalf("ignored failure accepted: %s", b)
		}
	}
	e := Decode([]byte(`[false]`), func(d serde.Deserializer) error {
		return d.Seq(serde.VisitorFuncs{OnSeq: func(a serde.SeqAccess) error {
			_, _ = a.NextElement(func(d serde.Deserializer) error { _, e := serde.ReadInt64(d); return e })
			return nil
		}})
	})
	if e == nil {
		t.Fatal("ignored read error accepted")
	}
}
