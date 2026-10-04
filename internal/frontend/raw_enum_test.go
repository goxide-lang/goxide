package frontend

import (
	"strings"
	"testing"
)

func TestRawEnumSyntaxRejected(t *testing.T) {
	for _, decl := range []string{
		`enum Status string { Pending="pending", Paid="paid", }`,
		`enum Code int { A=1, }`,
		`enum Code uint8 { A=255, }`,
		`#[derive(Clone)]
enum Code int { A=1, }`,
	} {
		_, err := Compile("raw.gox", []byte("package p\n"+decl+"\n"))
		if err == nil || !strings.Contains(err.Error(), "raw/C enum underlying types are unsupported") {
			t.Fatalf("raw syntax accepted or unclear diagnostic: %v", err)
		}
	}
	_, err := CompilePackage("example/p", map[string][]byte{
		"defs.gox": []byte("package p\npub enum Status string {Pending=\"pending\",}\n"),
		"uses.gox": []byte("package p\nfunc f(){}\n"),
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "raw/C enum underlying types are unsupported") {
		t.Fatalf("package raw syntax: %v", err)
	}
}
