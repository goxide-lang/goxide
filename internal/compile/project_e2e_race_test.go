//go:build race

package compile_test

func init() {
	// This external test package owns its macro host independently of compile.
	// Ordinary P5 runs retain the production invocation and cumulative budgets.
	p5WASIBudgetMultiplier = 20
}
