package lower_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/lower"
)

func TestMemberMappingThroughPreparedModule(t *testing.T) {
	tests := []struct {
		name, want string
		files      map[string]string
	}{
		{
			name: "generic_cross_package",
			want: "1 3 4\n",
			files: map[string]string{
				"lib/lib.gox": `package lib
pub struct box[T any] {
	pub value T
	Hidden int
	pub child *box[T]
}
pub func (b box[T]) next() *box[T] { return b.child }
pub func (b box[T]) readHidden() int { return b.Hidden }
pub func makeBox() *box[int] {
	return &box[int]{value: 1, Hidden: 2, child: &box[int]{value: 3, Hidden: 4}}
}
`,
				"main.gox": `package main
use fmt;
use members::lib;
func main() {
	box := lib::makeBox()
	fmt::Println(box.value, box.next().value, box.next().readHidden())
}
`,
			},
		},
		{
			name: "renamed_embedding_and_distinct_key",
			want: "7 9\n",
			files: map[string]string{
				"main.gox": `package main
use fmt;
struct Private { pub amount int }
type holder struct { Private }
struct local { value int }
pub var value = 9
func main() {
	h := holder{Private: Private{amount: 7}}
	v := local{value: value}
	fmt::Println(h.Private.amount, v.value)
}
`,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := t.TempDir()
			for name, contents := range test.files {
				writeDiagnosticModuleFile(t, source, name, []byte(contents))
			}
			module, err := lower.PrepareModule([]string{source}, "members")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(module) })
			if test.name == "generic_cross_package" {
				generated, err := os.ReadFile(filepath.Join(module, "lib", "lib.go"))
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(generated), "_Hidden") {
					t.Fatalf("private uppercase field did not receive its Go name:\n%s", generated)
				}
			}
			if got := runGo(t, module, "run", "."); got != test.want {
				t.Fatalf("runtime output = %q, want %q", got, test.want)
			}
		})
	}
}
