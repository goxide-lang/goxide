package sourcemap

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"reflect"
	"strings"
	"testing"
)

type evidenceFixture struct {
	fs   *token.FileSet
	root *ast.File
	m    *Map
	e    *ASTEvidence
}

func evidenceStack(name string) []Expansion {
	return []Expansion{{Macro: name,
		Definition:         Span{File: "definitions.gox", Start: 10, End: 40, Line: 2, Column: 1},
		Callsite:           Span{File: "input.gox", Start: 50, End: 58, Line: 4, Column: 9},
		DefinitionOriginal: true, CallsiteOriginal: true}}
}

func evidenceInput(t *testing.T, source string) evidenceFixture {
	t.Helper()
	fs := token.NewFileSet()
	root, err := parser.ParseFile(fs, "input.gox", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	m := Identity("input.gox", []byte(source))
	for i := range m.Segments {
		m.Segments[i].Stack = evidenceStack("input")
	}
	return evidenceFixture{fs, root, m, m.CaptureAST(fs, root)}
}

func evidenceValue(root *ast.File) *ast.ValueSpec {
	return root.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec)
}

func evidenceRender(t *testing.T, fs *token.FileSet, root *ast.File) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := format.Node(&b, fs, root); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func evidencePrint(t *testing.T, f evidenceFixture) *Map {
	t.Helper()
	return f.e.Print(f.root, evidenceRender(t, f.fs, f.root), "test.print")
}

func evidenceAt(t *testing.T, m *Map, marker string, delta int) *Segment {
	t.Helper()
	i := bytes.Index(m.Source, []byte(marker))
	if i < 0 {
		t.Fatalf("missing %q in %s", marker, m.Source)
	}
	return m.Lookup(i + delta)
}

func evidenceExact(t *testing.T, got *Segment, want Span) {
	t.Helper()
	if got == nil || got.Origin == nil || *got.Origin != want {
		t.Fatalf("exact origin: got %+v, want %+v", got, want)
	}
}

func evidenceContextOnly(t *testing.T, got *Segment, want []Expansion) {
	t.Helper()
	if got == nil || got.Origin != nil || !reflect.DeepEqual(got.Stack, want) {
		t.Fatalf("context-only evidence: got %+v, want stack %+v", got, want)
	}
}

func evidenceUnknown(t *testing.T, got *Segment) {
	t.Helper()
	if got != nil && (got.Origin != nil || len(got.Stack) != 0) {
		t.Fatalf("unknown token acquired evidence: %+v", got)
	}
}

func TestASTEvidenceFrozenInputAndOutputCopies(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = source\n")
	id := evidenceValue(f.root).Values[0].(*ast.Ident)
	off := f.fs.Position(id.Pos()).Offset
	seg := f.m.Lookup(off)
	wantOrigin, wantStack := *seg.Origin, append([]Expansion(nil), seg.Stack...)
	seg.Origin.File = "mutated.gox"
	seg.Origin.Start = 999
	seg.Stack[0].Macro = "mutated"
	seg.Stack[0].Callsite.File = "mutated.gox"
	f.m.Source[off] = 'X'
	one := evidencePrint(t, f)
	got := evidenceAt(t, one, "= source", 2)
	evidenceExact(t, got, wantOrigin)
	if !reflect.DeepEqual(got.Stack, wantStack) {
		t.Fatalf("input stack was not frozen: %+v", got)
	}
	got.Origin.File = "output-mutated.gox"
	got.Stack[0].Macro = "output-mutated"
	two := evidencePrint(t, f)
	got = evidenceAt(t, two, "= source", 2)
	evidenceExact(t, got, wantOrigin)
	if !reflect.DeepEqual(got.Stack, wantStack) {
		t.Fatalf("output mutation corrupted snapshot: %+v", got)
	}
}

