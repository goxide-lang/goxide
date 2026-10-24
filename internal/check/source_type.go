package check

import (
	"go/types"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// SourceType returns the checked type of an annotation, including annotations
// whose representation did not need an override during demand collection.
func (i *Info) SourceType(node ast.Type) types.Type {
	if override := i.TypeOverrides[node]; override != nil {
		return override
	}
	return i.typeNodes[node]
}
