package lower_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
)

type compilerFixture struct {
	name   string
	count  int
	macros bool
}

func compilerFixtures() []compilerFixture {
	return []compilerFixture{
		{"small", 16, false},
		{"functions", 128, false},
		{"declarative_macros", 64, true},
	}
}

func (fixture compilerFixture) source() []byte {
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
	return []byte(source.String())
}

// BenchmarkCompilePackage measures source analysis and Go generation without
// hiding their cost behind a Go subprocess or dependency downloads.
func BenchmarkCompilePackage(b *testing.B) {
	for _, fixture := range compilerFixtures() {
		b.Run(fixture.name, func(b *testing.B) {
			text := fixture.source()
			input := map[string][]byte{"fixture.gox": text}
			b.SetBytes(int64(len(text)))
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
