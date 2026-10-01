package sourcemap

import (
	"fmt"
	"strconv"
	"strings"
)

// Fragments transports ranges at the precise point where a text lowerer copies
// them. Private placeholders exist only in the renderer, never in its output.
// Equal token text, repeated literals and generated syntax do not establish an
// origin. Input containing the private delimiter is rejected by the caller's
// scanner/parser; Copy leaves it unmapped rather than interpreting it.
type Fragments struct {
	Input  *Map
	ranges [][2]int
}

func (f *Fragments) Copy(start, end int) string {
	if f == nil || f.Input == nil {
		panic("missing fragment input")
	}
	f.ranges = append(f.ranges, [2]int{start, end})
	return fmt.Sprintf("\x00%d\x00", len(f.ranges)-1)
}
func (f *Fragments) Finish(text, stage string) ([]byte, *Map) {
	var out strings.Builder
	m := &Map{Stages: append(append([]string{}, f.Input.Stages...), stage)}
	for len(text) > 0 {
		i := strings.IndexByte(text, 0)
		if i < 0 {
			out.WriteString(text)
			break
		}
		out.WriteString(text[:i])
		text = text[i+1:]
		j := strings.IndexByte(text, 0)
		if j < 0 {
			panic("invalid fragment")
		}
		n, err := strconv.Atoi(text[:j])
		if err != nil || n < 0 || n >= len(f.ranges) {
			panic("invalid fragment")
		}
		r := f.ranges[n]
		m.Segments = append(m.Segments, f.Input.Copy(r[0], r[1], out.Len())...)
		out.Write(f.Input.Source[r[0]:r[1]])
		text = text[j+1:]
	}
	m.Source = []byte(out.String())
	return m.Source, m
}
