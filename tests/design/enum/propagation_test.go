package enumdemo_test

import (
	e "example.org/goxidedesigncheck/enumdemo"
	"io"
	"reflect"
	"testing"
)

type typedNilError struct{}

func (*typedNilError) Error() string { panic("must not inspect error text") }
func fromGoInt(value int, err error) e.Result[int, error] {
	if err != nil {
		return e.Err[int, error]{Error: err}
	}
	return e.Ok[int, error]{Value: value}
}
func lowerQuestion(call func() e.Result[int, error], events *[]string) e.Result[int, error] {
	defer func() { *events = append(*events, "defer") }()
	*events = append(*events, "first")
	tmp := call()
	var n int
	switch v := tmp.(type) {
	case e.Ok[int, error]:
		n = v.Value
	case e.Err[int, error]:
		return e.Err[int, error]{Error: v.Error}
	default:
		panic("invalid Result at ?")
	}
	*events = append(*events, "last")
	return e.Ok[int, error]{Value: n + 1}
}
func TestQuestionResultMechanism(t *testing.T) {
	var p *typedNilError
	var err error = p
	v := fromGoInt(99, err)
	got, ok := v.(e.Err[int, error])
	if !ok || got.Error != err || got.Error == nil {
		t.Fatal("typed nil lost")
	}
	for _, success := range []bool{true, false} {
		events := []string{}
		calls := 0
		out := lowerQuestion(func() e.Result[int, error] {
			calls++
			events = append(events, "operand")
			if success {
				return fromGoInt(4, nil)
			}
			return v
		}, &events)
		want := []string{"first", "operand", "defer"}
		if success {
			want = []string{"first", "operand", "last", "defer"}
			if out.(e.Ok[int, error]).Value != 5 {
				t.Fatal(out)
			}
		} else if out.(e.Err[int, error]).Error != err {
			t.Fatal("error changed")
		}
		if calls != 1 || !reflect.DeepEqual(events, want) {
			t.Fatal(calls, events)
		}
	}
	rejects(t, func() { events := []string{}; lowerQuestion(func() e.Result[int, error] { return nil }, &events) })
}
func optionIncrement(call func() e.Option[int]) e.Option[int] {
	tmp := call()
	switch v := tmp.(type) {
	case nil:
		return nil
	case e.Some[int]:
		return e.Some[int]{Value: v.Value + 1}
	default:
		panic("invalid Option at ?")
	}
}
func TestQuestionOptionAndClosureMechanism(t *testing.T) {
	calls := 0
	if optionIncrement(func() e.Option[int] { calls++; return nil }) != nil || calls != 1 {
		t.Fatal(calls)
	}
	if optionIncrement(func() e.Option[int] { return e.Some[int]{Value: 2} }).(e.Some[int]).Value != 3 {
		t.Fatal("some")
	}
	events := []string{}
	func() {
		events = append(events, "outer")
		inner := func() e.Result[int, error] {
			return lowerQuestion(func() e.Result[int, error] { return e.Err[int, error]{Error: io.EOF} }, &events)
		}
		_ = inner()
		events = append(events, "outer-continued")
	}()
	if !reflect.DeepEqual(events, []string{"outer", "first", "defer", "outer-continued"}) {
		t.Fatal(events)
	}
}

type partialReader struct{}

func (partialReader) Read(p []byte) (int, error) { p[0] = 'x'; return 1, io.EOF }
func TestQuestionDoesNotReplacePartialGoResult(t *testing.T) {
	buf := make([]byte, 2)
	n, err := (partialReader{}).Read(buf)
	if n != 1 || err != io.EOF || buf[0] != 'x' {
		t.Fatal(n, err)
	}
	// This explicit adapter deliberately drops n on error. It is not an automatic
	// lowering for Read, whose partial result must be processed by the caller.
	if _, ok := fromGoInt(n, err).(e.Err[int, error]); !ok {
		t.Fatal("expected explicit error branch")
	}
}
