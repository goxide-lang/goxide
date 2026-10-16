package acceptance_test

import (
	"strings"
	"testing"
)

func TestSourceTypecheckAfterLowering(t *testing.T) {
	const src = `package fixture
func prefix()Option[int]{x:=Option[int]::Some(1)?;return Option[int]::Some(x)}
macro_rules! bad {()=>{var broken int="wrong";_=broken};}
func run()Option[int]{n:=Option[int]::Some(7)?;bad!();return Option[int]::Some(n)}
`
	_, err := compileAcceptanceLanguageFixture("real_lowered.gox", []byte(src))
	if err == nil || !strings.Contains(err.Error(), "cannot use") {
		t.Fatalf("not the actual type error: %v", err)
	}
}
func TestSourceSyntheticMacroTypeError(t *testing.T) {
	for _, ending := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
		for _, tc := range []struct{ name, body string }{
			{"addition", `$x?+"wrong"`},
			{"length", `len($x?)`},
			{"selector", `($x?).Missing`},
			{"index", `($x?)[0]`},
			{"new-expression", `new($x?)`},
			{"make-type-expression", `make($x?,1)`},
			{"new-literal-after-question", `func()Option[int]{_=$x?;_=new(7);return Option[int]::None}()`},
			{"new-literal", `new(7)`},
			{"allocation-size-expression", `make([]int,"bad")`},
			{"conversion", `int("bad")`},
		} {
			t.Run(ending.name+"/"+tc.name, func(t *testing.T) {
				src := `package fixture
pub enum Event{A,B(int),}
func value()Option[int]{return Option[int]::Some(1)}
func before()Option[int]{a:=value()?;b:=value()?;return Option[int]::Some(a+b)}
macro_rules! inner {($x:expr)=>{` + tc.body + `};}
macro_rules! outer {($x:expr)=>{inner!($x)};}
func run()Option[int]{n:=Option[int]::Some(7)?;_=n;_=outer!(value());return Option[int]::Some(1)}
`
				_, err := compileAcceptanceLanguageFixture("synthetic.gox", []byte(strings.ReplaceAll(src, "\n", ending.text)))
				if err == nil {
					t.Fatal("accepted invalid macro expression after propagation")
				}
			})
		}
	}
}
func TestSourcePrivateDelimiterRejected(t *testing.T) {
	_, err := compileAcceptanceLanguageFixture("nul.gox", []byte("package p\nvar s=\"\x000\x00\""))
	if err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("private delimiter accepted: %v", err)
	}
}
