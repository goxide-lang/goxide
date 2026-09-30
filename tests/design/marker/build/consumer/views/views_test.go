package views

import "testing"

func TestCustomViewsShareOriginal(t *testing.T) {
	b := NewBuffer([]byte("abc"))
	bytes, text := b.AsByteReader(), b.AsTextReader()
	b.data[0] = 'z'
	p := make([]byte, 3)
	if _, err := bytes.Read(p); err != nil || string(p) != "zbc" {
		t.Fatal(err, string(p))
	}
	if text.Read() != "zbc" {
		t.Fatal(text.Read())
	}
	if _, ok := any(b).(ByteReader); ok {
		t.Fatal("custom trait marker leaked onto raw type")
	}
}
