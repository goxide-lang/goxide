package playground

import (
	"strings"
	"testing"
)

func TestBrowserBundleRejectsEnvironmentDependentSource(t *testing.T) {
	_, err := ParseMacroBundle([]byte(`{"source_packages":[{"package":"example.org/library","environment_dependent":true}]}`))
	if err == nil || !strings.Contains(err.Error(), "example.org/library depends on environment variables") {
		t.Fatalf("unreplayable fixed bundle accepted: %v", err)
	}
}
