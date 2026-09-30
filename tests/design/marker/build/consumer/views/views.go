package views

type ByteReader interface {
	Read([]byte) (int, error)
	HyperGoTraitByteReader()
}

type TextReader interface {
	Read() string
	HyperGoTraitTextReader()
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
		panic("hgo: nil Buffer")
	}
	return bufferByteReader{target: b}
}

func (b *Buffer) AsTextReader() TextReader {
	if b == nil {
		panic("hgo: nil Buffer")
	}
	return bufferTextReader{target: b}
}

func (v bufferByteReader) HyperGoTraitByteReader() {}
func (v bufferTextReader) HyperGoTraitTextReader() {}

func (v bufferByteReader) Read(p []byte) (int, error) {
	if v.target == nil {
		panic("hgo: invalid ByteReader view")
	}
	return copy(p, v.target.data), nil
}

func (v bufferTextReader) Read() string {
	if v.target == nil {
		panic("hgo: invalid TextReader view")
	}
	return string(v.target.data)
}
