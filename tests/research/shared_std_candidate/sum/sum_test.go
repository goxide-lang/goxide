package sum_test

import (
	"github.com/goxide-lang/std/sum"
	"testing"
)

type alias[T, E any] = sum.Result[T, E]
type optionEmbed struct{ sum.Option[int] }
type resultEmbed struct{ sum.Result[int, error] }
type failure struct{}

func (*failure) Error() string { return "failure" }

func rejects(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("invalid shape accepted")
		}
	}()
	f()
}
func TestValueSemantics(t *testing.T) {
	var r alias[int, error] = sum.NewOk[int, error](7)
	if r.(sum.Ok[int, error]).Value != 7 {
		t.Fatal(r)
	}
	sum.ValidateResult(r)
	sum.ValidateOption(sum.None[int]())
	present := sum.NewSome((*int)(nil))
	if present == nil || present.(sum.Some[*int]).Value != nil {
		t.Fatal("Some(nil) collapsed")
	}
	sum.ValidateOption(present)
	values := []int{1}
	copied := sum.NewSome(values)
	values[0] = 2
	if copied.(sum.Some[[]int]).Value[0] != 2 {
		t.Fatal("assignment unexpectedly cloned")
	}
	nilErr := sum.NewErr[int, error](nil)
	sum.ValidateResult(nilErr)
	if nilErr.(sum.Err[int, error]).Value != nil {
		t.Fatal("nil Err payload changed")
	}
	var ptr *failure
	boxed := sum.NewErr[int, error](ptr)
	if boxed.(sum.Err[int, error]).Value == nil {
		t.Fatal("typed nil interface changed")
	}
	partial := struct {
		Value int
		Err   *failure
	}{3, &failure{}}
	kept := sum.NewErr[int, struct {
		Value int
		Err   *failure
	}](partial)
	if kept.(sum.Err[int, struct {
		Value int
		Err   *failure
	}]).Value != partial {
		t.Fatal("partial changed")
	}
}
func TestInvalidShapes(t *testing.T) {
	var op *sum.Some[int]
	var rp *sum.Ok[int, error]
	cases := []func(){
		func() { sum.ValidateOption[int](op) },
		func() { sum.ValidateOption[int](&sum.Some[int]{Value: 1}) },
		func() { sum.ValidateOption[int](optionEmbed{}) },
		func() { sum.ValidateResult[int, error](nil) },
		func() { sum.ValidateResult[int, error](rp) },
		func() { sum.ValidateResult[int, error](&sum.Err[int, error]{}) },
		func() { sum.ValidateResult[int, error](resultEmbed{}) },
	}
	for i, f := range cases {
		t.Run(string(rune('a'+i)), func(t *testing.T) { rejects(t, f) })
	}
}
