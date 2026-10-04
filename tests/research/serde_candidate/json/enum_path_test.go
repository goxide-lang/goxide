package serdejson

import (
	"errors"
	"github.com/goxide-lang/serde"
	"testing"
)

func TestVariantPayloadPaths(t *testing.T) {
	cause := errors.New("payload rejected")
	_, encoded := Encode(func(s serde.Serializer) error {
		return s.NewtypeVariant("Event", "Item", func(serde.Serializer) error { return cause })
	})
	decoded := Decode([]byte(`{"Item":7}`), func(d serde.Deserializer) error {
		return d.Enum("Event", []string{"Item"}, serde.VisitorFuncs{OnEnum: func(a serde.EnumAccess) error {
			_, v, e := a.Variant()
			if e != nil {
				return e
			}
			return v.Newtype(func(serde.Deserializer) error { return cause })
		}})
	})
	for _, e := range []error{encoded, decoded} {
		var se *serde.Error
		if !errors.Is(e, cause) || !errors.As(e, &se) || len(se.Path) != 1 || se.Path[0].Kind != "Variant" || se.Path[0].Name != "Item" {
			t.Fatalf("variant cause/path lost: %#v %v", se, e)
		}
	}
}
