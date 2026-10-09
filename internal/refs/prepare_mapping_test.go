package refs

import (
	"bytes"
	"github.com/hxsf/goxide/internal/sourcemap"
	"strings"
	"testing"
)

func TestPrepareMappedCopiedTokensAndGeneratedIR(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		source := []byte(strings.ReplaceAll("package p\nimport route \"example.org/model\"\n#[derive(Clone)]\ntype Record struct{N int}\nfunc F(p &mut int)int{q:=&mut (*p);_ = route.Value;*q=3;return route.Value}\n", "\n", newline))
		mapping := sourcemap.Identity("input.gox", source)
		result, err := PrepareMapped("input.gox", source, mapping)
		if err != nil {
			t.Fatal(err)
		}
		for _, spelling := range []string{"route", "Value", "Record"} {
			sourceAt, outputAt := 0, 0
			for {
				a := bytes.Index(source[sourceAt:], []byte(spelling))
				b := bytes.Index(result.Source[outputAt:], []byte(spelling))
				if a < 0 {
					break
				}
				if b < 0 {
					t.Fatalf("lost %s", spelling)
				}
				a += sourceAt
				b += outputAt
				segment := mapping.Lookup(b)
				if segment == nil || segment.Origin == nil || segment.Origin.Start != a || segment.Origin.End != a+len(spelling) {
					t.Fatalf("%s %d: %+v", spelling, a, segment)
				}
				sourceAt = a + len(spelling)
				outputAt = b + len(spelling)
			}
		}
		generated := []byte("__goxide_ref_make_write")
		for at := 0; ; {
			delta := bytes.Index(result.Source[at:], generated)
			if delta < 0 {
				break
			}
			at += delta
			if segment := mapping.Lookup(at); segment != nil && segment.Origin != nil {
				t.Fatalf("generated IR borrowed origin %+v", segment)
			}
			at += len(generated)
		}
	}
}
