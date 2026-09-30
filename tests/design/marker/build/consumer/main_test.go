package consumer

import (
	"example.org/consumer/contracts"
	native "example.org/native/buffer"
	"testing"
)

func TestDeclareKeepsOriginal(t *testing.T) {
	b := &native.Buffer{Data: []byte("abc")}
	var r contracts.Reader = b.AsByteReader()
	if got, ok := r.(*native.Buffer); !ok || got != b {
		t.Fatal("identity lost")
	}
	var direct contracts.Reader = b
	if direct != r {
		t.Fatal("not same receiver")
	}
	p := make([]byte, 3)
	if n, err := r.Read(p); n != 3 || err != nil || string(p) != "abc" {
		t.Fatal(n, err, string(p))
	}
}
