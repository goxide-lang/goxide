package buffer

type Buffer struct{ Data []byte }

func (b *Buffer) Read(p []byte) (int, error) {
	return copy(p, b.Data), nil
}
