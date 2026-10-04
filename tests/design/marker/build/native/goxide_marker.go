package buffer

func (s *Buffer) GoxideTraitByteReader() {}

func (s *Buffer) AsByteReader() interface {
	Read([]byte) (int, error)
	GoxideTraitByteReader()
} {
	return s
}
