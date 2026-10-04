package macroreview

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/macros"
)

// Isolate macro expansion from downstream frontend/type-checker costs.
func BenchmarkRepeatedHelper(b *testing.B) {
	for _, n := range []int{128, 512, 2048} {
		for _, imported := range []bool{false, true} {
			b.Run(fmt.Sprintf("imported=%v/n=%d", imported, n), func(b *testing.B) {
				var source strings.Builder
				source.WriteString("package p\n")
				helper := "Helper"
				if imported {
					source.WriteString("import \"strconv\"\n")
					helper = "strconv.Itoa"
				} else {
					source.WriteString("func Helper(n int) string { return \"\" }\n")
				}
				source.WriteString("macro_rules! all { ($($x:expr),*) => { $(_ = " + helper + "($x));* }; }\nfunc Run() { all!(")
				for i := 0; i < n; i++ {
					if i > 0 {
						source.WriteByte(',')
					}
					fmt.Fprint(&source, i)
				}
				source.WriteString(") }\n")
				data := []byte(source.String())
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					if _, err := macros.Expand("repeated.hgo", data); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
