package enums

import (
	"strings"
	"testing"
)

func TestProtocolGenericAdmissionUsesDeclarationRegistry(t *testing.T) {
	source := []byte("package p;func F[T interface{}](value T)T{return value}")
	if _, err := Lower("a.gox", source); err == nil || !strings.Contains(err.Error(), "user generic functions unsupported") {
		t.Fatalf("unregistered generic accepted: %v", err)
	}
	if _, err := LowerPackageWithGenericFunctions("a.gox", source, nil, nil, nil, map[string]bool{"Other": true}); err == nil {
		t.Fatal("unrelated registered declaration authorized F")
	}
	if _, err := LowerPackageWithGenericFunctions("a.gox", source, nil, nil, nil, map[string]bool{"F": true}); err != nil {
		t.Fatal(err)
	}
}
