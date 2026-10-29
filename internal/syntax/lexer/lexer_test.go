package lexer

import (
	"reflect"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/source"
	"github.com/goxide-lang/goxide/internal/syntax/token"
)

func kinds(ts []token.Token) []token.Kind {
	ks := make([]token.Kind, len(ts))
	for i, t := range ts {
		ks[i] = t.Kind
	}
	return ks
}

func TestCRLFCommentAndSemicolonRanges(t *testing.T) {
	const text = "x // 中\r\ny"
	tokens, diagnostics := Lex([]byte(text))
	if len(diagnostics) != 0 {
		t.Fatal(diagnostics)
	}
	comment, semicolon := tokens[1], tokens[2]
	if comment.Kind != token.COMMENT || comment.Text != "// 中" || comment.Start != 2 || comment.End != 8 {
		t.Fatalf("comment includes line ending: %+v", comment)
	}
	if semicolon.Kind != token.SEMICOLON || !semicolon.Implicit || semicolon.Start != 8 || semicolon.End != 8 {
		t.Fatalf("semicolon splits CRLF: %+v", semicolon)
	}
	index := source.New(text)
	for _, token := range tokens {
		if _, err := index.Range(int(token.Start), int(token.End), source.UTF16); err != nil {
			t.Fatalf("token %v has an unrepresentable source range: %v", token, err)
		}
	}
}

func TestNewTokensAndLongestMatch(t *testing.T) {
	src := ":: => ? # $ ~ != := : = > ! &^= >>= ..."
	got, ds := Lex([]byte(src))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	want := []token.Kind{token.DCOLON, token.FATARROW, token.QUESTION, token.HASH, token.DOLLAR, token.TILDE, token.NEQ, token.DEFINE, token.COLON, token.ASSIGN, token.GTR, token.NOT, token.AND_NOT_ASSIGN, token.SHR_ASSIGN, token.ELLIPSIS, token.EOF}
	if !reflect.DeepEqual(kinds(got), want) {
		t.Fatalf("got %v, want %v", kinds(got), want)
	}
	for _, tok := range got[:len(got)-1] {
		if tok.Text != src[tok.Start:tok.End] {
			t.Errorf("incorrect span: %+v", tok)
		}
	}
}

