package equality

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestEqualityProvenancePreservesUnchangedMacroCalls(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		for _, eq := range []bool{false, true} {
			name := "PartialEq"
			if eq {
				name += "+Eq"
			}
			t.Run(name+strings.ReplaceAll(newline, "\n", "LF"), func(t *testing.T) {
				source := []byte(strings.ReplaceAll("package fixture\ntype T struct{Value int}\nfunc zero()int{return int(0)}\n", "\n", newline))
				m := sourcemap.Identity("input.gox", source)
				pos := strings.Index(string(source), "int(0)")
				origin := sourcemap.Position("input.gox", source, pos, pos+3)
				stack := equalityProvenanceStack("zero", 40)
				m.Attach(pos, pos+3, &origin, stack, "macro template")
				cfg := Config{Derives: map[string]Derive{"T": {PartialEq: true, Eq: eq}}, Maps: map[string]*sourcemap.Map{"input.gox": m}}
				out, err := Prepare(map[string][]byte{"input.gox": source}, cfg)
				if err != nil {
					t.Fatal(err)
				}
				calls := equalityProvenanceCalls(t, out["input.gox"], cfg.Maps["input.gox"], "int")
				if len(calls) != 1 {
					t.Fatalf("want one unchanged call, got %d", len(calls))
				}
				if calls[0] == nil || calls[0].Origin == nil || *calls[0].Origin != origin || !reflect.DeepEqual(calls[0].Stack, stack) {
					t.Fatalf("derive erased an untouched macro call: %+v", calls[0])
				}
			})
		}
	}
}

func TestEqualityProvenanceGeneratedCallsAndHelperBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, receiver, prepared, lowered string
		derive                            map[string]Derive
	}{
		{"derived", "T{1}", "EqualsT", "EqualsT", map[string]Derive{"T": {PartialEq: true, Eq: true}}},
		{"scalar", "int(1)", intrinsic, "__goxide_enum_scalar_equals", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package fixture\ntype T struct{Value int}\nfunc first()bool{return " + tc.receiver + ".equals(" + tc.receiver + ")}\nfunc second()bool{return " + tc.receiver + ".equals(" + tc.receiver + ")}\nfunc plain()bool{return " + tc.receiver + ".equals(" + tc.receiver + ")}\n")
			m := sourcemap.Identity("input.gox", source)
			stacks := [][]sourcemap.Expansion{equalityProvenanceStack("first", 50), equalityProvenanceStack("second", 80), nil}
			search := 0
			for i, stack := range stacks {
				pos := search + strings.Index(string(source[search:]), "equals")
				origin := sourcemap.Position("input.gox", source, pos, pos+len("equals"))
				if i == 1 {
					// Upstream can know a context without knowing this token's point.
					m.Attach(pos, pos+len("equals"), nil, stack, "generated/unmapped")
				} else {
					m.Attach(pos, pos+len("equals"), &origin, stack, "macro template")
				}
				// A receiver's independently known context must never substitute for
				// the selector's context, including when the selector has none.
				receiver := search + strings.Index(string(source[search:]), "return ") + len("return ")
				width := 1
				if tc.receiver == "int(1)" {
					width = 3
				}
				receiverOrigin := sourcemap.Position("input.gox", source, receiver, receiver+width)
				m.Attach(receiver, receiver+width, &receiverOrigin, equalityProvenanceStack("operand", 120+i*20), "macro template")
				search = pos + len("equals")
			}
			cfg := Config{Derives: tc.derive, Maps: map[string]*sourcemap.Map{"input.gox": m}}
			prepared, err := Prepare(map[string][]byte{"input.gox": source}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			equalityProvenanceRequireCalls(t, prepared["input.gox"], cfg.Maps["input.gox"], tc.prepared, stacks)
			equalityProvenanceRequireUnownedHelper(t, prepared["input.gox"], cfg.Maps["input.gox"], tc.prepared)
			lowered, err := Lower(prepared, cfg)
			if err != nil {
				t.Fatal(err)
			}
			equalityProvenanceRequireCalls(t, lowered["input.gox"], cfg.Maps["input.gox"], tc.lowered, stacks)
			equalityProvenanceRequireUnownedHelper(t, lowered["input.gox"], cfg.Maps["input.gox"], tc.lowered)
		})
	}
}

