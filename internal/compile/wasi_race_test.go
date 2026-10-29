//go:build race

package compile

func init() {
	// Keep actual macro integrations under race instrumentation without
	// applying production wall-clock budgets to the instrumentation overhead.
	wasiTestBudgetMultiplier = 20
}