func TestRawIdentifiers(t *testing.T) {
	for _, name := range []string{"match", "Self", "func", "async", "where", "你好", "_"} {
		src := "r#" + name
		ts, ds := Lex([]byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		if ts[0].Kind != token.IDENT || ts[0].Text != name || !ts[0].Raw || ts[0].Start != 0 || ts[0].End != uint32(len(src)) {
			t.Errorf("%s: %+v", src, ts[0])
		}
		if ts[1].Kind != token.SEMICOLON || !ts[1].Implicit {
			t.Errorf("raw identifier must end a statement: %v", ts)
		}
	}
}

func TestContextualWordsStayIdentifiers(t *testing.T) {
	ts, ds := Lex([]byte("func (self *T) M(where int, yield func(int) bool) { yield(where) }"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tok := range ts {
		if tok.Text == "self" || tok.Text == "where" || tok.Text == "yield" {
			if tok.Kind != token.IDENT {
				t.Errorf("ordinary identifier: %+v", tok)
			}
		}
	}
	ts, ds = Lex([]byte("mut ref as where self super in macro_rules async await let fn loop while macro"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	for _, tok := range ts[:len(ts)-2] {
		if tok.Kind != token.IDENT {
			t.Errorf("reserved tier incorrectly promoted: %+v", tok)
		}
	}
}

func TestAutomaticSemicolons(t *testing.T) {
	for _, src := range []string{"x\n", "123\n", "2.3\n", "2i\n", "'x'\n", "\"x\"\n", "break\n", "continue\n", "fallthrough\n", "return\n", "x++\n", "x--\n", "f()\n", "a[0]\n", "{}\n", "Self\n", "x", "T::from::<From[S]>\n", "value.into::<Into[T]>", "value.into::/*comment*/<Into[T]>/*tail\n*/"} {
		ts, ds := Lex([]byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		if last := ts[len(ts)-2]; last.Kind != token.SEMICOLON || !last.Implicit {
			t.Errorf("%q: no implicit semicolon: %v", src, ts)
		}
	}
	for _, src := range []string{"x +\n", "a,\n", "func\n", "if\n", "x;\n", "x >\n", "x >>\n", "T::from::<From[S]> >\n"} {
		ts, ds := Lex([]byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		for _, tok := range ts {
			if tok.Implicit {
				t.Errorf("%q: unexpected semicolon: %v", src, tok)
			}
		}
	}
}

func TestSupplementarySemicolons(t *testing.T) {
	t.Run("postfix question", func(t *testing.T) {
		ts, ds := Lex([]byte("x := f()?\ny := 1"))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		for i, tok := range ts {
			if tok.Kind == token.QUESTION && (ts[i+1].Kind != token.SEMICOLON || !ts[i+1].Implicit) {
				t.Fatal(ts)
			}
		}
	})
	t.Run("attribute closing bracket", func(t *testing.T) {
		ts, ds := Lex([]byte("#[derive(a([1], { x: [2] }))]\n#[cfg(feature = \"x\")]\nstruct S {}\na[0]\n"))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		semis := 0
		for _, tok := range ts {
			if tok.Implicit {
				semis++
			}
		}
		if semis != 2 {
			t.Fatalf("want semicolons only after declaration and indexing, got %v", ts)
		}
	})
	t.Run("last match arm", func(t *testing.T) {
		ts, ds := Lex([]byte("match x {\n A => 1,\n B => 2\n}"))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		for i, tok := range ts {
			if tok.Text == "2" && (ts[i+1].Kind != token.SEMICOLON || !ts[i+1].Implicit || ts[i+2].Kind != token.RBRACE) {
				t.Fatal(ts)
			}
		}
	})
}

func TestCommentsAndSemicolons(t *testing.T) {
	for _, src := range []string{"x // comment\ny", "x /* comment\ncomment */ y", "x /* comment */\ny"} {
		ts, ds := Lex([]byte(src))
		if len(ds) != 0 {
			t.Fatal(ds)
		}
		want := []token.Kind{token.IDENT, token.COMMENT, token.SEMICOLON, token.IDENT, token.SEMICOLON, token.EOF}
		if !reflect.DeepEqual(kinds(ts), want) {
			t.Errorf("%q: got %v", src, kinds(ts))
		}
	}
	ts, _ := Lex([]byte("#[a] /* comment\ncomment */\nfunc F() {}"))
	for i, tok := range ts {
		if tok.Kind == token.COMMENT && ts[i+1].Kind == token.SEMICOLON {
			t.Fatal("attribute newline must not insert semicolon")
		}
	}
}

func TestCommentAndUnicodePositions(t *testing.T) {
	ts, ds := Lex([]byte("α/*\n*/β"))
	if len(ds) != 0 {
		t.Fatal(ds)
	}
	want := []struct {
		kind       token.Kind
		start, end uint32
	}{
		{token.IDENT, 0, 2}, {token.COMMENT, 2, 7},
		{token.SEMICOLON, 4, 4}, {token.IDENT, 7, 9},
		{token.SEMICOLON, 9, 9}, {token.EOF, 9, 9},
	}
	if len(ts) != len(want) {
		t.Fatal(ts)
	}
	for i, w := range want {
		if got := ts[i]; got.Kind != w.kind || got.Start != w.start || got.End != w.end {
			t.Errorf("token %d: got %+v, want %+v", i, got, w)
		}
	}
	ts, ds = Lex([]byte("\ufeffα\r\n"))
	if len(ds) != 0 || ts[0].Start != 3 || ts[0].End != 5 || ts[1].Start != 5 || ts[1].End != 5 || !ts[1].Implicit {
		t.Fatalf("BOM and CRLF byte positions: %v, %v", ts, ds)
	}
	for _, src := range []string{"//\xff\n", "/*\ufeff*/", "`\x00`", "\"\xff\""} {
		if _, ds := Lex([]byte(src)); len(ds) == 0 {
			t.Errorf("source encoding error hidden inside comment or literal: %q", src)
		}
	}
}

func TestNumericLiterals(t *testing.T) {
	valid := map[token.Kind][]string{
		token.INT:   {"0", "42", "0600", "0_600", "0b1010", "0B_1010", "0o600", "0O_600", "0xBadFace", "0X_Bad_Face", "1_000"},
		token.FLOAT: {"0.", "72.40", "072.40", ".25", "1e6", "1.e+0", "1_5.0_0e+0_2", "0x1p-2", "0x1.fp+2", "0X.8p-0", "0x_1.ff_fp+0_2"},
		token.IMAG:  {"0i", "0129i", "0o123i", "0xabci", "0.i", ".5i", "1e3i", "0x1p-2i"},
	}
	for want, literals := range valid {
		for _, src := range literals {
			ts, ds := Lex([]byte(src))
			if len(ds) != 0 || ts[0].Kind != want || ts[0].Text != src {
				t.Errorf("%s: got %v, diagnostics %v", src, ts, ds)
			}
		}
	}
	for _, src := range []string{"0x", "0b2", "0o9", "08", "1e", "1p2", "0b1.0", "0o1e2", "0x1.2", "1__2", "1_", "1_.0", "1._0", "1e_2", "0x__1", "0x1p_2"} {
		if _, ds := Lex([]byte(src)); len(ds) == 0 {
			t.Errorf("%s: expected diagnostic", src)
		}
	}
}

func TestLiteralsAndUnicode(t *testing.T) {
	for _, src := range []string{"\"hello\\n\\u4e16\\U0001f600\"", "'世'", "'\\x41'", "`raw\nstring\r`", "世界 := 1", "\ufeffpackage p"} {
		if _, ds := Lex([]byte(src)); len(ds) != 0 {
			t.Errorf("%q: %v", src, ds)
		}
	}
	for _, src := range []string{"'ab'", "''", "'\\uD800'", "\"\\q\"", "\"line\nnext", "`unclosed", "/* unclosed", "\x00", "x\ufeff", "\xff"} {
		if _, ds := Lex([]byte(src)); len(ds) == 0 {
			t.Errorf("%q: expected diagnostics", src)
		}
	}
}

func TestErrorRecovery(t *testing.T) {
	src := "@\n\"unterminated\nfunc after() {}"
	ts, ds := Lex([]byte(src))
	if len(ds) < 2 {
		t.Fatal(ds)
	}
	found := false
	for _, tok := range ts {
		if tok.Kind == token.IDENT && tok.Text == "after" {
			found = true
		}
	}
	if !found || ts[len(ts)-1].Kind != token.EOF {
		t.Fatal("lexing did not recover", ts)
	}
	for _, d := range ds {
		if d.Start > d.End || int(d.End) > len(src) || strings.TrimSpace(d.Message) == "" {
			t.Fatalf("invalid diagnostic: %+v", d)
		}
	}
}
