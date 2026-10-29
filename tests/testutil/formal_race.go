//go:build race

package testutil

func init() {
	// Instrumented WASI execution validates behavior and races; ordinary tests
	// retain the production invocation and compilation budgets.
	formalWASIBudgetMultiplier = 20
}
