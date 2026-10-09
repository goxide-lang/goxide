package matchexpr

import (
	"github.com/hxsf/goxide/internal/paths"
	"github.com/hxsf/goxide/internal/refs"
)

// parseView changes syntax only for Go AST shape inspection. All emitted
// fragments continue to come from the original source and its provenance.
func parseView(source []byte) []byte { return refs.GoView(paths.GoView(source)) }
