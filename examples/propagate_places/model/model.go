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
