// Package lexer implements Goxide tokenization and automatic semicolons.
package lexer

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/goxide-lang/goxide/internal/syntax/token"
)

type Diagnostic struct {
	Start, End uint32
	Message    string
}

type delimiter struct {
	kind token.Kind
	attr bool
}
type scanner struct {
	src         []byte
	pos         int
	tokens      []token.Token
	diagnostics []Diagnostic
	semi        bool
	delims      []delimiter
	attrPrefix  int
}

// Lex includes comments, inserted semicolons, and a final EOF token. It always
// makes progress after malformed input and reports byte-based diagnostics.
func Lex(src []byte) ([]token.Token, []Diagnostic) {
	s := scanner{src: src}
	for p := 0; p < len(src); {
		r, n := utf8.DecodeRune(src[p:])
		if r == utf8.RuneError && n == 1 {
			s.error(p, p+1, "invalid UTF-8 encoding")
		}
		if r == 0 {
			s.error(p, p+n, "NUL is not permitted in source")
		}
		if r == '\ufeff' && p != 0 {
			s.error(p, p+n, "byte order mark is only permitted at the start of a file")
		}
		p += n
	}
	if len(src) >= 3 && string(src[:3]) == "\xef\xbb\xbf" {
		s.pos = 3
	}
	for s.pos < len(s.src) {
		s.next()
	}
	if s.semi {
		s.insertSemi(s.pos)
	}
	s.tokens = append(s.tokens, token.Token{Kind: token.EOF, Start: uint32(s.pos), End: uint32(s.pos)})
	return s.tokens, s.diagnostics
}

func (s *scanner) error(start, end int, message string) {
	s.diagnostics = append(s.diagnostics, Diagnostic{Start: uint32(start), End: uint32(end), Message: message})
}

func (s *scanner) insertSemi(pos int) {
	s.tokens = append(s.tokens, token.Token{Kind: token.SEMICOLON, Text: ";", Start: uint32(pos), End: uint32(pos), Implicit: true})
	s.semi = false
}

func (s *scanner) emit(kind token.Kind, start int, raw bool) {
	text := string(s.src[start:s.pos])
	if raw {
		text = text[2:]
	}
	s.tokens = append(s.tokens, token.Token{Kind: kind, Text: text, Start: uint32(start), End: uint32(s.pos), Raw: raw})
	if kind == token.COMMENT {
		return
	}
	attrClose := false
	switch kind {
	case token.LPAREN, token.LBRACK, token.LBRACE:
		s.delims = append(s.delims, delimiter{kind: kind, attr: kind == token.LBRACK && s.attrPrefix != 0})
	case token.RPAREN, token.RBRACK, token.RBRACE:
		if n := len(s.delims); n > 0 {
			d := s.delims[n-1]
			if (d.kind == token.LPAREN && kind == token.RPAREN) || (d.kind == token.LBRACK && kind == token.RBRACK) || (d.kind == token.LBRACE && kind == token.RBRACE) {
				attrClose = d.attr
				s.delims = s.delims[:n-1]
			}
		}
	}
	if kind == token.HASH {
		s.attrPrefix = 1
	} else if kind == token.NOT && s.attrPrefix == 1 {
		s.attrPrefix = 2
	} else {
		s.attrPrefix = 0
	}
	s.semi = kind.IsLiteral() || kind == token.BREAK || kind == token.CONTINUE || kind == token.FALLTHROUGH || kind == token.RETURN || kind == token.INC || kind == token.DEC || kind == token.RPAREN || kind == token.RBRACK || kind == token.RBRACE || kind == token.QUESTION || kind == token.SELF
	if attrClose {
		s.semi = false
	}
}