func TestASTEvidenceRenameAndPositionChanges(t *testing.T) {
	t.Run("rename retains only context", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = marker(arg)\n")
		call := evidenceValue(f.root).Values[0].(*ast.CallExpr)
		arg := call.Args[0].(*ast.Ident)
		wantArg := *f.m.Lookup(f.fs.Position(arg.Pos()).Offset).Origin
		call.Fun.(*ast.Ident).Name = "len"
		out := evidencePrint(t, f)
		evidenceContextOnly(t, evidenceAt(t, out, "len(arg)", 0), evidenceStack("input"))
		evidenceExact(t, evidenceAt(t, out, "len(arg)", 4), wantArg)
	})
	t.Run("new position cannot steal neighbor", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = left + right\n")
		binary := evidenceValue(f.root).Values[0].(*ast.BinaryExpr)
		left := binary.X.(*ast.Ident)
		want := *f.m.Lookup(f.fs.Position(left.Pos()).Offset).Origin
		left.NamePos = binary.Y.Pos()
		evidenceExact(t, evidenceAt(t, evidencePrint(t, f), "left + right", 0), want)
	})
}

func TestASTEvidenceFreshIdentityRequiresExplicitBinding(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = source\n")
	value := evidenceValue(f.root)
	old := value.Values[0].(*ast.Ident)
	fresh := &ast.Ident{Name: old.Name, NamePos: old.NamePos}
	ctx := f.e.Context(old, IdentToken)
	value.Values[0] = fresh
	evidenceUnknown(t, evidenceAt(t, evidencePrint(t, f), "= source", 2))
	if !f.e.Generated(fresh, ctx) {
		t.Fatal("explicit fresh-node binding rejected")
	}
	evidenceContextOnly(t, evidenceAt(t, evidencePrint(t, f), "= source", 2), evidenceStack("input"))
}

func TestASTEvidenceRegistrationConflictsAndTombstones(t *testing.T) {
	t.Run("same stack different handles conflict permanently", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = left + right\n")
		binary := evidenceValue(f.root).Values[0].(*ast.BinaryExpr)
		a, b := f.e.Context(binary.X, IdentToken), f.e.Context(binary.Y, IdentToken)
		fresh := ast.NewIdent("generated")
		if !f.e.Generated(fresh, a) || !f.e.Generated(fresh, a) {
			t.Fatal("same-handle registration must be idempotent")
		}
		if f.e.Generated(fresh, b) || f.e.Generated(fresh, a) {
			t.Fatal("conflicted node must never regain evidence")
		}
		binary.X = fresh
		evidenceUnknown(t, evidenceAt(t, evidencePrint(t, f), "generated + right", 0))
	})
	t.Run("unknown first binding is a tombstone", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = source\n")
		value := evidenceValue(f.root)
		ctx := f.e.Context(value.Values[0], IdentToken)
		fresh := ast.NewIdent("generated")
		if f.e.Generated(fresh, ASTContext{}) || f.e.Generated(fresh, ctx) {
			t.Fatal("unknown binding must not be upgraded")
		}
		value.Values[0] = fresh
		evidenceUnknown(t, evidenceAt(t, evidencePrint(t, f), "= generated", 2))
	})
	t.Run("input record cannot be overwritten", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = left + right\n")
		binary := evidenceValue(f.root).Values[0].(*ast.BinaryExpr)
		want := *f.m.Lookup(f.fs.Position(binary.X.Pos()).Offset).Origin
		if f.e.Generated(binary.X, f.e.Context(binary.Y, IdentToken)) {
			t.Fatal("input node accepted generated binding")
		}
		evidenceExact(t, evidenceAt(t, evidencePrint(t, f), "left + right", 0), want)
	})
}

func TestASTEvidenceForeignWitnessesAndNoPos(t *testing.T) {
	t.Run("foreign node and context", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = source\n")
		foreign := evidenceInput(t, "package p\nvar out = source\n")
		other := evidenceValue(foreign.root).Values[0]
		if f.e.Generated(ast.NewIdent("a"), f.e.Context(other, IdentToken)) {
			t.Fatal("foreign node acquired local context")
		}
		if f.e.Generated(ast.NewIdent("b"), foreign.e.Context(other, IdentToken)) {
			t.Fatal("foreign registry context accepted")
		}
	})
	t.Run("NoPos input remains unknown", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = source\n")
		id := evidenceValue(f.root).Values[0].(*ast.Ident)
		id.NamePos = token.NoPos
		f.e = f.m.CaptureAST(f.fs, f.root)
		if f.e.Generated(ast.NewIdent("new"), f.e.Context(id, IdentToken)) {
			t.Fatal("NoPos node acquired context")
		}
		evidenceUnknown(t, evidenceAt(t, evidencePrint(t, f), "= source", 2))
	})
}

