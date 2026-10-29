package lower

import (
	"errors"
	goast "go/ast"
	"go/types"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/analysis"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func TestSyntheticValidationErrorHasNoSourceLocation(t *testing.T) {
	generated := goast.NewIdent("synthetic")
	e := &emitter{
		name: "source.gox", tree: &syntax.File{},
		goFile:      &goast.File{Name: generated},
		sourceNodes: map[goast.Node]syntax.Node{},
	}
	set := validationPositions([]*emitter{e})
	original := types.Error{Fset: set, Pos: generated.Pos(), Msg: "synthetic error"}
	validation := &definitionValidation{provenance: true}
	validation.report(original, []*emitter{e})
	var sourceDiagnostic *analysis.Diagnostic
	if errors.As(validation.err, &sourceDiagnostic) {
		t.Fatalf("synthetic error invented source location: %+v", sourceDiagnostic)
	}
	var generatedDiagnostic types.Error
	if !errors.As(validation.err, &generatedDiagnostic) || generatedDiagnostic.Pos != original.Pos || generatedDiagnostic.Fset != set || !strings.Contains(validation.err.Error(), "synthetic error") {
		t.Fatalf("original generated diagnostic was lost: %v", validation.err)
	}
}
