package frontend

import (
	"errors"

	"github.com/hxsf/goxide/internal/sourcemap"
)

// Declaration positions refer to the initial namespace-normalized source,
// before carriers, generic suffixes and late declarations change its length.
func mapProtocolDeclarationError(err error, uses *usePackage) error {
	var declaration *protocolDeclarationError
	if !errors.As(err, &declaration) || uses == nil {
		return err
	}
	diagnostic := &sourcemap.Error{Message: declaration.Message, Generated: declaration.Position}
	if mapping := uses.sourceMap(declaration.Position.Filename); mapping != nil {
		if segment := mapping.Lookup(declaration.Position.Offset); segment != nil {
			diagnostic.Origin, diagnostic.Stack = segment.Origin, segment.Stack
		}
	}
	return diagnostic
}