func TestASTEvidenceGeneratedTreeRejectsMixedRecords(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = left + right\n")
	value := evidenceValue(f.root)
	old := value.Values[0].(*ast.BinaryExpr)
	left := old.X
	want := *f.m.Lookup(f.fs.Position(left.Pos()).Offset).Origin
	ctx := f.e.Context(old.Y, IdentToken)
	fresh := ast.NewIdent("fresh")
	mixed := &ast.BinaryExpr{X: left, Op: token.ADD, Y: fresh}
	if f.e.GeneratedTree(mixed, ctx) {
		t.Fatal("mixed tree was accepted")
	}
	if f.e.Generated(fresh, ctx) || f.e.Generated(mixed, ctx) {
		t.Fatal("rejected tree's new nodes were not tombstoned")
	}
	value.Values[0] = mixed
	out := evidencePrint(t, f)
	evidenceExact(t, evidenceAt(t, out, "left + fresh", 0), want)
	evidenceUnknown(t, evidenceAt(t, out, "left + fresh", 7))
}

func TestASTEvidenceRenameCannotRoundTripBackToOrigin(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = source\n")
	id := evidenceValue(f.root).Values[0].(*ast.Ident)
	id.Name = "changed"
	evidenceContextOnly(t, evidenceAt(t, evidencePrint(t, f), "= changed", 2), evidenceStack("input"))
	id.Name = "source"
	evidenceContextOnly(t, evidenceAt(t, evidencePrint(t, f), "= source", 2), evidenceStack("input"))
}

func TestASTEvidenceInputUnknownCannotBeRebound(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = left + right\n")
	binary := evidenceValue(f.root).Values[0].(*ast.BinaryExpr)
	seg := f.m.Lookup(f.fs.Position(binary.Y.Pos()).Offset)
	seg.Origin, seg.Stack = nil, nil
	f.e = f.m.CaptureAST(f.fs, f.root)
	if f.e.Generated(binary.Y, f.e.Context(binary.X, IdentToken)) {
		t.Fatal("captured unknown input was rebound")
	}
	evidenceUnknown(t, evidenceAt(t, evidencePrint(t, f), "left + right", 7))
}

func TestASTEvidenceGeneratedTreeRejectsUnknownInput(t *testing.T) {
	f := evidenceInput(t, "package p; var out = left + right")
	value := evidenceValue(f.root)
	old := value.Values[0].(*ast.BinaryExpr)
	segment := f.m.Lookup(f.fs.Position(old.Y.Pos()).Offset)
	segment.Origin, segment.Stack = nil, nil
	f.e = f.m.CaptureAST(f.fs, f.root)
	ctx := f.e.Context(old.X, IdentToken)
	fresh := ast.NewIdent("fresh")
	mixed := &ast.BinaryExpr{X: old.Y, Op: token.ADD, Y: fresh}
	if f.e.GeneratedTree(mixed, ctx) || f.e.Generated(mixed, ctx) || f.e.Generated(fresh, ctx) {
		t.Fatal("captured unknown input accepted a tree binding or lost its new-node tombstones")
	}
	value.Values[0] = mixed
	out := evidencePrint(t, f)
	for _, delta := range []int{0, 6, 8} {
		evidenceUnknown(t, evidenceAt(t, out, "right + fresh", delta))
	}
}

func TestASTEvidenceGeneratedTreeBindsOnlyFreshTree(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = 42\n")
	value := evidenceValue(f.root)
	literal := value.Values[0].(*ast.BasicLit)
	if f.e.Generated(ast.NewIdent("wrongslot"), f.e.Context(literal, IdentToken)) {
		t.Fatal("literal accepted an identifier slot")
	}
	fresh := &ast.BinaryExpr{X: ast.NewIdent("first"), Op: token.ADD, Y: ast.NewIdent("second")}
	if !f.e.GeneratedTree(fresh, f.e.Context(literal, LiteralToken)) {
		t.Fatal("entirely fresh tree rejected")
	}
	value.Values[0] = fresh
	out := evidencePrint(t, f)
	for _, delta := range []int{0, 6, 8} {
		evidenceContextOnly(t, evidenceAt(t, out, "first + second", delta), evidenceStack("input"))
	}
}

