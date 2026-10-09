package paths

import (
	"reflect"
	"testing"
)

func TestParsePreservesStaticPathSegments(t *testing.T) {
	p, err := Parse("wire /* owner */ :: Serialize\n")
	if err != nil || !reflect.DeepEqual(p.Names(), []string{"wire", "Serialize"}) {
		t.Fatalf("%v, %v", p, err)
	}
	for _, source := range []string{"wire.Serialize", "wire::", "wire\nSerialize", "wire::Serialize;", "wire::Serialize()", "_::Serialize", "use::Serialize"} {
		if _, err := Parse(source); err == nil {
			t.Errorf("accepted invalid path %q", source)
		}
	}
}

func TestGoViewDoesNotRewriteStringsOrComments(t *testing.T) {
	source := []byte("p::T /* x::y */ {S: \"a::b\", R: `c::d`} // e::f\n")
	got := string(GoView(source))
	want := "p. T /* x::y */ {S: \"a::b\", R: `c::d`} // e::f\n"
	if got != want || len(got) != len(source) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
