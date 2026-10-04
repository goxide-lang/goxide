package acceptance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestReadonlyCacheApplicationRace(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	source, err := os.ReadFile(filepath.Join(filepath.Dir(here), "../../examples/prototype/readonly_cache.gox"))
	if err != nil {
		t.Fatal(err)
	}
	macroRaceModule(t, string(source), `package main
import "testing"
func TestSnapshotsAndSharedIdentity(t *testing.T){
 limit:=11;c:=Cache{Reads:2,Samples:[3]int{3,4,5},Values:[]int{5,8},Counts:map[string]int{"hits":7},Limit:&limit}
 if got:=inspectCache(&c);got!=40{t.Fatal(got)}
 if c.Reads!=2||c.Samples[0]!=3||c.Values[0]!=5||c.Counts["hits"]!=7||limit!=11{t.Fatal("snapshot mutated original")}
 updateCache(&c)
 if got:=inspectCache(&c);got!=44{t.Fatal(got)}
 if c.Reads!=3||c.Samples[0]!=3||c.Values[0]!=6||c.Counts["hits"]!=8||limit!=12{t.Fatal("writer lost identity")}
}
`)
}