func TestEqualityProvenanceFilesAndNativeStayIndependent(t *testing.T) {
	files := map[string][]byte{
		"a.gox":     []byte("package fixture\nfunc a()bool{return int(1).equals(int(2))}\n"),
		"b.gox":     []byte("package fixture\nfunc b()bool{return int(1).equals(int(2))}\n"),
		"native.go": []byte("package fixture\n// retain native spacing\nfunc native() int { return int(3) }\n"),
	}
	maps := map[string]*sourcemap.Map{}
	for name, source := range files {
		maps[name] = sourcemap.Identity(name, source)
		if strings.HasSuffix(name, ".gox") {
			pos := strings.Index(string(source), "equals")
			maps[name].Attach(pos, pos+len("equals"), nil, equalityProvenanceStack(name, 50), "generated/unmapped")
		}
	}
	nativeMap := maps["native.go"]
	cfg := Config{Maps: maps, Native: map[string]bool{"native.go": true}}
	prepared, err := Prepare(files, cfg)
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := Lower(prepared, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.gox", "b.gox"} {
		equalityProvenanceRequireCalls(t, lowered[name], maps[name], "__goxide_enum_scalar_equals", [][]sourcemap.Expansion{equalityProvenanceStack(name, 50)})
	}
	if !bytes.Equal(lowered["native.go"], files["native.go"]) || maps["native.go"] != nativeMap {
		t.Fatal("native source or its independent map was rewritten")
	}
}

func TestEqualityProvenanceRenamedBindingsLoseOnlyPoint(t *testing.T) {
	source := []byte("package fixture\ntype T struct{Value int}\nfunc f()bool{EqualsT:=1;_=EqualsT;return T{1}.equals(T{1})}\n")
	m := sourcemap.Identity("input.gox", source)
	stack := equalityProvenanceStack("binding", 40)
	for start := 0; ; {
		rel := strings.Index(string(source[start:]), "EqualsT")
		if rel < 0 {
			break
		}
		pos := start + rel
		origin := sourcemap.Position("input.gox", source, pos, pos+len("EqualsT"))
		m.Attach(pos, pos+len("EqualsT"), &origin, stack, "macro template")
		start = pos + len("EqualsT")
	}
	cfg := Config{Derives: map[string]Derive{"T": {PartialEq: true}}, Maps: map[string]*sourcemap.Map{"input.gox": m}}
	out, err := Prepare(map[string][]byte{"input.gox": source}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, segment := range cfg.Maps["input.gox"].Segments {
		if strings.HasPrefix(string(out["input.gox"][segment.Start:segment.End]), "__goxide_enum_eq_local_") {
			count++
			if segment.Origin != nil || segment.Kind != "generated/unmapped" || !reflect.DeepEqual(segment.Stack, stack) {
				t.Fatalf("renamed binding regained a point or lost its frozen context: %+v", segment)
			}
		}
	}
	if count != 2 {
		t.Fatalf("want two renamed binding tokens, got %d", count)
	}
}

func TestEqualityProvenanceNoopPrecision(t *testing.T) {
	const nativeMethods = "package fixture\ntype T struct{Value int}\nfunc(t T)equals(other T)bool{return t.Value==other.Value}\nfunc pair(a,b int)int{return a+b}\nfunc use()bool{return T{1}.equals(T{2})}\n"
	for _, tc := range []struct {
		name, source, other string
		derive              map[string]Derive
	}{
		{name: "comment", source: "package fixture\n// equals only appears in this comment.\nfunc pair(a,b int)int{return a+b}\n"},
		{name: "native-method", source: nativeMethods},
		{name: "derive-in-other-file", source: nativeMethods, other: "package fixture\ntype D struct{Value int}\n", derive: map[string]Derive{"D": {PartialEq: true, Eq: true}}},
		{name: "scalar-in-other-file", source: nativeMethods, other: "package fixture\nfunc scalar()bool{return int(1).equals(int(2))}\n"},
	} {
		for _, newline := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
			t.Run(tc.name+"/"+newline.name, func(t *testing.T) {
				// Other-file helpers use the lexically first file; this file itself
				// has no equality rewrite in either Prepare or Lower.
				const name = "z.gox"
				source := []byte(strings.ReplaceAll(tc.source, "\n", newline.text))
				files := map[string][]byte{name: source}
				if tc.other != "" {
					files["a.gox"] = []byte(strings.ReplaceAll(tc.other, "\n", newline.text))
				}
				maps := map[string]*sourcemap.Map{}
				for file, text := range files {
					maps[file] = sourcemap.Identity(file, text)
				}
				want := equalityProvenancePunctuation(source)
				if len(want) == 0 {
					t.Fatal("fixture must contain punctuation")
				}
				for _, pos := range want {
					origin := sourcemap.Position(name, source, pos, pos+1)
					maps[name].Attach(pos, pos+1, &origin, equalityProvenanceStack("punctuation", pos), "macro template")
				}
				cfg := Config{Derives: tc.derive, Maps: maps}
				current := files
				for _, stage := range []struct {
					name string
					run  func(map[string][]byte, Config) (map[string][]byte, error)
				}{{"Prepare", Prepare}, {"Lower", Lower}} {
					out, err := stage.run(current, cfg)
					if err != nil {
						t.Fatal(err)
					}
					got := equalityProvenancePunctuation(out[name])
					if len(got) != len(want) {
						t.Fatalf("%s changed punctuation count: got %d, want %d", stage.name, len(got), len(want))
					}
					for i, pos := range got {
						origin := sourcemap.Position(name, source, want[i], want[i]+1)
						segment := maps[name].Lookup(pos)
						if segment == nil || segment.Origin == nil || *segment.Origin != origin || !reflect.DeepEqual(segment.Stack, equalityProvenanceStack("punctuation", want[i])) {
							t.Fatalf("%s erased unchanged %q token at %d: %+v", stage.name, out[name][pos:pos+1], pos, segment)
						}
					}
					current = out
				}
			})
		}
	}
}

