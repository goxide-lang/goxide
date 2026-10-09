package acceptance_test

import (
	"errors"
	"github.com/hxsf/goxide/internal/sourcemap"
	"strings"
	"testing"
)

func TestSourceMappingLoweredMatchPropagation(t *testing.T) {
	const src = `package fixture
func prefix()Option[int]{x:=Option[int]::Some(11)?;return Option[int]::Some(x)}
macro_rules! inner {($x:expr)=>{$x+771};}
macro_rules! outer {($x:expr)=>{inner!($x)+771};}
enum State { Ready(int), Empty }
#[derive(Clone,Debug)]
type Value struct{N int}
func get()Option[int]{return Option[int]::Some(771)}
func run()Option[int]{
 n:=get()?
 match State::Ready(n) {
 State::Ready(v) => { n=outer!(771)+v },
 State::Empty => { n=771 },
 }
 return Option[int]::Some(n)
}
func expression(s State)int{return match s {State::Ready(v)=>outer!(771)+v, State::Empty=>771}}
`
	r, err := compileAcceptanceLanguageFixture("lowered.gox", []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	inner := strings.Index(src, "$x+771") + len("$x+")
	outer := strings.Index(src, "($x)+771") + len("($x)+")
	call1 := strings.Index(src, "outer!(771)")
	call2 := strings.LastIndex(src, "outer!(771)")
	expected := []int{strings.Index(src, "Some(771)") + len("Some("), call1 + len("outer!("), inner, outer, strings.Index(src, "n=771") + 2, call2 + len("outer!("), inner, outer, strings.LastIndex(src, "=>771") + 2}
	count := 0
	depth2 := 0
	for start := 0; start < len(r.Go); {
		n := strings.Index(string(r.Go[start:]), "771")
		if n < 0 {
			break
		}
		off := start + n
		seg := r.Diagnostics.Lookup(off)
		if seg == nil || seg.Origin == nil || src[seg.Origin.Start:seg.Origin.End] != "771" {
			t.Fatalf("lost literal provenance at %d: %+v\n%s", off, seg, r.Go)
		}
		if count >= len(expected) || seg.Origin.Start != expected[count] || seg.Origin.End != expected[count]+3 {
			t.Fatalf("token %d got %+v want %v", count, seg, expected)
		}
		for _, frame := range seg.Stack {
			if frame.Definition.Start != strings.Index(src, "macro_rules! "+frame.Macro) {
				t.Fatalf("shifted definition: %+v", frame)
			}
			call := call1
			if count >= 5 {
				call = call2
			}
			text := "outer!(771)"
			if frame.Macro == "inner" {
				call = strings.Index(src, "inner!($x)")
				text = "inner!($x)"
			}
			if frame.Callsite.Start != call || frame.Callsite.End != call+len(text) {
				t.Fatalf("shifted call: %+v", frame)
			}
		}
		count++
		if len(seg.Stack) == 2 {
			depth2++
		}
		start = off + 3
	}
	if count != 9 || depth2 != 4 {
		t.Fatalf("literal count=%d nested=%d", count, depth2)
	}
	off := strings.Index(string(r.Go), "__goxide_qtmp_")
	if off < 0 {
		t.Fatal("no substantive propagation")
	}
	if s := r.Diagnostics.Lookup(off); s != nil && s.Origin != nil {
		t.Fatalf("generated temp borrowed user origin: %+v", s)
	}
	off = strings.Index(string(r.Go), "CloneValue")
	if s := r.Diagnostics.Lookup(off); s == nil || s.Origin == nil || src[s.Origin.Start:s.Origin.End] != "Value" {
		t.Fatalf("derive origin lost: %+v", s)
	}
}
func TestSourceMappingRealTypecheckAfterLowering(t *testing.T) {
	const src = `package fixture
func prefix()Option[int]{x:=Option[int]::Some(1)?;return Option[int]::Some(x)}
macro_rules! bad {()=>{var broken int="wrong";_=broken};}
func run()Option[int]{n:=Option[int]::Some(7)?;bad!();return Option[int]::Some(n)}
`
	_, err := compileAcceptanceLanguageFixture("real_lowered.gox", []byte(src))
	var mapped *sourcemap.Error
	if !errors.As(err, &mapped) || mapped.Origin == nil {
		t.Fatalf("real go/types error lacks proven origin: %v", err)
	}
	if !strings.Contains(mapped.Message, "cannot use") {
		t.Fatalf("not the actual type error: %v", err)
	}
	want := strings.Index(src, `"wrong"`)
	if mapped.Origin.Start != want || mapped.Origin.End != want+len(`"wrong"`) {
		t.Fatalf("wrong error origin: %+v", mapped)
	}
	if len(mapped.Stack) != 1 || mapped.Stack[0].Definition.Start != strings.Index(src, "macro_rules!") || mapped.Stack[0].Callsite.Start != strings.Index(src, "bad!()") {
		t.Fatalf("wrong macro error stack: %+v", mapped)
	}
}
func TestSourceMappingSyntheticErrorRemainsUnmapped(t *testing.T) {
	const src = `package fixture
macro_rules! bad {($x:expr)=>{$x+"wrong"};}
func run()Option[int]{n:=Option[int]::Some(7)?;return Option[int]::Some(bad!(n))}
`
	_, err := compileAcceptanceLanguageFixture("synthetic.gox", []byte(src))
	var mapped *sourcemap.Error
	if !errors.As(err, &mapped) || mapped.Origin != nil || !strings.Contains(err.Error(), "generated/unmapped") {
		t.Fatalf("synthetic macro parentheses borrowed an origin: %v", err)
	}
}
func TestSourceMappingPrivateDelimiterRejected(t *testing.T) {
	_, err := compileAcceptanceLanguageFixture("nul.gox", []byte("package p\nvar s=\"\x000\x00\""))
	if err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("private delimiter accepted: %v", err)
	}
}
