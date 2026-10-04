package views

type ByteReader interface {
	Read([]byte) (int, error)
	GoxideTraitByteReader()
}

type TextReader interface {
	Read() string
	GoxideTraitTextReader()
}

type Buffer struct {
	data []byte
}

func NewBuffer(data []byte) *Buffer {
	return &Buffer{data: data}
}

type bufferByteReader struct{ target *Buffer }
type bufferTextReader struct{ target *Buffer }

func (b *Buffer) AsByteReader() ByteReader {
	if b == nil {
		panic("goxide: nil Buffer")
	}
	return bufferByteReader{target: b}
}

func (b *Buffer) AsTextReader() TextReader {
	if b == nil {
		panic("goxide: nil Buffer")
	}
	return bufferTextReader{target: b}
}

func (v bufferByteReader) GoxideTraitByteReader() {}
func (v bufferTextReader) GoxideTraitTextReader() {}

func (v bufferByteReader) Read(p []byte) (int, error) {
	if v.target == nil {
		panic("goxide: invalid ByteReader view")
	}
	return copy(p, v.target.data), nil
}

func (v bufferTextReader) Read() string {
	if v.target == nil {
		panic("goxide: invalid TextReader view")
	}
	return string(v.target.data)
}
