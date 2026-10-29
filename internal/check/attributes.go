package check

import (
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/syntax/ast"
)

// The expander consumes executable attributes and registered derive helpers.
// Anything left has no semantic consumer.
func (c *checker) checkRemainingAttributes() {
	for _, pkg := range c.out.Resolve.Packages {
		for _, file := range pkg.Files {
			c.file = file
			ast.Walk(file.AST, func(node ast.Node) bool {
				if attribute, ok := node.(*ast.Attr); ok {
					c.diag(attribute, DeriveInvalid, "unsupported attribute or invalid attribute position: "+resolve.AttributeName(attribute))
					return false
				}
				return true
			})
		}
	}
}
