package frontend

import (
	"errors"
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/sourcemap"
	"strings"
	"testing"
)

func TestQualifiedSerdeProtocolErrorUsesCanonicalOwner(t *testing.T) {
	source := []byte("package p\nuse serde;use time;\n#[derive(serde::Deserialize)]\ntype Wrap struct{Value time::Time}\n")
	roots := serdeFixtureRoots()
	roots["time"] = paths.Namespace{PackagePath: "time", Source: "toolchain"}
	_, err := CompileWithOptions("protocol.gox", source, FileOptions{Roots: roots})
	var diagnostic *sourcemap.Error
	if !errors.As(err, &diagnostic) || diagnostic.Origin == nil {
		t.Fatalf("missing exact selected type origin: %v", err)
	}
	if got := string(source[diagnostic.Origin.Start:diagnostic.Origin.End]); got != "Time" {
		t.Fatalf("origin %q: %v", got, err)
	}
	if !strings.Contains(err.Error(), "required registered serde protocol: time::Time") || strings.Contains(err.Error(), "goxideNamespace") {
		t.Fatal(err)
	}
}