func TestASTEvidenceEmptyStackDoesNotAuthorizeGeneration(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = source\n")
	id := evidenceValue(f.root).Values[0]
	seg := f.m.Lookup(f.fs.Position(id.Pos()).Offset)
	want := *seg.Origin
	seg.Stack = nil
	f.e = f.m.CaptureAST(f.fs, f.root)
	if f.e.Generated(ast.NewIdent("generated"), f.e.Context(id, IdentToken)) {
		t.Fatal("empty stack was treated as transferable known context")
	}
	evidenceExact(t, evidenceAt(t, evidencePrint(t, f), "= source", 2), want)
}

func TestASTEvidenceGeneratedTreePreservesGeneratedChild(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = left + right\n")
	value := evidenceValue(f.root)
	old := value.Values[0].(*ast.BinaryExpr)
	f.m.Lookup(f.fs.Position(old.X.Pos()).Offset).Stack = evidenceStack("left")
	f.m.Lookup(f.fs.Position(old.Y.Pos()).Offset).Stack = evidenceStack("right")
	f.e = f.m.CaptureAST(f.fs, f.root)
	a, b := f.e.Context(old.X, IdentToken), f.e.Context(old.Y, IdentToken)
	child, fresh := ast.NewIdent("bound"), ast.NewIdent("fresh")
	if !f.e.Generated(child, a) {
		t.Fatal("child binding rejected")
	}
	root := &ast.BinaryExpr{X: child, Op: token.ADD, Y: fresh}
	if f.e.GeneratedTree(root, b) || f.e.Generated(fresh, b) {
		t.Fatal("mixed generated tree accepted or its tombstone was lost")
	}
	value.Values[0] = root
	out := evidencePrint(t, f)
	evidenceContextOnly(t, evidenceAt(t, out, "bound + fresh", 0), evidenceStack("left"))
	evidenceUnknown(t, evidenceAt(t, out, "bound + fresh", 6))
	evidenceUnknown(t, evidenceAt(t, out, "bound + fresh", 8))
}

func TestASTEvidenceOwnSlotsDoNotBorrowChildren(t *testing.T) {
	t.Run("binary operator owns its own context", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = left == right\n")
		value := evidenceValue(f.root)
		binary := value.Values[0].(*ast.BinaryExpr)
		f.m.Lookup(f.fs.Position(binary.X.Pos()).Offset).Stack = evidenceStack("left")
		f.m.Lookup(f.fs.Position(binary.OpPos).Offset).Stack = evidenceStack("operator")
		f.m.Lookup(f.fs.Position(binary.Y.Pos()).Offset).Stack = evidenceStack("right")
		f.e = f.m.CaptureAST(f.fs, f.root)
		if f.e.Generated(ast.NewIdent("bad"), f.e.Context(binary, IdentToken)) {
			t.Fatal("binary inherited its lhs identifier slot")
		}
		generated := ast.NewIdent("generated")
		if !f.e.Generated(generated, f.e.Context(binary, OperatorToken)) {
			t.Fatal("two-byte operator slot rejected")
		}
		value.Values[0] = generated
		evidenceContextOnly(t, evidenceAt(t, evidencePrint(t, f), "= generated", 2), evidenceStack("operator"))
	})
	t.Run("call parentheses are distinct from callee and argument", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = callee(arg)\n")
		value := evidenceValue(f.root)
		call := value.Values[0].(*ast.CallExpr)
		for _, slot := range []struct {
			pos  token.Pos
			name string
		}{{call.Fun.Pos(), "callee"}, {call.Lparen, "open"}, {call.Rparen, "close"}, {call.Args[0].Pos(), "argument"}} {
			f.m.Lookup(f.fs.Position(slot.pos).Offset).Stack = evidenceStack(slot.name)
		}
		f.e = f.m.CaptureAST(f.fs, f.root)
		if f.e.Generated(ast.NewIdent("bad"), f.e.Context(call, IdentToken)) || f.e.Generated(ast.NewIdent("badop"), f.e.Context(call, OperatorToken)) {
			t.Fatal("call inherited a child slot")
		}
		left, right := ast.NewIdent("opening"), ast.NewIdent("closing")
		if !f.e.Generated(left, f.e.Context(call, LeftParenToken)) || !f.e.Generated(right, f.e.Context(call, RightParenToken)) {
			t.Fatal("call parenthesis context rejected")
		}
		wrapper := &ast.CallExpr{Fun: ast.NewIdent("wrapper"), Args: []ast.Expr{call.Args[0], left, right}}
		if !f.e.Generated(wrapper, f.e.Context(call, LeftParenToken)) {
			t.Fatal("explicit call node registration rejected")
		}
		value.Values[0] = wrapper
		out := evidencePrint(t, f)
		evidenceUnknown(t, evidenceAt(t, out, "wrapper(", 0))
		evidenceContextOnly(t, evidenceAt(t, out, "wrapper(", 7), evidenceStack("open"))
		argument := evidenceAt(t, out, "(arg,", 1)
		if argument == nil || argument.Origin == nil || !reflect.DeepEqual(argument.Stack, evidenceStack("argument")) {
			t.Fatalf("wrapper overrode source argument: %+v", argument)
		}
		evidenceContextOnly(t, evidenceAt(t, out, "opening", 0), evidenceStack("open"))
		evidenceContextOnly(t, evidenceAt(t, out, "closing", 0), evidenceStack("close"))
	})
}

