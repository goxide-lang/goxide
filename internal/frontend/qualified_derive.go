package frontend

import (
	"errors"
	"fmt"
	"go/types"

	"github.com/hxsf/goxide/internal/derives"
	"github.com/hxsf/goxide/internal/paths"
)

// deriveBindingLookup resolves in the declaring file's lexical scope. It must
// return the binding's canonical owner even when source spelling uses a use
// alias or a public facade. It must not infer a derive from a Go object's name.
type deriveBindingLookup func([]string) (paths.Binding, error)

func resolveDerive(text string, lookup deriveBindingLookup, imp types.Importer) (string, error) {
	path, err := paths.Parse(text)
	if err != nil {
		return "", fmt.Errorf("derive: %w", err)
	}
	names := path.Names()
	var binding paths.Binding
	if lookup != nil {
		binding, err = lookup(names)
	} else {
		err = &paths.UnboundError{Name: names[0]}
	}
	if err != nil {
		var unbound *paths.UnboundError
		if errors.As(err, &unbound) {
			// Prelude entries apply only when there is no explicit binding.
			// Serialize/Deserialize have no unbound prelude registration.
			if len(names) == 1 {
				if def, ok := derives.Builtin(names[0]); ok {
					return def.Name, nil
				}
			}
			return "", fmt.Errorf("unbound derive %s; introduce its owner or symbol with use", text)
		}
		return "", fmt.Errorf("derive %s: %w", text, err)
	}
	if binding.Kind != "derive" {
		return "", fmt.Errorf("derive %s resolves to %s, not a registered derive", text, binding.Kind)
	}
	def, ok := derives.Lookup(binding.OwnerPath, binding.Name)
	if !ok || def.Owner == "" {
		return "", fmt.Errorf("derive owner %s does not provide %s", binding.OwnerPath, binding.Name)
	}
	if err := derives.Validate(def, imp); err != nil {
		return "", err
	}
	return def.Name, nil
}

// resolveDerive is also supplied to macro collection so generated helper names
// are registered from the same file binding as the actual derive request.
func (u *usePackage) resolveDerive(file, text string) (string, error) {
	if u == nil {
		return resolveDerive(text, nil, nil)
	}
	return resolveDerive(text, func(names []string) (paths.Binding, error) {
		return u.resolvePath(file, names)
	}, u.imp)
}