func (s *scanner) next() {
	start := s.pos
	c := s.src[s.pos]
	switch c {
	case ' ', '\t', '\r':
		s.pos++
		return
	case '\n':
		if s.semi {
			s.insertSemi(s.pos)
		}
		s.pos++
		return
	}
	r, n := utf8.DecodeRune(s.src[s.pos:])
	if isLetter(r) {
		raw := c == 'r' && s.pos+2 < len(s.src) && s.src[s.pos+1] == '#' && startsIdent(s.src[s.pos+2:])
		if raw {
			s.pos += 2
			_, n = utf8.DecodeRune(s.src[s.pos:])
		}
		s.pos += n
		for s.pos < len(s.src) {
			r, n = utf8.DecodeRune(s.src[s.pos:])
			if !isLetter(r) && !unicode.IsDigit(r) {
				break
			}
			s.pos += n
		}
		kind := token.IDENT
		if !raw {
			kind = token.Lookup(string(s.src[start:s.pos]))
		}
		s.emit(kind, start, raw)
		return
	}
	if isDigit(c) || c == '.' && s.pos+1 < len(s.src) && isDigit(s.src[s.pos+1]) {
		kind := s.number()
		s.emit(kind, start, false)
		return
	}
	if c == '"' || c == '\'' || c == '`' {
		s.quoted(c)
		kind := token.STRING
		if c == '\'' {
			kind = token.CHAR
		}
		s.emit(kind, start, false)
		return
	}
	if c == '/' && s.pos+1 < len(s.src) {
		next := s.src[s.pos+1]
		if next == '/' {
			s.pos += 2
			for s.pos < len(s.src) && s.src[s.pos] != '\n' {
				s.pos++
			}
			s.emit(token.COMMENT, start, false)
			return
		}
		if next == '*' {
			s.pos += 2
			newline := -1
			closed := false
			for s.pos < len(s.src) {
				if s.src[s.pos] == '\n' && newline < 0 {
					newline = s.pos
				}
				if s.src[s.pos] == '*' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '/' {
					s.pos += 2
					closed = true
					break
				}
				s.pos++
			}
			if !closed {
				s.error(start, s.pos, "unterminated block comment")
			}
			s.emit(token.COMMENT, start, false)
			if newline >= 0 && s.semi {
				s.insertSemi(newline)
			}
			return
		}
	}
	for width := 3; width >= 1; width-- {
		if s.pos+width > len(s.src) {
			continue
		}
		if kind, ok := operators[string(s.src[s.pos:s.pos+width])]; ok {
			s.pos += width
			s.emit(kind, start, false)
			return
		}
	}
	s.pos += n
	s.error(start, s.pos, "unexpected character "+strconv.QuoteRune(r))
	s.emit(token.ILLEGAL, start, false)
}

var operators = func() map[string]token.Kind {
	m := make(map[string]token.Kind)
	for k := token.ADD; k <= token.DOLLAR; k++ {
		m[k.String()] = k
	}
	return m
}()

func isLetter(r rune) bool        { return r == '_' || unicode.IsLetter(r) }
func startsIdent(src []byte) bool { r, _ := utf8.DecodeRune(src); return isLetter(r) }
func isDigit(c byte) bool         { return c >= '0' && c <= '9' }
func hexDigit(c byte) bool        { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func (s *scanner) quoted(quote byte) {
	start := s.pos
	s.pos++
	closed := false
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == quote {
			s.pos++
			closed = true
			break
		}
		if c == '\n' && quote != '`' {
			break
		}
		if c == '\\' && quote != '`' {
			s.pos++
			if s.pos >= len(s.src) || s.src[s.pos] == '\n' {
				break
			}
		}
		_, n := utf8.DecodeRune(s.src[s.pos:])
		s.pos += n
	}
	if !closed {
		s.error(start, s.pos, "unterminated literal")
		return
	}
	value, err := strconv.Unquote(string(s.src[start:s.pos]))
	if err != nil || quote == '\'' && utf8.RuneCountInString(value) != 1 {
		s.error(start, s.pos, "invalid string or rune literal")
	}
}

