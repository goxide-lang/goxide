package serdejson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/goxide-lang/serde"
)

func TestBackendSyntaxCauses(t *testing.T) {
	skip := func(d serde.Deserializer) error { return d.Skip() }
	enum := func(d serde.Deserializer) error {
		return d.Enum("Event", []string{"Item"}, serde.VisitorFuncs{OnEnum: func(a serde.EnumAccess) error {
			_, v, e := a.Variant()
			if e != nil {
				return e
			}
			return v.Newtype(func(d serde.Deserializer) error { _, e := serde.ReadInt64(d); return e })
		}})
	}
	for _, tc := range []struct {
		name, input string
		read        serde.ReadValue
	}{
		{"token", "@", skip}, {"sequence-end", "[1}", skip}, {"map-key", `{1:2}`, skip},
		{"map-end", `{"a":1]`, skip}, {"tail", "1 @", skip},
		{"enum-key", `{@`, enum}, {"enum-end", `{"Item":1]`, enum},
		{"token-eof", "", skip},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Match the same Token API, not Unmarshal, whose offsets may differ.
			native := json.NewDecoder(bytes.NewBufferString(tc.input))
			native.UseNumber()
			var original error
			for original == nil {
				_, original = native.Token()
			}
			got := Decode([]byte(tc.input), tc.read)
			var detail *serde.Error
			if !errors.As(got, &detail) || detail.Kind != serde.Syntax {
				t.Fatalf("%v", got)
			}
			var wantSyntax, gotSyntax *json.SyntaxError
			if errors.As(original, &wantSyntax) {
				if !errors.As(got, &gotSyntax) || gotSyntax.Offset != wantSyntax.Offset || gotSyntax.Error() != wantSyntax.Error() {
					t.Fatalf("native=%v wrapped=%v", original, got)
				}
			} else if !errors.Is(original, io.EOF) && !errors.Is(original, io.ErrUnexpectedEOF) {
				t.Fatalf("unexpected native error: %v", original)
			} else if !errors.Is(got, original) {
				t.Fatalf("native=%v wrapped=%v", original, got)
			}
		})
	}
}

func TestBackendNumericCauses(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		read        serde.ReadValue
		cause       error
	}{
		{"signed-range", "9223372036854775808", func(d serde.Deserializer) error { _, e := serde.ReadInt64(d); return e }, strconv.ErrRange},
		{"unsigned-range", "18446744073709551616", func(d serde.Deserializer) error { _, e := serde.ReadUint64(d); return e }, strconv.ErrRange},
		{"float-range", "1e999", func(d serde.Deserializer) error { _, e := serde.ReadFloat64(d); return e }, strconv.ErrRange},
		{"integer-syntax", "1.0", func(d serde.Deserializer) error { _, e := serde.ReadInt64(d); return e }, strconv.ErrSyntax},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Decode([]byte(tc.input), tc.read)
			var detail *serde.Error
			var native *strconv.NumError
			if !errors.As(got, &detail) || detail.Kind != serde.Range || !errors.As(got, &native) || native.Num != tc.input || !errors.Is(got, tc.cause) {
				t.Fatalf("%v", got)
			}
		})
	}
}

func TestBackendChecksDoNotInventCauses(t *testing.T) {
	for _, tc := range []struct {
		input string
		read  serde.ReadValue
	}{
		{"1 2", func(d serde.Deserializer) error { return d.Skip() }},
		{`"\ud800"`, func(d serde.Deserializer) error { return d.Skip() }},
		{"1e-999", func(d serde.Deserializer) error { _, e := serde.ReadFloat64(d); return e }},
		{"true", func(d serde.Deserializer) error { _, e := serde.ReadInt64(d); return e }},
	} {
		got := Decode([]byte(tc.input), tc.read)
		if got == nil || errors.Unwrap(got) != nil {
			t.Fatalf("self-detected %s: %v cause=%v", tc.input, got, errors.Unwrap(got))
		}
	}
}

func TestUnicodeParseCauseIsReal(t *testing.T) {
	for _, input := range []string{`"\uZZZZ"`, `"\ud800\uZZZZ"`} {
		got := Decode([]byte(input), func(d serde.Deserializer) error { return d.Skip() })
		var detail *serde.Error
		var native *strconv.NumError
		if !errors.As(got, &detail) || detail.Kind != serde.Syntax || !errors.As(got, &native) || !errors.Is(got, strconv.ErrSyntax) {
			t.Fatalf("%s: %v", input, got)
		}
	}
}