func equalityProvenancePunctuation(source []byte) []int {
	fs := token.NewFileSet()
	file := fs.AddFile("input.go", -1, len(source))
	var s scanner.Scanner
	s.Init(file, source, nil, 0)
	var offsets []int
	for {
		pos, tok, _ := s.Scan()
		if tok == token.EOF {
			return offsets
		}
		if tok == token.COMMA || tok == token.PERIOD {
			offsets = append(offsets, file.Offset(pos))
		}
	}
}

func equalityProvenanceRequireCalls(t *testing.T, source []byte, m *sourcemap.Map, name string, stacks [][]sourcemap.Expansion) {
	t.Helper()
	calls := equalityProvenanceCalls(t, source, m, name)
	if len(calls) != len(stacks) {
		t.Fatalf("want %d %s calls, got %d", len(stacks), name, len(calls))
	}
	for i, segment := range calls {
		if segment == nil || segment.Origin != nil || segment.Kind != "generated/unmapped" || !reflect.DeepEqual(segment.Stack, stacks[i]) {
			t.Fatalf("generated %s call %d has wrong provenance: %+v; want stack %+v", name, i, segment, stacks[i])
		}
	}
}

func equalityProvenanceRequireUnownedHelper(t *testing.T, source []byte, m *sourcemap.Map, name string) {
	t.Helper()
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "output.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		helper, ok := decl.(*ast.FuncDecl)
		if !ok || helper.Name.Name != name {
			continue
		}
		start, end := fs.Position(helper.Pos()).Offset, fs.Position(helper.End()).Offset
		for _, segment := range m.Segments {
			if segment.Start >= start && segment.End <= end && (segment.Origin != nil || len(segment.Stack) != 0) {
				t.Fatalf("shared helper borrowed one caller's provenance: %+v", segment)
			}
		}
		return
	}
	t.Fatalf("missing generated helper %s", name)
}

func equalityProvenanceStack(macro string, callsite int) []sourcemap.Expansion {
	return []sourcemap.Expansion{
		{Macro: "outer", Definition: sourcemap.Span{File: "macros.gox", Start: 0, End: 20, Line: 1, Column: 1}, Callsite: sourcemap.Span{File: "caller.gox", Start: callsite, End: callsite + 10, Line: 3, Column: 1}},
		{Macro: macro, Definition: sourcemap.Span{File: "macros.gox", Start: 21, End: 40, Line: 2, Column: 1}, Callsite: sourcemap.Span{File: "macros.gox", Start: 8, End: 16, Line: 1, Column: 9}},
	}
}

func equalityProvenanceCalls(t *testing.T, source []byte, m *sourcemap.Map, name string) []*sourcemap.Segment {
	t.Helper()
	fs := token.NewFileSet()
	file, err := parser.ParseFile(fs, "output.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found []*sourcemap.Segment
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		id, ok := call.Fun.(*ast.Ident)
		if ok && id.Name == name {
			found = append(found, m.Lookup(fs.Position(id.Pos()).Offset))
		}
		return true
	})
	return found
}
