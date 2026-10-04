package enumdemo_test

import (
	e "example.org/goxidedesigncheck/enumdemo"
	"testing"
)

type forgedResult struct{ e.Result[int, string] }
type forgedOption struct{ e.Option[int] }

func rejects(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected boundary rejection")
		}
	}()
	f()
}
func TestResultCases(t *testing.T) {
	for _, tc := range []struct {
		v    e.Result[int, string]
		want string
	}{
		{e.Ok[int, string]{Value: 3}, "positive"},
		{e.Ok[int, string]{Value: 0}, "non-positive"},
		{e.Ok[int, string]{Value: -1}, "non-positive"},
		{e.Err[int, string]{Error: "failed"}, "failed"},
	} {
		if got := e.Describe(tc.v); got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}
func TestForeignInvalidCases(t *testing.T) {
	for _, v := range []e.Result[int, string]{nil, (*e.Ok[int, string])(nil), &e.Ok[int, string]{Value: 1}, forgedResult{}} {
		rejects(t, func() { e.Describe(v) })
	}
}
func TestNestedOptions(t *testing.T) {
	cases := []struct {
		v    e.Option[e.Option[int]]
		want string
	}{
		{nil, "None"},
		{e.Some[e.Option[int]]{Value: nil}, "Some(None)"},
		{e.Some[e.Option[int]]{Value: e.Some[int]{Value: 7}}, "Some(Some(7))"},
	}
	for _, tc := range cases {
		if got := e.NestedState(tc.v); got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
	rejects(t, func() { e.NestedState(e.Some[e.Option[int]]{Value: (*e.Some[int])(nil)}) })
	rejects(t, func() { e.NestedState(e.Some[e.Option[int]]{Value: forgedOption{}}) })
	rejects(t, func() { e.NestedState((*e.Some[e.Option[int]])(nil)) })
}
func TestNullablePointerOption(t *testing.T) {
	n := 9
	for _, tc := range []struct {
		v    e.Option[e.Ptr[int]]
		want string
	}{
		{nil, "None"},
		{e.Some[e.Ptr[int]]{Value: nil}, "Some(nil)"},
		{e.Some[e.Ptr[int]]{Value: e.Ptr[int](&n)}, "Some(pointer)"},
	} {
		if got := e.PointerState(tc.v); got != tc.want {
			t.Fatalf("got %q want %q", got, tc.want)
		}
	}
}
func TestCallbackEvaluationAndBoundary(t *testing.T) {
	calls := 0
	got := e.FetchAndDescribe(func() e.Result[int, string] { calls++; return e.Ok[int, string]{Value: 8} })
	if got != "positive" || calls != 1 {
		t.Fatalf("got %q calls %d", got, calls)
	}
	rejects(t, func() { e.FetchAndDescribe(func() e.Result[int, string] { return nil }) })
	rejects(t, func() { e.FetchAndDescribe(nil) })
}
func TestSharedFieldRevalidation(t *testing.T) {
	holder := struct{ value e.Result[int, string] }{e.Ok[int, string]{Value: 1}}
	saved := holder.value
	holder.value = nil
	if e.Describe(saved) != "positive" {
		t.Fatal("saved interface value changed")
	}
	rejects(t, func() { e.Describe(holder.value) })
}
