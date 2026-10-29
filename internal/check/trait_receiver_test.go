package check_test

import (
	"github.com/goxide-lang/goxide/internal/check"
	"strings"
	"testing"
)

func TestBorrowedValueExcludesNativePointerImpl(t *testing.T) {
	const prefix = `package p
trait Read { func read(self &Self) int }
impl Read for int { func read(self &Self) int { return 10 } }
impl[T Read] Read for *T { func read(self &Self) int { return 99 } }
`
	for name, source := range map[string]string{
		"generic-qualified": `func F[T Read](value &T) int { return Read::read(value) }`,
		"borrowed-dot":      `func F(value &int) int { return value.read() }`,
		"local-copy":        `func F(value &int) int { alias:=value; return alias.read() }`,
		"returned-borrow":   `func Borrow(value &int) &int { return value }; func F(value &int) int { return Borrow(value).read() }`,
		"method-value":      `func F(value &int) int { read:=value.read; return read() }`,
		"pointer-borrow":    `func F(value &*int) int { return Read::read(value) }`,
		"write-borrow":      `func F(value &mut int) int { return Read::read(value) }`,
		"borrowed-view":     `func F(value &int) int { var view Read=value; return view.read() }`,
		"reborrow":          `func F(value &int) int { return (&*value).read() }`,
		"field-address":     `type Holder struct { Field int }; func F(value &Holder) int { return (&value.Field).read() }`,
		"field-qualified":   `type Holder struct { Field int }; func F(value &Holder) int { return Read::read(&value.Field) }`,
		"array-element":     `func F(value &[2]int) int { return (&(*value)[0]).read() }`,
		"slice-element":     `func F(value &[]int) int { return Read::read(&(*value)[0]) }`,
		"nested-slice":      `type Holder struct { Field []int }; func F(value &Holder) int { return (&value.Field[0]).read() }`,
	} {
		t.Run(name, func(t *testing.T) {
			files, res := parse(t, prefix+source)
			_, ds := check.Check(files, res)
			if len(ds) != 0 {
				t.Fatal(ds)
			}
		})
	}
}

func TestBorrowedValueKeepsNativePointerAndPermissionBoundaries(t *testing.T) {
	for name, tc := range map[string]struct{ source, want string }{
		"native-ambiguity":         {`trait Read { func read(self &Self) int }; impl Read for int { func read(self &Self) int{return 10} }; impl Read for *int { func read(self &Self) int{return 99} }; func F(value *int) int{return value.read()}`, "ambiguous trait method"},
		"native-address-ambiguity": {`trait Read { func read(self &Self) int }; impl Read for int { func read(self &Self) int{return 10} }; impl Read for *int { func read(self &Self) int{return 99} }; func F(value int) int{return (&value).read()}`, "ambiguous trait method"},
		"native-pointer-only":      {`trait Read { func read(self &Self) int }; impl Read for *int { func read(self &Self) int{return 99} }; func F(value *int) int{return value.read()}`, ""},
		"borrow-not-pointer":       {`trait Read { func read(self &Self) int }; impl Read for *int { func read(self &Self) int{return 99} }; func F(value &int) int{return Read::read(value)}`, "no explicit implementation"},
		"readonly-mutation":        {`trait Change { func change(self &mut Self) }; impl Change for int {func change(self &mut Self){*self=1} }; func F(value &int){ value.change() }`, "cannot"},
	} {
		t.Run(name, func(t *testing.T) {
			files, res := parse(t, "package p;"+tc.source)
			_, ds := check.Check(files, res)
			if tc.want == "" {
				if len(ds) != 0 {
					t.Fatal(ds)
				}
				return
			}
			for _, d := range ds {
				if strings.Contains(d.Message, tc.want) && (name != "readonly-mutation" || d.Code == check.ReferencePermission) {
					return
				}
			}
			t.Fatalf("want %q, got %v", tc.want, ds)
		})
	}
}
