package buffer

func (s *Buffer) HyperGoTraitByteReader() {}

func (s *Buffer) AsByteReader() interface {
	Read([]byte) (int, error)
	HyperGoTraitByteReader()
} {
	return s
}
