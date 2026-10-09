package sourcemap

import (
	"go/parser"
	"testing"
)

func TestASTEvidenceAddInputPreservesDistinctTokenOrigins(t *testing.T) {
	f := evidenceInput(t, "package p\nvar Before = T.M\n")
	source := []byte("package p\nvar After = T.M\n")
	fragment, err := parser.ParseFile(f.fs, "fragment.gox", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	mapping := Identity("fragment.gox", source)
	if !f.e.AddInput(f.fs, fragment, mapping) {
		t.Fatal("fresh fragment rejected")
	}
	if f.e.AddInput(f.fs, fragment, Identity("wrong.gox", source)) {
		t.Fatal("existing node proof overwritten")
	}
	f.root.Decls = append(f.root.Decls, fragment.Decls...)
	output := evidencePrint(t, f)
	before := evidenceAt(t, output, "Before = T.M", len("Before = T."))
	after := evidenceAt(t, output, "After = T.M", len("After = T."))
	if before == nil || before.Origin == nil || before.Origin.File != "input.gox" || after == nil || after.Origin == nil || after.Origin.File != "fragment.gox" {
		t.Fatalf("distinct copied tokens lost origins: before=%+v after=%+v", before, after)
	}
}
