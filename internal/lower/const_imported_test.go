package lower_test

import (
	"strings"
	"testing"
)

func TestConstArrayImportedIntegerConstants(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"values/lib.gox": `package values;pub const Width int=2`,
		"main.gox": `package main
use fmt;
use time as clock;
use time::Thursday as Count;
use associatedfixture::values;
func Size[const N int]()int{return N}
func Copy[T any,const N int](value [N]T)[N]T{return value}
func main(){a:=Copy[int,values::Width]([2]int{20,22});fmt::Println(a[0]+a[1],Size[clock::Sunday](),Size[Count](),Size[clock::Thursday+1]())}`,
	}, "42 0 4 5\n")
}
func TestConstArrayImportedNonIntegerConstant(t *testing.T) {
	_, err := lowerDeriveSources(map[string]string{"main.gox": `package main;use math;func Size[const N int]()int{return N};func main(){_=Size[math::Pi]()}`})
	if err == nil || !strings.Contains(err.Error(), "integer constant") {
		t.Fatalf("want integer constant diagnostic: %v", err)
	}
}
