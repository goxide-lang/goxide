package lower_test

import (
	"strings"
	"testing"
)

func TestEqualityPreservesArgumentOrderAcrossPropagation(t *testing.T) {
	runDeriveFixture(t, "equality_flow", "true [left right] false [left right] true true 7\n")
}

func TestEqualsRequiresExplicitNamedProtocol(t *testing.T) {
	for _, expression := range []string{"([]int{}).equals([]int{})", "Option[int]::None.equals(Option[int]::None)", "Plain(1).equals(Plain(1))"} {
		_, err := lowerDeriveSources(map[string]string{"main.gox": "package main\ntype Plain int\nfunc main() { _ = " + expression + " }\n"})
		if err == nil || !strings.Contains(err.Error(), "explicitly registered PartialEq") {
			t.Fatalf("%s: expected explicit protocol diagnostic, got %v", expression, err)
		}
	}
}