func TestASTEvidencePartialTokenDoesNotAuthorizeContext(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = left == right\n")
	binary := evidenceValue(f.root).Values[0].(*ast.BinaryExpr)
	seg := f.m.Lookup(f.fs.Position(binary.OpPos).Offset)
	seg.End--
	f.e = f.m.CaptureAST(f.fs, f.root)
	if f.e.Generated(ast.NewIdent("generated"), f.e.Context(binary, OperatorToken)) {
		t.Fatal("partial operator token authorized a context")
	}
}

func TestASTEvidenceActualOperatorMutationRetainsOnlyContext(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = left + right\n")
	binary := evidenceValue(f.root).Values[0].(*ast.BinaryExpr)
	wantLeft := *f.m.Lookup(f.fs.Position(binary.X.Pos()).Offset).Origin
	f.m.Lookup(f.fs.Position(binary.OpPos).Offset).Stack = evidenceStack("operator")
	f.e = f.m.CaptureAST(f.fs, f.root)
	binary.Op = token.SUB
	out := evidencePrint(t, f)
	evidenceContextOnly(t, evidenceAt(t, out, "left - right", 5), evidenceStack("operator"))
	evidenceExact(t, evidenceAt(t, out, "left - right", 0), wantLeft)
}

func TestASTEvidenceRejectsStructuralMismatches(t *testing.T) {
	for _, tc := range []struct {
		name, before, after, marker string
		delta                       int
	}{
		{"operator", "package p; var out = left + right", "package p; var out = left - right", "left - right", 5},
		{"field role", "package p; func f(){for i = 0; ; {}}", "package p; func f(){for ; ; i = 0 {}}", "i = 0", 0},
		{"ellipsis", "package p; var out = f(xs...)", "package p; var out = f(xs)", "f(xs)", 1},
		{"alias", "package p; type T = U", "package p; type T U", "T U", 0},
		{"channel direction", "package p; var out chan<- T", "package p; var out <-chan T", "<-chan", 0},
		{"range assignment", "package p; func f(){for k := range xs {}}", "package p; func f(){for k = range xs {}}", "k = range", 2},
		{"concrete leaf type", "package p; var out = item", "package p; var out = 1234", "1234", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := evidenceInput(t, tc.before)
			out := f.e.Print(f.root, []byte(tc.after), "test.mismatch")
			evidenceUnknown(t, evidenceAt(t, out, tc.marker, tc.delta))
		})
	}
	t.Run("literal kind even when spelling is identical", func(t *testing.T) {
		f := evidenceInput(t, "package p; var out = 1")
		literal := evidenceValue(f.root).Values[0].(*ast.BasicLit)
		literal.Kind = token.FLOAT
		out := f.e.Print(f.root, []byte("package p; var out = 1"), "test.mismatch")
		evidenceUnknown(t, evidenceAt(t, out, "= 1", 2))
	})
	t.Run("Slice3 is not redundant metadata", func(t *testing.T) {
		f := evidenceInput(t, "package p; var out = xs[lo:hi]")
		evidenceValue(f.root).Values[0].(*ast.SliceExpr).Slice3 = true
		out := f.e.Print(f.root, []byte("package p; var out = xs[lo:hi]"), "test.mismatch")
		evidenceUnknown(t, evidenceAt(t, out, "xs[lo:hi]", 2))
	})
}

