package check_test

import (
	"testing"

	"github.com/goxide-lang/goxide/internal/check"
)

func TestTraitPrerequisitesCanonicalBuiltinAliases(t *testing.T) {
	for _, source := range []string{
		`impl Marker for uint8{}; impl Marker for int32{}; func run(){need([]byte{1});need([]rune{'x'});need([1]byte{2});need(map[string]rune{"x":'x'})}`,
		`impl Marker for byte{}; impl Marker for rune{}; func run(){need([]uint8{1});need([]int32{'x'});need([1]uint8{2});need(map[string]int32{"x":'x'})}`,
	} {
		files, resolved := parse(t, `package p
trait Marker{}
impl[T Marker] Marker for []T{}
impl[T Marker,const N int] Marker for [N]T{}
impl[T Marker] Marker for map[string]T{}
func need[T Marker](value T){}
`+source)
		if _, diagnostics := check.Check(files, resolved); len(diagnostics) != 0 {
			t.Fatal(diagnostics)
		}
	}
	files, resolved := parse(t, `package p
trait Marker{}
impl Marker for uint8{}
impl Marker for byte{}
`)
	if _, diagnostics := check.Check(files, resolved); len(diagnostics) == 0 {
		t.Fatal("alternate primitive spelling created a distinct implementation head")
	}
}
