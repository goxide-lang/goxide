// Package source provides immutable source text and byte/position conversion.
package source

import (
	"fmt"
	"sort"
	"unicode/utf8"
)

type Encoding string

const (
	UTF8  Encoding = "utf-8"
	UTF16 Encoding = "utf-16"
	UTF32 Encoding = "utf-32"
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Index owns immutable text. Positions are zero-based, and byte ranges are
// half-open. Empty encoding selects LSP's required UTF-16 default.
type Index struct {
	text   string
	starts []int
}

func New(text string) *Index {
	x := &Index{text: text, starts: []int{0}}
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			x.starts = append(x.starts, i+1)
		case '\n':
			x.starts = append(x.starts, i+1)
		}
	}
	return x
}

func (x *Index) Text() string { return x.text }
func (x *Index) Len() int     { return len(x.text) }

func (x *Index) lineEnd(line int) int {
	if line+1 == len(x.starts) {
		return len(x.text)
	}
	end := x.starts[line+1]
	if end > x.starts[line] && x.text[end-1] == '\n' {
		end--
	}
	if end > x.starts[line] && x.text[end-1] == '\r' {
		end--
	}
	return end
}

func validEncoding(encoding Encoding) bool {
	return encoding == "" || encoding == UTF8 || encoding == UTF16 || encoding == UTF32
}

func width(r rune, size int, encoding Encoding) int {
	switch encoding {
	case UTF8:
		return size
	case UTF32:
		return 1
	default:
		if r > 0xffff {
			return 2
		}
		return 1
	}
}

func (x *Index) Position(offset int, encoding Encoding) (Position, error) {
	if offset < 0 || offset > len(x.text) || !validEncoding(encoding) {
		return Position{}, fmt.Errorf("invalid source offset %d or encoding %q", offset, encoding)
	}
	line := sort.Search(len(x.starts), func(i int) bool { return x.starts[i] > offset }) - 1
	if offset > x.lineEnd(line) {
		return Position{}, fmt.Errorf("source offset %d splits a line ending", offset)
	}
	character := 0
	for i := x.starts[line]; i < offset; {
		r, size := utf8.DecodeRuneInString(x.text[i:])
		if i+size > offset {
			return Position{}, fmt.Errorf("source offset %d splits a UTF-8 sequence", offset)
		}
		character += width(r, size, encoding)
		i += size
	}
	return Position{Line: line, Character: character}, nil
}

// Offset clamps positions beyond a line/document to its end as LSP specifies.
// Positions inside a UTF-8 sequence or UTF-16 surrogate pair are rejected, so
// callers cannot silently turn an invalid edit into a different source edit.
func (x *Index) Offset(position Position, encoding Encoding) (int, error) {
	if position.Line < 0 || position.Character < 0 || !validEncoding(encoding) {
		return 0, fmt.Errorf("invalid source position %+v or encoding %q", position, encoding)
	}
	if position.Line >= len(x.starts) {
		return len(x.text), nil
	}
	end := x.lineEnd(position.Line)
	character := 0
	for i := x.starts[position.Line]; i < end; {
		if character == position.Character {
			return i, nil
		}
		r, size := utf8.DecodeRuneInString(x.text[i:end])
		character += width(r, size, encoding)
		if character > position.Character {
			return 0, fmt.Errorf("source position %+v splits a character", position)
		}
		i += size
	}
	return end, nil
}

func (x *Index) Range(start, end int, encoding Encoding) (Range, error) {
	if start > end {
		return Range{}, fmt.Errorf("reversed source range %d:%d", start, end)
	}
	from, err := x.Position(start, encoding)
	if err != nil {
		return Range{}, err
	}
	to, err := x.Position(end, encoding)
	return Range{Start: from, End: to}, err
}

func (x *Index) Offsets(span Range, encoding Encoding) (start, end int, err error) {
	start, err = x.Offset(span.Start, encoding)
	if err != nil {
		return 0, 0, err
	}
	end, err = x.Offset(span.End, encoding)
	if err == nil && start > end {
		err = fmt.Errorf("reversed source range %+v", span)
	}
	return
}
