package analysis

import (
	"go/types"

	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

// A struct literal's key names a field rather than a lexical binding. Map and
// array keys remain ordinary expressions indexed by the regular syntax walk.
func (q *Queries) indexCompositeKeys(name string, literal *syntax.CompositeLit, fieldKeys map[*syntax.Ident]bool) {
	if q.result.Checked == nil {
		return
	}
	receiver := q.result.Checked.Types[literal]
	if receiver == nil {
		return
	}
	base := types.Unalias(receiver)
	if pointer, ok := base.Underlying().(*types.Pointer); ok {
		base = types.Unalias(pointer.Elem())
	}
	structure, ok := base.Underlying().(*types.Struct)
	if !ok {
		return
	}
	for _, element := range literal.Elts {
		pair, ok := element.(*syntax.KeyValue)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*syntax.Ident)
		if !ok {
			continue
		}
		for i := 0; i < structure.NumFields(); i++ {
			field := structure.Field(i)
			if field.Name() != key.Name {
				continue
			}
			symbol, goRef := q.goMember(receiver, key.Name, name)
			if symbol != nil || goRef != nil {
				fieldKeys[key] = true
				location := q.location(name, key)
				q.entries[location.File] = append(q.entries[location.File], queryEntry{location: location, symbol: symbol, goRef: goRef, typ: field.Type(), deferred: symbol == nil})
			}
			break
		}
	}
}
