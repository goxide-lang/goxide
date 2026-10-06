package frontend

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/propagate"
	"github.com/hxsf/goxide/internal/sourcemap"
)

func requireSyntheticMacroError(t *testing.T, err error, frames int) *sourcemap.Error {
	t.Helper()
	var d *sourcemap.Error
	if !errors.As(err, &d) || d.Origin != nil || len(d.Stack) != frames || !strings.Contains(err.Error(), "generated/unmapped") || strings.Contains(err.Error(), "source span") {
		t.Fatalf("synthetic point must keep only its proven stack: %v", err)
	}
	return d
}
func checkMacroFrame(t *testing.T, span sourcemap.Span, source, prefix string) {
	t.Helper()
	if span.Start < 0 || span.End > len(source) || span.Start >= span.End || !strings.HasPrefix(source[span.Start:span.End], prefix) {
		t.Fatalf("wrong frame %+v for %q", span, prefix)
	}
	want := sourcemap.Position(span.File, []byte(source), span.Start, span.End)
	if span != want {
		t.Fatalf("wrong frame coordinates %+v want %+v", span, want)
	}
}
func TestMacroSyntheticDiagnosticKeepsKnownFrames(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := strings.ReplaceAll("package p\nmacro_rules! bad {($v:expr)=>{new($v?)};}\nmacro_rules! outer {($v:expr)=>{bad!($v)};}\nfunc f(){_=First()?;_=Second()?;_=outer!(value)}\n", "\n", newline)
		for _, packaged := range []bool{false, true} {
			var err error
			if packaged {
				_, err = CompilePackage("example/p", map[string][]byte{"synthetic.gox": []byte(source)}, nil)
			} else {
				_, err = Compile("synthetic.gox", []byte(source))
			}
			d := requireSyntheticMacroError(t, err, 2)
			for i, name := range []string{"outer", "bad"} {
				if d.Stack[i].Macro != name || !d.Stack[i].DefinitionOriginal || !d.Stack[i].CallsiteOriginal {
					t.Fatalf("wrong frame: %+v", d.Stack[i])
				}
				checkMacroFrame(t, d.Stack[i].Definition, source, "macro_rules! "+name)
				checkMacroFrame(t, d.Stack[i].Callsite, source, name+"!(")
			}
			var again error
			if packaged {
				_, again = CompilePackage("example/p", map[string][]byte{"synthetic.gox": []byte(source)}, nil)
			} else {
				_, again = Compile("synthetic.gox", []byte(source))
			}
			repeated := requireSyntheticMacroError(t, again, 2)
			if err.Error() != again.Error() || !reflect.DeepEqual(d, repeated) {
				t.Fatal("nondeterministic macro diagnostic")
			}
		}
	}
}
func TestMacroSyntheticCrossFileAndPackageFrames(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		defs := strings.ReplaceAll("package p\nmacro_rules! bad {($v:expr)=>{new($v?)};}\nmacro_rules! outer {($v:expr)=>{bad!($v)};}\n", "\n", newline)
		caller := strings.ReplaceAll("package p\nfunc f(){_=First()?;_=outer!(value)}\n", "\n", newline)
		_, err := CompilePackage("example/p", map[string][]byte{"definitions.gox": []byte(defs), "caller.gox": []byte(caller)}, nil)
		d := requireSyntheticMacroError(t, err, 2)
		checkMacroFrame(t, d.Stack[0].Definition, defs, "macro_rules! outer")
		checkMacroFrame(t, d.Stack[0].Callsite, caller, "outer!(")
		checkMacroFrame(t, d.Stack[1].Definition, defs, "macro_rules! bad")
		checkMacroFrame(t, d.Stack[1].Callsite, defs, "bad!(")
		if d.Stack[0].Callsite.File != "caller.gox" || d.Stack[1].Callsite.File != "definitions.gox" {
			t.Fatalf("wrong owners: %+v", d.Stack)
		}

		// The dependency deliberately uses the caller's filename but a much larger
		// original offset. Its registry-owned coordinates must not use caller bytes.
		depSource := strings.ReplaceAll("package dep\n"+strings.Repeat("\n", 300)+"pub macro_rules! bad {($v:expr)=>{new($v?)};}\n", "\n", newline)
		dep, err := preparePackageContext("example/dep", map[string][]byte{"shared.gox": []byte(depSource)}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		caller = strings.ReplaceAll("package caller\nimport dep \"example/dep\"\nmacro_rules! outer {($v:expr)=>{dep.bad!($v)};}\nfunc f(){_=First()?;_=outer!(value)}\n", "\n", newline)
		_, err = preparePackageContext("example/caller", map[string][]byte{"shared.gox": []byte(caller)}, map[string]PackageResult{"example/dep": dep}, nil)
		d = requireSyntheticMacroError(t, err, 2)
		if d.Stack[1].Definition.Start <= len(caller) {
			t.Fatal("fixture must place dependency definition outside the same-named caller's byte range")
		}
		checkMacroFrame(t, d.Stack[0].Definition, caller, "macro_rules! outer")
		checkMacroFrame(t, d.Stack[0].Callsite, caller, "outer!(")
		checkMacroFrame(t, d.Stack[1].Definition, depSource, "macro_rules! bad")
		checkMacroFrame(t, d.Stack[1].Callsite, caller, "dep.bad!(")
	}
}
func TestMacroSyntheticTypeErrorKeepsFrames(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := strings.ReplaceAll("package fixture\nenum Noise { A, }\nmacro_rules! bad {($x:expr)=>{$x+\"wrong\"};}\nfunc run()Option[int]{n:=Option[int]::Some(7)?;return Option[int]::Some(bad!(n))}\n", "\n", newline)
		_, err := Compile("lowered.gox", []byte(source))
		d := requireSyntheticMacroError(t, err, 1)
		checkMacroFrame(t, d.Stack[0].Definition, source, "macro_rules! bad")
		checkMacroFrame(t, d.Stack[0].Callsite, source, "bad!(")
	}
}
func TestMacroSyntheticSegmentRetainsStack(t *testing.T) {
	source := []byte("package p\nmacro_rules! keep {($x:expr)=>{$x};}\nfunc f(){_=keep!(7)}\n")
	result, err := Compile("segments.gox", source)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, segment := range result.Diagnostics.Segments {
		if segment.Origin == nil && len(segment.Stack) > 0 {
			found = true
			if segment.Kind != "generated/unmapped" || len(segment.Stack) != 1 || segment.Stack[0].Macro != "keep" {
				t.Fatalf("wrong synthetic segment: %+v", segment)
			}
		}
	}
	if !found {
		t.Fatal("successful expansion lost every synthetic segment's proven stack")
	}
}

