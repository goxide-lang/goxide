package refs

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestFinishErrorUsesCopiedInputPosition(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		for _, failing := range []string{"*r = 7", "p := &mut (*r); _ = p"} {
			t.Run(strings.ReplaceAll(newline, "\n", "LF"), func(t *testing.T) {
				source := []byte(strings.ReplaceAll("package p\nfunc bad(r &int) {\n _ = *r\n "+failing+"\n}\n", "\n", newline))
				mapping := sourcemap.Identity("original.gox", source)
				prepared, err := PrepareMapped("original.gox", source, mapping)
				if err != nil {
					t.Fatal(err)
				}
				bound, bindings, err := BindProjectionsWithPath("example/p", map[string][]byte{"original.gox": prepared.Source}, nil, nil)
				if err != nil {
					t.Fatal(err)
				}
				mapping = bindings.MapSource("original.gox", bound["original.gox"], mapping)
				_, err = FinishWithPackagePath("example/p", "original.gox", bound["original.gox"], nil)
				var diagnostic *Error
				if !errors.As(err, &diagnostic) || diagnostic.Generated {
					t.Fatalf("missing copied input error: %v", err)
				}
				want := bytes.LastIndex(source, []byte("*r"))
				segment := mapping.Lookup(diagnostic.Position.Offset)
				if segment == nil || segment.Origin == nil || segment.Origin.Start != want || segment.Origin.Line != 4 {
					t.Fatalf("lost original assignment: %v; segment=%+v want=%d", err, segment, want)
				}
			})
		}
	}
}
