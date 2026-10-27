package lower_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
)

// BenchmarkCompilePackage measures source analysis and Go generation without
// hiding their cost behind a Go subprocess or dependency downloads.
func BenchmarkCompilePackage(b *testing.B) {
	for _, fixture := range []struct {
		name   string
		count  int
		macros bool
	}{
		{"small", 16, false},
		{"functions", 128, false},
		{"declarative_macros", 64, true},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			var source strings.Builder
			source.WriteString("package fixture\n")
			if fixture.macros {
				source.WriteString("macro_rules! increment { ($value:expr) => { $value + 1 }; }\n")
			}
			for i := range fixture.count {
				body := "value + 1"
				if fixture.macros {
					body = "increment!(value)"
				}
				fmt.Fprintf(&source, "func Value%d(value int) int { return %s }\n", i, body)
			}
			input := map[string][]byte{"fixture.gox": []byte(source.String())}
			b.SetBytes(int64(source.Len()))
			b.ReportAllocs()
			for b.Loop() {
				output, err := lower.CompilePackage(input, "example.test/fixture", nil, nil, lower.PackageOptions{})
				if err != nil {
					b.Fatal(err)
				}
				if len(output["fixture.gox"]) == 0 {
					b.Fatal("missing generated Go")
				}
			}
		})
	}
}
