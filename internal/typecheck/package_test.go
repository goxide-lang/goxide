package typecheck

import (
	"go/importer"
	"strings"
	"testing"
)

func TestCheckPackageFileScopesAndDeclarations(t *testing.T) {
	files := map[string][]byte{
		"model.go": []byte("package model\nimport native \"strings\"\ntype Model struct { Name string }; func normalized(m Model) string { return native.ToUpper(m.Name) }"),
		"use.go":   []byte("package model\nimport native \"strconv\"\nfunc Render(m Model) string { return normalized(m)+native.Itoa(1) }"),
	}
	if err := CheckPackage("example.org/app/model", files, importer.Default()); err != nil {
		t.Fatal(err)
	}
	files["use.go"] = []byte("package model\nfunc Render(m Model) string { return native.ToUpper(m.Name) }")
	if err := CheckPackage("example.org/app/model", files, importer.Default()); err == nil || !strings.Contains(err.Error(), "use.go:2:") || !strings.Contains(err.Error(), "undefined: native") {
		t.Fatalf("file import leaked or location lost: %v", err)
	}
}

func TestCheckPackageRejectsConflicts(t *testing.T) {
	for _, tc := range []struct{ name, other, want string }{
		{"duplicate", "package p\ntype Item int", "redeclared"},
		{"mixed-package", "package other\ntype Other int", "package"},
		{"nominal", "package p\ntype Other int; var x Item = Other(1)", "cannot use"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPackage("example.org/p", map[string][]byte{"a.go": []byte("package p\ntype Item int"), "b.go": []byte(tc.other)}, importer.Default())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q, got %v", tc.want, err)
			}
		})
	}
	if err := CheckPackage("", map[string][]byte{"a.go": []byte("package p")}, nil); err == nil {
		t.Fatal("missing package identity accepted")
	}
	if err := CheckPackage("example.org/p", nil, nil); err == nil {
		t.Fatal("empty package accepted")
	}
}
