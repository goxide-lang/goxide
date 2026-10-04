package plain

import (
	native "example.org/native/buffer"
	"testing"
)

func TestOriginalGoUnchanged(t *testing.T) {
	b := &native.Buffer{Data: []byte("abc")}
	p := make([]byte, 3)
	if _, err := b.Read(p); err != nil || string(p) != "abc" {
		t.Fatal(err, string(p))
	}
	if _, ok := any(b).(interface{ GoxideTraitByteReader() }); ok {
		t.Fatal("marker leaked into original")
	}
}
