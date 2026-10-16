package typecheck

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/hxsf/goxide/internal/config"
)

func testConfig(t *testing.T, captured []string) config.Config {
	t.Helper()
	if captured == nil {
		captured = os.Environ()
	}
	environment, err := config.NewEnvironment(config.HostTarget(), captured)
	if err != nil {
		t.Fatal(err)
	}
	return config.Config{Environment: environment, Reads: new(config.Reads)}
}

func TestLocalRuntimeAndStandardIdentity(t *testing.T) {
	imp := NewImporter("input.gox", ImporterOptions{Config: testConfig(t, nil)})
	runtime, err := imp.Import("github.com/goxide-lang/serde")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Scope().Lookup("Serializer") == nil {
		t.Fatal("missing actual runtime export")
	}
	origins := imp.(interface {
		PackageOrigin(string) (string, bool, error)
	})
	for _, tc := range []struct {
		path, module string
		standard     bool
	}{
		{"github.com/goxide-lang/serde", "github.com/goxide-lang/serde", false},
		{"github.com/goxide-lang/std/runtime", "github.com/goxide-lang/std", false},
		{"fmt", "", true},
		{"unsafe", "", true},
	} {
		module, standard, err := origins.PackageOrigin(tc.path)
		if err != nil || module != tc.module || standard != tc.standard {
			t.Fatalf("origin(%s) = %q, %t, %v", tc.path, module, standard, err)
		}
	}
	fmtPackage, err := imp.Import("fmt")
	if err != nil {
		t.Fatal(err)
	}
	again, err := imp.Import("fmt")
	if err != nil || again != fmtPackage {
		t.Fatal("package identity/cache lost")
	}
}

func TestImporterHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	imp := NewImporter("input.gox", ImporterOptions{Context: ctx, Config: testConfig(t, nil)})
	if _, err := imp.Import("fmt"); err == nil {
		t.Fatal("cancelled export query succeeded")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancelled export query delayed")
	}
}
