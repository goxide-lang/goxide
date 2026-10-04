package macros

import (
	"strings"
	"testing"
)

func TestGoResultExpansionBudget(t *testing.T) {
	// Fewer than the expansion-count limit and below the input token budget;
	// specialized adapters themselves must still count toward output limits.
	source := "package p;func F(){" + strings.Repeat("_=go_result_keep!(int, f());", 3500) + "}"
	_, err := Expand("budget.gox", []byte(source))
	if err == nil || !strings.Contains(err.Error(), "token budget") {
		t.Fatalf("adapter output escaped token budget: %v", err)
	}
}
