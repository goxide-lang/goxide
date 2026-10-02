package model

type mask uint64
type flag bool
type inner struct {
	Wide    mask
	Enabled flag
}
type Record struct{ inner }
type Masks []mask
type Table map[mask]mask
type Pair [2]mask

func NewMask() *mask { return new(mask) }

type count uint

var N mask = 1

func Count(n int) count { return count(n) }

// Tree deliberately exports promoted fields while keeping both pointer layers
// private. These ordinary business methods mutate/observe the native model;
// the compiler never calls them to obtain an assignment destination.
type leaf struct {
	Count   int
	Wide    mask
	Enabled flag
	Values  [2]int
}
type branch struct{ *leaf }
type Tree struct{ *branch }

func NewTree(n int) Tree        { return Tree{&branch{&leaf{Count: n}}} }
func NilLeaf() Tree             { return Tree{&branch{}} }
func NilBranch() Tree           { return Tree{} }
func Replace(t *Tree, n int)    { t.leaf = &leaf{Count: n} }
func Observe(t Tree) func() int { old := t.leaf; return func() int { return old.Count } }
func LeafSlot(t *Tree) **leaf   { return &t.leaf }
func NewLeaf(n int) *leaf       { return &leaf{Count: n} }

// Native helpers expose private scalar identity without exporting its name.
func Flag(v bool) flag { return flag(v) }
func BoolKind[T any](v T) int {
	if _, ok := any(v).(bool); ok {
		return 1
	}
	return 2
}
