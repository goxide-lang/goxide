package model

type mask uint64
type enabled bool
type row struct {
	Count int
	Mask  mask
}

type Rows []*row
type Masks []mask
type Table map[mask]mask
type Toggles []enabled
type Pair [2]row
