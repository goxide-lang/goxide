package views

import "testing"

func TestSeparateMethodsShareOriginal(t *testing.T) {
	b := NewBuffer([]byte("abc"))
	bytes, text := b.AsByteReader(), b.AsTextReader()
	p := make([]byte, 3)
	if n, err := bytes.Read(p); n != 3 || err != nil || string(p) != "abc" {
		t.Fatal(n, err, string(p))
	}
	if got := text.Read(); got != "abc" {
		t.Fatal(got)
	}
	b.data[0] = 'z'
	if got := text.Read(); got != "zbc" {
		t.Fatal(got)
	}
	if _, err := bytes.Read(p); err != nil || string(p) != "zbc" {
		t.Fatal(err, string(p))
	}
	if _, ok := any(b).(interface{ Read([]byte) (int, error) }); ok {
		t.Fatal("raw Buffer must not expose trait method")
	}
}
func TestNilFactoryRejected(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil factory accepted")
		}
	}()
	var b *Buffer
	b.AsByteReader()
}
