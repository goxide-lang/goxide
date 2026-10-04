package frontend

import (
	"bytes"
	"testing"
)

func TestScalarEqualitySingleFileSpacing(t *testing.T) {
	r, err := Compile("spacing.gox", []byte("package p\nfunc F()bool{a:=1;return a . equals(1)}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(r.Go, []byte("__goxide_enum_equals[")) || bytes.Contains(r.Go, []byte("__goxide_enum_equals(")) {
		t.Fatal("unlowered equality intrinsic")
	}
	if !bytes.Contains(r.Go, []byte("__goxide_enum_scalar_equals")) {
		t.Fatal("missing typed scalar comparison")
	}
}