func TestASTEvidenceDuplicateLiteralsFollowCurrentNodeOrder(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = f(\"same\", \"same\")\n")
	call := evidenceValue(f.root).Values[0].(*ast.CallExpr)
	first, second := call.Args[0], call.Args[1]
	firstSeg := f.m.Lookup(f.fs.Position(first.Pos()).Offset)
	secondSeg := f.m.Lookup(f.fs.Position(second.Pos()).Offset)
	firstSeg.Stack, secondSeg.Stack = evidenceStack("first"), evidenceStack("second")
	wantFirst, wantSecond := *firstSeg.Origin, *secondSeg.Origin
	f.e = f.m.CaptureAST(f.fs, f.root)
	call.Args[0], call.Args[1] = second, first
	out := evidencePrint(t, f)
	start := strings.Index(string(out.Source), "\"same\"")
	end := strings.LastIndex(string(out.Source), "\"same\"")
	if start < 0 || start == end {
		t.Fatalf("bad duplicate fixture: %s", out.Source)
	}
	evidenceExact(t, out.Lookup(start), wantSecond)
	evidenceExact(t, out.Lookup(end), wantFirst)
	if !reflect.DeepEqual(out.Lookup(start).Stack, evidenceStack("second")) || !reflect.DeepEqual(out.Lookup(end).Stack, evidenceStack("first")) {
		t.Fatal("duplicate literal stacks followed old positions rather than current pointers")
	}
}

func TestASTEvidenceParenthesisChains(t *testing.T) {
	t.Run("unchanged pair retains both own slots", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = (value)\n")
		paren := evidenceValue(f.root).Values[0].(*ast.ParenExpr)
		left := *f.m.Lookup(f.fs.Position(paren.Lparen).Offset).Origin
		right := *f.m.Lookup(f.fs.Position(paren.Rparen).Offset).Origin
		out := evidencePrint(t, f)
		evidenceExact(t, evidenceAt(t, out, "(value)", 0), left)
		evidenceExact(t, evidenceAt(t, out, "(value)", 6), right)
	})
	t.Run("collapsed chain cannot pick either pair", func(t *testing.T) {
		f := evidenceInput(t, "package p\nvar out = ((value))\n")
		outer := evidenceValue(f.root).Values[0].(*ast.ParenExpr)
		inner := outer.X.(*ast.ParenExpr)
		for _, p := range []token.Pos{outer.Lparen, outer.Rparen} {
			f.m.Lookup(f.fs.Position(p).Offset).Stack = evidenceStack("outer")
		}
		for _, p := range []token.Pos{inner.Lparen, inner.Rparen} {
			f.m.Lookup(f.fs.Position(p).Offset).Stack = evidenceStack("inner")
		}
		want := *f.m.Lookup(f.fs.Position(inner.X.Pos()).Offset).Origin
		f.e = f.m.CaptureAST(f.fs, f.root)
		out := evidencePrint(t, f)
		evidenceUnknown(t, evidenceAt(t, out, "(value)", 0))
		evidenceUnknown(t, evidenceAt(t, out, "(value)", 6))
		evidenceExact(t, evidenceAt(t, out, "(value)", 1), want)
	})
}

