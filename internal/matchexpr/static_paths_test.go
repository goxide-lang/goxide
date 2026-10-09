package matchexpr

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestStaticPathParserViewsKeepSourceIntent(t *testing.T) {
	for name, body := range map[string]string{
		"pointer-method":        `func F()int{return (*m::S)::Pointer(nil,match E::A{E::A=>3,E::B=>0})}`,
		"generic-call":          `func F()m::T{return m::Identity[m::T](match E::A{E::A=>3,E::B=>0})}`,
		"typed-local":           `func F(){var v m::T=match E::A{E::A=>3,E::B=>0};_=v}`,
		"composite":             `func F()m::S{return m::S{N:match E::A{E::A=>3,E::B=>0}}}`,
		"function-literal-type": `func F()[]func()m::T{return []func()m::T{match E::A{E::A=>func()m::T{return match E::A{E::A=>3,E::B=>0}},E::B=>nil}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			source := []byte("package p\nenum E{A,B,}\n" + body + "\n")
			provenance := sourcemap.Identity("paths.gox", source)
			out, err := LowerMapped("paths.gox", source, provenance)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(out), "m.") || !strings.Contains(string(out), "m::") {
				t.Fatalf("parser view leaked into source: %s", out)
			}
			if !strings.Contains(string(out), "__goxide_match_expr_") {
				t.Fatalf("match was not normalized: %s", out)
			}
			// Namespace spelling still carries its original token positions; later
			// resolution must see and validate the actual source separator.
			at := strings.Index(string(out), "m::")
			segment := provenance.Lookup(at)
			if segment == nil || segment.Origin == nil || segment.Origin.Start != strings.Index(string(source), "m::") {
				t.Fatalf("lost namespace source evidence: %+v", segment)
			}
			if name == "pointer-method" && !strings.Contains(string(out), "(*m::S)::Pointer(") {
				t.Fatalf("lost static method separator: %s", out)
			}
		})
	}
}