func TestMacroOriginMapperSeparatesPointAndFrame(t *testing.T) {
	source := []byte("package p\nmacro_rules! one {($x:expr)=>{$x?};}\nfunc f(){_=one!(value)}\n")
	preparedMap := sourcemap.Identity("unit.gox", source)
	prepared, err := propagate.PrepareMapped("unit.gox", source, preparedMap)
	if err != nil {
		t.Fatal(err)
	}
	mapper := macroSourceMapper{filename: "unit.gox", original: source, prepared: preparedMap}
	frame := sourcemap.Position("unit.gox", prepared, strings.Index(string(prepared), "macro_rules!"), strings.Index(string(prepared), "\nfunc"))
	want := sourcemap.Position("unit.gox", source, strings.Index(string(source), "macro_rules!"), strings.Index(string(source), "\nfunc"))
	if got := mapper.frame(frame, false); got == nil || *got != want {
		t.Fatalf("frame lost around generated question syntax: %+v", got)
	}
	if got := mapper.point(frame, false); got != nil {
		t.Fatalf("whole changed frame was misrepresented as exact point: %+v", got)
	}
	value := strings.Index(string(prepared), "value")
	point := sourcemap.Position("unit.gox", prepared, value, value+len("value"))
	originalValue := strings.Index(string(source), "value")
	pointWant := sourcemap.Position("unit.gox", source, originalValue, originalValue+len("value"))
	if got := mapper.point(point, false); got == nil || *got != pointWant {
		t.Fatalf("copied token lost exact source: %+v", got)
	}
	marker := strings.Index(string(prepared), "__goxide_question")
	synthetic := sourcemap.Position("unit.gox", prepared, marker, marker+len("__goxide_question"))
	if got := mapper.point(synthetic, false); got != nil {
		t.Fatalf("marker borrowed a source token: %+v", got)
	}
	callStart := strings.Index(string(prepared), "one!(value)")
	call := sourcemap.Position("unit.gox", prepared, callStart, callStart+len("one!(value)"))
	input := []sourcemap.Expansion{{Macro: "one", Definition: frame, Callsite: call}}
	before := append([]sourcemap.Expansion(nil), input...)
	if got := mapper.stack(input); len(got) != 1 || got[0].Definition != want || !got[0].DefinitionOriginal || !got[0].CallsiteOriginal {
		t.Fatalf("known stack lost: %+v", got)
	}
	if !reflect.DeepEqual(input, before) {
		t.Fatal("mapping mutated macro metadata")
	}
	input = append(input, sourcemap.Expansion{Macro: "unknown", Definition: frame, Callsite: synthetic})
	if got := mapper.stack(input); got != nil {
		t.Fatalf("incomplete chain was presented as complete: %+v", got)
	}
}