func TestASTEvidencePrinterNormalizations(t *testing.T) {
	for _, tc := range []struct {
		name, source, marker string
		change               func(*ast.FuncDecl)
	}{
		{"nil parameters", "package p; func f(){}", "f()", func(fn *ast.FuncDecl) { fn.Type.Params = nil }},
		{"empty results", "package p; func f(){}", "f()", func(fn *ast.FuncDecl) { fn.Type.Results = &ast.FieldList{} }},
		{"NoPos func", "package p; func f(){}", "f()", func(fn *ast.FuncDecl) { fn.Type.Func = token.NoPos }},
		{"direct empty statements", "package p; func f(){; kept(); ;}", "kept()", nil},
		{"labeled explicit empty", "package p; func f(){label:;}", "label:", nil},
		{"anonymous result parentheses", "package p; func f() (int) { return 0 }", "int", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := evidenceInput(t, tc.source)
			at := strings.Index(tc.source, tc.marker)
			want := *f.m.Lookup(at).Origin
			if tc.change != nil {
				tc.change(f.root.Decls[0].(*ast.FuncDecl))
			}
			evidenceExact(t, evidenceAt(t, evidencePrint(t, f), tc.marker, 0), want)
		})
	}
}

func TestASTEvidenceReparseNeverUpgradesGeneratedOrigin(t *testing.T) {
	f := evidenceInput(t, "package p\nvar out = left + right\n")
	binary := evidenceValue(f.root).Values[0].(*ast.BinaryExpr)
	ctx := f.e.Context(binary.X, IdentToken)
	generated, unknown := ast.NewIdent("generated"), ast.NewIdent("unknown")
	if !f.e.Generated(generated, ctx) {
		t.Fatal("generated binding rejected")
	}
	binary.X, binary.Y = generated, unknown
	first := evidencePrint(t, f)
	evidenceContextOnly(t, evidenceAt(t, first, "generated + unknown", 0), evidenceStack("input"))
	evidenceUnknown(t, evidenceAt(t, first, "generated + unknown", 12))
	fs := token.NewFileSet()
	root, err := parser.ParseFile(fs, "round2.go", first.Source, 0)
	if err != nil {
		t.Fatal(err)
	}
	e := first.CaptureAST(fs, root)
	secondBinary := evidenceValue(root).Values[0].(*ast.BinaryExpr)
	if e.Generated(secondBinary.Y, e.Context(secondBinary.X, IdentToken)) {
		t.Fatal("reparsed unknown input accepted fallback binding")
	}
	second := e.Print(root, evidenceRender(t, fs, root), "test.reparse")
	evidenceContextOnly(t, evidenceAt(t, second, "generated + unknown", 0), evidenceStack("input"))
	evidenceUnknown(t, evidenceAt(t, second, "generated + unknown", 12))
}

func TestASTEvidenceRemovedSlotCannotRegainOrigin(t *testing.T) {
	t.Run("alias assignment", func(t *testing.T) {
		f := evidenceInput(t, "package p; type T = U")
		spec := f.root.Decls[0].(*ast.GenDecl).Specs[0].(*ast.TypeSpec)
		position := spec.Assign
		spec.Assign = token.NoPos
		evidencePrint(t, f)
		spec.Assign = position
		evidenceContextOnly(t, evidenceAt(t, evidencePrint(t, f), "T = U", 2), evidenceStack("input"))
	})
	t.Run("call ellipsis", func(t *testing.T) {
		f := evidenceInput(t, "package p; var out = f(xs...)")
		call := evidenceValue(f.root).Values[0].(*ast.CallExpr)
		position := call.Ellipsis
		call.Ellipsis = token.NoPos
		evidencePrint(t, f)
		call.Ellipsis = position
		evidenceContextOnly(t, evidenceAt(t, evidencePrint(t, f), "xs...", 2), evidenceStack("input"))
	})
}

