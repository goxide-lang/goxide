package project

import (
	"strings"
	"testing"
)

func TestProceduralMacroManifestTarget(t *testing.T) {
	const header = "[go_mod]\nmodule = 'example.org/macros'\ngo = '1.27'\n"
	for _, test := range []struct {
		name, target string
		want, reject bool
	}{
		{"ordinary lib", "[lib]\nname='macros'\npath='src'\n", false, false},
		{"proc lib", "[lib]\nname='macros'\npath='src'\nproc_macro=true\n", true, false},
		{"disabled proc lib", "[lib]\nname='macros'\npath='src'\nproc_macro=false\n", false, false},
		{"bin", "[[bin]]\nname='macros'\npath='src'\nproc_macro=true\n", false, true},
		{"example", "[[example]]\nname='macros'\npath='src'\nproc_macro=true\n", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest, err := Parse("goxide.toml", []byte(header+test.target))
			if test.reject {
				if err == nil || !strings.Contains(err.Error(), "only supported for lib") {
					t.Fatalf("invalid target accepted: %v", err)
				}
				return
			}
			if err != nil || manifest.Lib == nil || manifest.Lib.ProcMacro != test.want {
				t.Fatalf("lib = %+v, error = %v", manifest.Lib, err)
			}
		})
	}
	base := header + "[lib]\nname='macros'\npath='src'\n"
	ordinary, err := Parse("goxide.toml", []byte(base))
	if err != nil {
		t.Fatal(err)
	}
	proc, err := Parse("goxide.toml", []byte(base+"proc_macro=true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if Digest(ordinary) == Digest(proc) {
		t.Fatal("proc_macro role does not affect manifest identity")
	}
}
