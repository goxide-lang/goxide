// Package derives defines the compiler's fixed derive registry. A registration
// is a compile-time symbol, distinct from an ordinary Go type or function.
package derives

import (
	"fmt"
	"go/types"
)

const SerdePackage = "github.com/goxide-lang/serde"

// Definition identifies a derive and the protocol its generated helpers use.
// Owner is empty only for a compiler builtin.
type Definition struct {
	Owner, Name, Protocol string
}

func Builtin(name string) (Definition, bool) {
	switch name {
	case "Default", "Clone", "Debug", "Error", "PartialEq", "Eq":
		return Definition{Name: name}, true
	}
	return Definition{}, false
}

// Lookup takes canonical identities, never a source alias or a path suffix.
func Lookup(owner, name string) (Definition, bool) {
	if owner == "" {
		return Builtin(name)
	}
	if owner == SerdePackage {
		switch name {
		case "Serialize":
			return Definition{owner, name, "Serializer"}, true
		case "Deserialize":
			return Definition{owner, name, "Deserializer"}, true
		}
	}
	return Definition{}, false
}

// Validate checks the selected owner's actual exported protocol. Serializer
// and Deserializer are validated independently: selecting one direction does
// not require the other protocol to exist. A canonical package spelling alone
// is insufficient: the resolver must prove the selected module owns that path.
func Validate(def Definition, imp types.Importer) error {
	registered, ok := Lookup(def.Owner, def.Name)
	if !ok || registered != def {
		return fmt.Errorf("unregistered derive owner %q member %s", def.Owner, def.Name)
	}
	if def.Owner == "" {
		return nil
	}
	if imp == nil {
		return fmt.Errorf("derive owner %s requires a package importer", def.Owner)
	}
	origin, ok := imp.(interface {
		PackageOrigin(string) (string, bool, error)
	})
	if !ok {
		return fmt.Errorf("derive owner %s requires package module origin proof", def.Owner)
	}
	module, standard, err := origin.PackageOrigin(def.Owner)
	if err != nil {
		return fmt.Errorf("derive owner %s module origin: %w", def.Owner, err)
	}
	if standard || module != SerdePackage {
		return fmt.Errorf("derive owner %s must belong to module %s, got module %q (standard=%t)", def.Owner, SerdePackage, module, standard)
	}
	pkg, err := imp.Import(def.Owner)
	if err != nil {
		return fmt.Errorf("derive owner %s: %w", def.Owner, err)
	}
	if pkg == nil || pkg.Path() != def.Owner {
		return fmt.Errorf("derive owner identity mismatch: expected %s", def.Owner)
	}
	obj, ok := pkg.Scope().Lookup(def.Protocol).(*types.TypeName)
	if !ok {
		return fmt.Errorf("derive owner %s is missing protocol %s", def.Owner, def.Protocol)
	}
	named, ok := types.Unalias(obj.Type()).(*types.Named)
	if !ok || named.Obj().Pkg() != pkg || named.Obj().Name() != def.Protocol {
		return fmt.Errorf("derive owner protocol %s must be defined by %s", def.Protocol, def.Owner)
	}
	if named.TypeParams().Len() != 0 {
		return fmt.Errorf("derive owner protocol %s must be non-generic", def.Protocol)
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok || !iface.IsMethodSet() {
		return fmt.Errorf("derive owner protocol %s must be a value interface", def.Protocol)
	}
	return nil
}
