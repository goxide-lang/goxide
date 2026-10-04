package typecheck

import (
	"context"
	"testing"
	"time"
)

func TestLocalRuntimeAndStandardIdentity(t *testing.T) {
	imp := NewImporter("input.hgo")
	runtime, err := imp.Import("github.com/hxsf/goxide/serde")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Scope().Lookup("Serializer") == nil {
		t.Fatal("missing actual runtime export")
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
	imp := NewImporterWithContext(ctx, "input.hgo", nil, false, nil)
	if _, err := imp.Import("fmt"); err == nil {
		t.Fatal("cancelled export query succeeded")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancelled export query delayed")
	}
}
