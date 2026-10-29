//go:build race

package compile_test

const p5Race = true

// This external test package owns its macro host independently of compile.
const p5WASIBudgetMultiplier = 20
