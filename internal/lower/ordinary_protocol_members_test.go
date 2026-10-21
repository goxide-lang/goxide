package lower_test

import (
	"strings"
	"testing"
)

func TestOrdinaryProtocolNamesUseSelectedMembers(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
trait LibraryPartialEq { func equals(self Self,other Self) bool }
trait LibraryClone { func clone(self Self) Self }
trait LibraryDebug { func debug(self Self) string }
impl LibraryPartialEq for int { func equals(self Self,other Self) bool { return self != other } }
impl LibraryClone for int { func clone(self Self) Self { return self + 10 } }
impl LibraryDebug for int { func debug(self Self) string { return "custom" } }
type Record struct { value int }
impl LibraryPartialEq for Record { func equals(self Self,other Self) bool { return self.value%10 == other.value%10 } }
func Equal[T LibraryPartialEq](a,b T) bool { return a.equals(b) }
func main() {
 a,b:=Record{value:11},Record{value:21}
 f:=a.equals
 fmt::Println(int(1).equals(int(2)),LibraryPartialEq::equals(int(1),int(2)),Equal(1,2),a.equals(b),f(b),LibraryPartialEq::equals(a,b),int(1).clone(),LibraryClone::clone(int(1)),int(1).debug(),LibraryDebug::debug(int(1)))
}
`}, "true true true true true true 11 11 custom custom\n")
}
func TestOrdinaryEqualsKeepsAmbiguityDiagnostic(t *testing.T) {
	_, err := lowerDeriveSources(map[string]string{"main.gox": `package main
trait First { func equals(self Self,other Self) bool }
trait Second { func equals(self Self,other Self) bool }
impl First for int { func equals(self Self,other Self) bool { return true } }
impl Second for int { func equals(self Self,other Self) bool { return false } }
func main(){ int(1).equals(int(2)) }
`})
	if err == nil || !strings.Contains(err.Error(), "ambiguous trait method") {
		t.Fatalf("primitive intrinsic hid ambiguous ordinary members: %v", err)
	}
}
