package frontend

import (
	"strings"
	"testing"
)

func TestCompositePermissionsAfterPropagation(t *testing.T) {
	const prelude = `package p
#[derive(Debug)]
type State struct { N int; Items []int }
func load()Result[int,string]{return Result[int,string]::Ok(2)}
`
	for _, test := range []struct {
		name, body string
		reject     bool
	}{
		{"value-copy", `func f(r &State)Result[int,string]{n:=load()?;v:=*r;v.N+=n;return Result[int,string]::Ok(v.N)}`, false},
		{"shared-field", `func f(r &State)Result[int,string]{n:=load()?;v:=*r;v.Items[0]=n;return Result[int,string]::Ok(n)}`, true},
		{"return-erasure", `func f(r &State)Result[[]int,string]{_=load()?;return Result[[]int,string]::Ok(r.Items)}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, err := Compile("composite_propagation.hgo", []byte(prelude+test.body))
			if test.reject {
				if err == nil {
					t.Fatal("accepted permission erasure")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(r.Go), "__hgo_ref_") {
				t.Fatal("analysis types escaped")
			}
		})
	}
}
