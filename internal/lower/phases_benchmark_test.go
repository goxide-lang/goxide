package lower_test

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/source"
)

// BenchmarkCompilerPhases measures the shared frontend separately from Go
// lowering/formatting and optional source mapping. Generation reuses one
// immutable analysis; these are component costs, not end-to-end latency.
func BenchmarkCompilerPhases(b *testing.B) {
	for _, fixture := range compilerFixtures() {
		b.Run(fixture.name, func(b *testing.B) {
			text := fixture.source()
			input := analysis.Input{Sources: map[string][]byte{"fixture.gox": text}, PackagePath: "example.test/fixture"}
			b.Run("Analyze", func(b *testing.B) {
				b.SetBytes(int64(len(text)))
				b.ReportAllocs()
				for b.Loop() {
					result, err := analysis.Analyze(b.Context(), input)
					if err != nil || result == nil || result.Checked == nil {
						b.Fatalf("analysis: %v", err)
					}
				}
			})
			result, err := analysis.Analyze(b.Context(), input)
			if err != nil {
				b.Fatal(err)
			}
			for _, mapped := range []bool{false, true} {
				name := "GenerateGo"
				if mapped {
					name += "SourceMaps"
				}
				b.Run(name, func(b *testing.B) {
					b.SetBytes(int64(len(text)))
					b.ReportAllocs()
					for b.Loop() {
						options := lower.GenerationOptions{}
						if mapped {
							options.SourceMaps = map[string]*source.Map{}
						}
						output, err := lower.Generate(b.Context(), result, options)
						if err != nil || len(output) == 0 {
							b.Fatalf("generation: %v", err)
						}
						if mapped && len(options.SourceMaps) == 0 {
							b.Fatal("missing source maps")
						}
					}
				})
			}
		})
	}
}
