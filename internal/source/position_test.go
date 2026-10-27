package source

import "testing"

func TestUnicodePositionsAndLineEndings(t *testing.T) {
	x := New("a中😀z\r\nβ\r\n\n")
	for _, test := range []struct {
		offset             int
		utf8, utf16, utf32 Position
	}{
		{0, Position{0, 0}, Position{0, 0}, Position{0, 0}},
		{1, Position{0, 1}, Position{0, 1}, Position{0, 1}},
		{4, Position{0, 4}, Position{0, 2}, Position{0, 2}},
		{8, Position{0, 8}, Position{0, 4}, Position{0, 3}},
		{9, Position{0, 9}, Position{0, 5}, Position{0, 4}},
		{11, Position{1, 0}, Position{1, 0}, Position{1, 0}},
		{13, Position{1, 2}, Position{1, 1}, Position{1, 1}},
		{15, Position{2, 0}, Position{2, 0}, Position{2, 0}},
		{16, Position{3, 0}, Position{3, 0}, Position{3, 0}},
	} {
		for encoding, want := range map[Encoding]Position{UTF8: test.utf8, UTF16: test.utf16, UTF32: test.utf32, "": test.utf16} {
			position, err := x.Position(test.offset, encoding)
			if err != nil || position != want {
				t.Fatalf("Position(%d, %q) = %+v, %v; want %+v", test.offset, encoding, position, err, want)
			}
			offset, err := x.Offset(position, encoding)
			if err != nil || offset != test.offset {
				t.Fatalf("Offset(%+v, %q) = %d, %v; want %d", position, encoding, offset, err, test.offset)
			}
		}
	}
	span, err := x.Range(4, 11, UTF16)
	if err != nil || span != (Range{Start: Position{0, 2}, End: Position{1, 0}}) {
		t.Fatalf("multi-line range: %+v, %v", span, err)
	}
	start, end, err := x.Offsets(span, UTF16)
	if err != nil || start != 4 || end != 11 {
		t.Fatalf("range round trip: %d:%d, %v", start, end, err)
	}
}

func TestPositionBoundaries(t *testing.T) {
	x := New("a😀\r\nb\r")
	for _, offset := range []int{-1, 2, 3, 4, 6, x.Len() + 1} {
		if _, err := x.Position(offset, UTF16); err == nil {
			t.Errorf("accepted nonrepresentable byte offset %d", offset)
		}
	}
	for _, test := range []struct {
		position Position
		encoding Encoding
	}{
		{Position{0, 2}, UTF16},
		{Position{0, 2}, UTF8},
		{Position{-1, 0}, UTF16},
		{Position{0, -1}, UTF16},
		{Position{}, Encoding("unknown")},
	} {
		if _, err := x.Offset(test.position, test.encoding); err == nil {
			t.Errorf("accepted invalid position %+v (%s)", test.position, test.encoding)
		}
	}
	for position, want := range map[Position]int{{0, 1000}: 5, {1000, 0}: x.Len(), {2, 0}: x.Len()} {
		if offset, err := x.Offset(position, UTF16); err != nil || offset != want {
			t.Errorf("clamped position %+v = %d, %v; want %d", position, offset, err, want)
		}
	}
	if _, _, err := x.Offsets(Range{Start: Position{1, 0}, End: Position{0, 0}}, UTF16); err == nil {
		t.Fatal("accepted reversed edit range")
	}
	if _, err := x.Range(5, 1, UTF16); err == nil {
		t.Fatal("accepted reversed byte range")
	}
	for _, text := range []string{"", "\n", "\r", "\r\n", "last"} {
		x := New(text)
		position, err := x.Position(x.Len(), UTF16)
		if err != nil {
			t.Fatal(err)
		}
		offset, err := x.Offset(position, UTF16)
		if err != nil || offset != len(text) {
			t.Fatalf("EOF round trip for %q: %d, %v", text, offset, err)
		}
	}
}
