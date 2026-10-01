package typecheck

import "testing"

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
