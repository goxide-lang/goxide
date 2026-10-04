package contracts

import native "example.org/native/buffer"

type Reader interface {
	Read([]byte) (int, error)
	GoxideTraitByteReader()
}

var _ Reader = (*native.Buffer)(nil)
