package check

import "go/types"

// Comparable follows array elements before inspecting the Go representative,
// whose interface constraint does not describe the source array layout.
func (info *Info) Comparable(t types.Type) bool {
	if t == nil {
		return false
	}
	if array, ok := info.ArrayOf(t); ok {
		return info.Comparable(array.Elem)
	}
	if structure, ok := t.Underlying().(*types.Struct); ok {
		for i := 0; i < structure.NumFields(); i++ {
			if !info.Comparable(structure.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return types.Comparable(t)
}
