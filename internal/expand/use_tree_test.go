package expand

import (
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
	pm "github.com/goxide-lang/syntax/proc_macro"
	public "github.com/goxide-lang/syntax/token"
)

func TestUseTreeAttributesSeeOneCompleteItem(t *testing.T) {
	for _, source := range []string{
		`#[forward] use dep::{A, nested::{B as C, *}};`,
		`macro_rules! imports { () => { #[forward] use dep::{A, nested::{B as C, *}}; }; }; imports!()`,
		`macro_rules! passthrough { ($item:item) => { $item }; }; passthrough!(#[forward] use dep::{A, nested::{B as C, *}};)`,
		`emit!()`,
	} {
		calls := 0
		resolver := newAttributeResolver(t, []pm.Entry{
			{Name: "forward", Attribute: func(_ pm.Context, _, input public.TokenStream) pm.Output {
				calls++
				text := input.Display()
				if strings.Count(text, "use") != 1 || !strings.Contains(text, "{") || !strings.Contains(text, "A") || !strings.Contains(text, "B") {
					t.Fatalf("attribute did not receive complete group: %s", text)
				}
				return pm.Success(input)
			}},
			{Name: "emit", Function: func(ctx pm.Context, _ public.TokenStream) pm.Output {
				return procText(ctx, "#[forward] use dep::{A, nested::{B as C, *}};")
			}},
		}, nil)
		result, err := expandAttributes([]*ast.File{expandFile(t, "uses.gox", source)}, resolver)
		if err != nil {
			t.Fatal(err)
		}
		if calls != 1 || len(result.Files[0].Items) != 3 {
			t.Fatalf("attribute calls=%d, leaves=%d", calls, len(result.Files[0].Items))
		}
		output := printer.Source(result.Files[0])
		for _, want := range []string{"use dep::A", "use dep::nested::B as C", "use dep::nested::*"} {
			if !strings.Contains(output, want) {
				t.Fatalf("missing %s: %s", want, output)
			}
		}
	}
}

func TestUseTreeConfigurationSelectsBeforeFlattening(t *testing.T) {
	file := expandFile(t, "uses.gox", `
#[cfg(any())] use missing::{A, B};
#[cfg_attr(all(), cfg(all()))] use selected::{A, B};
`)
	result, err := Expand([]*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	output := printer.Source(result.Files[0])
	if len(result.Files[0].Items) != 2 || strings.Contains(output, "missing") || !strings.Contains(output, "selected::A") || !strings.Contains(output, "selected::B") {
		t.Fatalf("configuration lost group selection: %s", output)
	}
}