func (s *scanner) number() token.Kind {
	start := s.pos
	base := 10
	prefix := false
	legacyOctal := false
	digits := 0
	invalidDigit := false
	if s.src[s.pos] == '0' {
		legacyOctal = true
		if s.pos+1 < len(s.src) {
			switch s.src[s.pos+1] {
			case 'x', 'X':
				base = 16
				prefix = true
			case 'o', 'O':
				base = 8
				prefix = true
			case 'b', 'B':
				base = 2
				prefix = true
			}
		}
		if prefix {
			s.pos += 2
			legacyOctal = false
		}
	}
	scanDigits := func() {
		for s.pos < len(s.src) {
			c := s.src[s.pos]
			if c == '_' {
				s.pos++
				continue
			}
			if !isDigit(c) && !(base == 16 && hexDigit(c)) {
				break
			}
			if isDigit(c) && int(c-'0') >= base {
				invalidDigit = true
			}
			digits++
			s.pos++
		}
	}
	scanDigits()
	fraction := false
	if s.pos < len(s.src) && s.src[s.pos] == '.' {
		fraction = true
		s.pos++
		scanDigits()
	}
	exponent := byte(0)
	if s.pos < len(s.src) {
		c := s.src[s.pos]
		if c == 'e' || c == 'E' || c == 'p' || c == 'P' {
			exponent = c
			s.pos++
			if s.pos < len(s.src) && (s.src[s.pos] == '+' || s.src[s.pos] == '-') {
				s.pos++
			}
			expDigits := 0
			for s.pos < len(s.src) && (isDigit(s.src[s.pos]) || s.src[s.pos] == '_') {
				if s.src[s.pos] != '_' {
					expDigits++
				}
				s.pos++
			}
			if expDigits == 0 {
				s.error(start, s.pos, "exponent has no digits")
			}
		}
	}
	kind := token.INT
	if fraction || exponent != 0 {
		kind = token.FLOAT
	}
	if s.pos < len(s.src) && s.src[s.pos] == 'i' {
		kind = token.IMAG
		s.pos++
	}
	if digits == 0 {
		s.error(start, s.pos, "numeric literal has no digits")
	}
	if legacyOctal && kind == token.INT {
		for _, c := range s.src[start:s.pos] {
			if c == '8' || c == '9' {
				invalidDigit = true
			}
		}
	}
	if invalidDigit {
		s.error(start, s.pos, "invalid digit in numeric literal")
	}
	if fraction && base != 10 && base != 16 {
		s.error(start, s.pos, "radix point requires decimal or hexadecimal mantissa")
	}
	if (exponent == 'e' || exponent == 'E') && base != 10 {
		s.error(start, s.pos, "decimal exponent requires decimal mantissa")
	}
	if (exponent == 'p' || exponent == 'P') && base != 16 {
		s.error(start, s.pos, "binary exponent requires hexadecimal mantissa")
	}
	if base == 16 && fraction && exponent == 0 {
		s.error(start, s.pos, "hexadecimal float requires a binary exponent")
	}
	lit := string(s.src[start:s.pos])
	if strings.Contains(lit, "_") && !validUnderscores(lit, prefix, base) {
		s.error(start, s.pos, "underscore must separate successive digits")
	}
	return kind
}

func validUnderscores(lit string, prefix bool, base int) bool {
	inExponent := false
	for i := 0; i < len(lit); i++ {
		c := lit[i]
		if c == 'p' || c == 'P' || base != 16 && (c == 'e' || c == 'E') {
			inExponent = true
		}
		if c != '_' {
			continue
		}
		digit := isDigit
		if base == 16 && !inExponent {
			digit = hexDigit
		}
		before := i > 0 && digit(lit[i-1]) || prefix && i == 2
		after := i+1 < len(lit) && digit(lit[i+1])
		if !before || !after {
			return false
		}
	}
	return true
}