func TestMacroOriginMapperRejectsUnprovenSpans(t *testing.T) {
	source := []byte("package p\nvar value=other\n")
	mapping := sourcemap.Identity("unit.gox", source)
	mapper := macroSourceMapper{filename: "unit.gox", original: source, prepared: mapping}
	start := strings.Index(string(source), "value")
	valid := sourcemap.Position("unit.gox", source, start, start+len("value"))
	invalid := []sourcemap.Span{valid, valid, valid, valid, valid, valid}
	invalid[0].File = ""
	invalid[1].Start = -1
	invalid[2].End = invalid[2].Start
	invalid[3].End = invalid[3].Start - 1
	invalid[4].Line = 0
	invalid[5].Column = 0
	for _, span := range invalid {
		for _, original := range []bool{false, true} {
			if mapper.point(span, original) != nil || mapper.frame(span, original) != nil {
				t.Fatalf("invalid span accepted original=%v: %+v", original, span)
			}
		}
	}
	foreign := valid
	foreign.File = "other.gox"
	overflow := valid
	overflow.End = len(source) + 1
	wrongCoordinates := valid
	wrongCoordinates.Column++
	partial := sourcemap.Position("unit.gox", source, start+1, start+len("value"))
	for _, span := range []sourcemap.Span{foreign, overflow, wrongCoordinates, partial} {
		if mapper.point(span, false) != nil || mapper.frame(span, false) != nil {
			t.Fatalf("unproven prepared span accepted: %+v", span)
		}
	}
	// Even a syntactically valid source span from another file is not evidence
	// for this prepared file; same numeric offsets do not transfer ownership.
	wrongOrigin := valid
	wrongOrigin.File = "other.gox"
	mapping.Attach(valid.Start, valid.End, &wrongOrigin, nil, "source")
	if mapper.point(valid, false) != nil || mapper.frame(valid, false) != nil {
		t.Fatal("foreign mapped endpoint was relabeled as caller source")
	}
	mapping = sourcemap.Identity("unit.gox", source)
	mapper.prepared = mapping
	wrongOrigin = valid
	wrongOrigin.End = len(source) + 1
	mapping.Attach(valid.Start, valid.End, &wrongOrigin, nil, "source")
	if mapper.point(valid, false) != nil || mapper.frame(valid, false) != nil {
		t.Fatal("out-of-range mapped endpoint accepted")
	}
	mapping = sourcemap.Identity("unit.gox", source)
	mapper.prepared = mapping
	wrongOrigin = valid
	wrongOrigin.End++
	mapping.Attach(valid.Start, valid.End, &wrongOrigin, nil, "source")
	if mapper.point(valid, false) != nil || mapper.frame(valid, false) != nil {
		t.Fatal("mapped token with changed byte width accepted")
	}
	mapping = sourcemap.Identity("unit.gox", source)
	mapper.prepared = mapping
	end := strings.Index(string(source), "other")
	last := sourcemap.Position("unit.gox", source, end, end+len("other"))
	mapping.Attach(valid.Start, valid.End, &last, nil, "source")
	mapping.Attach(last.Start, last.End, &valid, nil, "source")
	envelope := sourcemap.Position("unit.gox", source, valid.Start, last.End)
	if mapper.frame(envelope, false) != nil {
		t.Fatal("reversed original endpoints accepted")
	}
}
