package check_test

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestReferenceRecursiveInterfaceCapabilities(t *testing.T) {
	for _, test := range []struct {
		name, source string
		reject       bool
	}{
		{"ordinary-recursive-callback", `type Writer interface{Next()Writer;Write(Callback)error};type Callback func(Writer)error;func erase(value Writer)any{return value}`, false},
		{"readonly-result", `type Reader interface{Next()Reader;Read()&int};func erase(value Reader)any{return value}`, true},
		{"readonly-parameter", `type Visitor interface{Visit(&int);Next()Visitor};func erase(value Visitor)any{return value}`, true},
		{"generic-readonly-result", `type Reader[T any] interface{Next()Reader[T];Read()T};func erase(value Reader[&int])any{return value}`, true},
		{"generic-pointer-result", `type Reader[T any] interface{Next()Reader[T];Read()T};func erase(value Reader[*int])any{return value}`, false},
		{"alias-readonly-result", `type Reader = interface{Read()&int};func erase(value Reader)any{return value}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			files, resolved := parse(t, "package p;"+test.source)
			_, diagnostics := check.Check(files, resolved)
			for _, diagnostic := range diagnostics {
				if diagnostic.Code != check.ReferencePermission {
					t.Fatalf("unexpected diagnostic: %v", diagnostic)
				}
			}
			if (len(diagnostics) != 0) != test.reject {
				t.Fatalf("permission rejection = %v, want %v: %v", len(diagnostics) != 0, test.reject, diagnostics)
			}
		})
	}
}
