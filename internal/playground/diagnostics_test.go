package playground

import (
	"errors"
	"go/types"
	"testing"

	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/typecheck"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestPackageGeneratedDiagnosticDisplayName(t *testing.T) {
	for _, test := range []struct{ name, declarations, want string }{
		{"generated Go", "", "playground.gox:5:36: undefined: T"},
		{"generic definition", "func identity[T any](value T) T { return value }\n", "泛型定义检查失败（生成的 Go 无源码映射）: playground.gox:1:953: undefined: T"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := Options{Config: testdeps.Configuration(), Importer: typecheck.NewImporter("playground.gox", typecheck.ImporterOptions{Config: testdeps.Configuration()}), Roots: paths.Roots{
				"std": {Source: "module", ModulePath: "github.com/goxide-lang/std", GoPrefix: "github.com/goxide-lang/std"},
			}}
			_, err := CompilePackage([]File{{"playground.gox", "package main\n" + test.declarations + "func main() { value := Option::None; _ = value }\n"}}, options)
			if err == nil || err.Error() != test.want {
				t.Fatalf("diagnostic = %v, want %q", err, test.want)
			}
			var generated types.Error
			if !errors.As(err, &generated) || generated.Fset.Position(generated.Pos).Filename != "playground/playground.gox" {
				t.Fatalf("display formatting lost the original generated diagnostic: %v", err)
			}
		})
	}
}
