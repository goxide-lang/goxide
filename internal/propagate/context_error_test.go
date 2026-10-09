package propagate

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/enums"
	"github.com/hxsf/goxide/internal/refs"
	"github.com/hxsf/goxide/internal/typecheck"
)

// Both reference modes must retain the complete multi-argument enum type before
// contextual family inference; a truncated Result alias is not usable evidence.
func TestContextualErrorReferencePreparation(t *testing.T) {
	source := []byte(`package producer
func Slot(v &mut Result[int,error])&mut error {
 match &mut *v {Result::Err(p)=>{return p},Result::Ok(_)=>{panic("ok")},};panic("unreachable")
}
func View(v &Result[int,error])&error {
 match &*v {Result::Err(p)=>{return p},Result::Ok(_)=>{panic("ok")},};panic("unreachable")
}
`)
	lowered, err := enums.Lower("context-error.gox", source)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := refs.Prepare("context-error.gox", lowered.Source)
	if err != nil {
		t.Fatal(err)
	}
	temps := map[string]bool{}
	out, err := Lower("context-error.gox", prepared.Source, Config{PackagePath: "example.org/errorproducer", Importer: typecheck.NewImporter("input.gox"), Families: lowered.Enums, MatchReferenceTemps: temps})
	if err != nil {
		t.Fatalf("%v\n%s", err, prepared.Source)
	}
	if strings.Contains(string(out), "__goxide_enum_context_match(") || len(temps) == 0 || !strings.Contains(string(out), "__goxide_enum_project_read(") || !strings.Contains(string(out), "__goxide_enum_project_write(") {
		t.Fatalf("contextual match or its projection evidence was not resolved: %v\n%s", temps, out)
	}
}
