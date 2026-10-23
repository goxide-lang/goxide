//go:build race && !js

package procmacro

func init() {
	// Instrumented WASM compilation can be twenty times slower. Explicit
	// timeout and cancellation cases retain their own unscaled deadlines.
	wasiTestBudgetMultiplier = 20
}