func TestASTEvidenceRequiresWholeUncoveredTokenSegment(t *testing.T) {
	for _, mode := range []string{"wide segment", "internal unknown overlay", "internal known overlay"} {
		t.Run(mode, func(t *testing.T) {
			f := evidenceInput(t, "package p; var out = source")
			id := evidenceValue(f.root).Values[0]
			start := f.fs.Position(id.Pos()).Offset
			seg := f.m.Lookup(start)
			switch mode {
			case "wide segment":
				seg.Start--
			case "internal unknown overlay":
				f.m.Attach(start+2, start+3, nil, nil, "generated/unmapped")
			case "internal known overlay":
				f.m.Attach(start+2, start+3, seg.Origin, evidenceStack("other"), "macro")
			}
			f.e = f.m.CaptureAST(f.fs, f.root)
			bound := f.e.Generated(ast.NewIdent("generated"), f.e.Context(id, IdentToken))
			out := evidenceAt(t, evidencePrint(t, f), "= source", 2)
			if mode == "wide segment" {
				if !bound {
					t.Fatal("the single complete covering segment lost its proven context")
				}
				evidenceContextOnly(t, out, evidenceStack("input"))
			} else {
				if bound {
					t.Fatal("a partially overridden segment authorized token context")
				}
				evidenceUnknown(t, out)
			}
		})
	}
}

func TestASTEvidenceEmptyOverlayDoesNotHideToken(t *testing.T) {
	for _, width := range []int{0, -1} {
		t.Run(map[int]string{0: "zero width", -1: "reversed"}[width], func(t *testing.T) {
			f := evidenceInput(t, "package p; var out = source")
			id := evidenceValue(f.root).Values[0]
			start := f.fs.Position(id.Pos()).Offset
			want := *f.m.Lookup(start).Origin
			f.m.Attach(start+2, start+2+width, nil, nil, "generated/unmapped")
			f.e = f.m.CaptureAST(f.fs, f.root)
			if !f.e.Generated(ast.NewIdent("generated"), f.e.Context(id, IdentToken)) {
				t.Fatal("an empty overlay hid the token's complete context")
			}
			out := evidenceAt(t, evidencePrint(t, f), "= source", 2)
			evidenceExact(t, out, want)
			if !reflect.DeepEqual(out.Stack, evidenceStack("input")) {
				t.Fatal("an empty overlay changed the token's context")
			}
		})
	}
}

func TestASTEvidenceSourceSpanNeedNotHaveGeneratedWidth(t *testing.T) {
	f := evidenceInput(t, "package p; var out = source")
	id := evidenceValue(f.root).Values[0]
	want := Span{File: "definitions.gox", Start: 10, End: 11, Line: 2, Column: 1}
	f.m.Lookup(f.fs.Position(id.Pos()).Offset).Origin = &want
	f.e = f.m.CaptureAST(f.fs, f.root)
	evidenceExact(t, evidenceAt(t, evidencePrint(t, f), "= source", 2), want)
}

func TestASTEvidenceInvalidPointDoesNotInvalidateContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		span Span
	}{
		{"missing file", Span{Start: 10, End: 11, Line: 2, Column: 1}},
		{"negative start", Span{File: "input.gox", Start: -1, End: 11, Line: 2, Column: 1}},
		{"empty range", Span{File: "input.gox", Start: 10, End: 10, Line: 2, Column: 1}},
		{"inverted range", Span{File: "input.gox", Start: 11, End: 10, Line: 2, Column: 1}},
		{"missing line", Span{File: "input.gox", Start: 10, End: 11, Column: 1}},
		{"missing column", Span{File: "input.gox", Start: 10, End: 11, Line: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := evidenceInput(t, "package p; var out = source")
			id := evidenceValue(f.root).Values[0]
			f.m.Lookup(f.fs.Position(id.Pos()).Offset).Origin = &tc.span
			f.e = f.m.CaptureAST(f.fs, f.root)
			if !f.e.Generated(ast.NewIdent("generated"), f.e.Context(id, IdentToken)) {
				t.Fatal("invalid point discarded independently proven context")
			}
			evidenceContextOnly(t, evidenceAt(t, evidencePrint(t, f), "= source", 2), evidenceStack("input"))
		})
	}
}

func TestASTEvidenceFailedPrintStillObservesRewrite(t *testing.T) {
	f := evidenceInput(t, "package p; var out = source")
	id := evidenceValue(f.root).Values[0].(*ast.Ident)
	id.Name = "changed"
	f.e.Print(f.root, []byte("not a Go file"), "test.invalid")
	id.Name = "source"
	evidenceContextOnly(t, evidenceAt(t, evidencePrint(t, f), "= source", 2), evidenceStack("input"))
}
