package stdsupport

import (
	"errors"
	"io"
	"reflect"
	"testing"
)

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func expectPanic(t *testing.T, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()
	f()
}

func TestReadOncePartialErrorAndCapacity(t *testing.T) {
	failures := []error{io.EOF, errors.New("partial failure")}
	for _, failure := range failures {
		dst := make([]byte, 4, 12)
		calls := 0
		result := ReadOnce(readerFunc(func(p []byte) (int, error) {
			calls++
			if len(p) != 4 || cap(p) != 4 {
				t.Fatalf("view len/cap = %d/%d", len(p), cap(p))
			}
			copy(p, "ab")
			return 2, failure
		}), dst)
		if calls != 1 || result.N != 2 || result.Err != failure || string(dst[:result.N]) != "ab" {
			t.Fatalf("lost partial result: %+v calls=%d dst=%q", result, calls, dst)
		}
		if cap(dst) != 12 {
			t.Fatal("caller header changed")
		}
	}
}

func TestReadOnceZeroProgress(t *testing.T) {
	calls := 0
	result := ReadOnce(readerFunc(func(p []byte) (int, error) { calls++; return 0, nil }), nil)
	if calls != 1 || result.N != 0 || result.Err != nil {
		t.Fatalf("zero progress changed: %+v calls=%d", result, calls)
	}
}

func TestReadOnceInvalidCountAndNil(t *testing.T) {
	expectPanic(t, func() { ReadOnce(nil, make([]byte, 2)) })
	for _, n := range []int{-1, 3} {
		dst := make([]byte, 2)
		calls := 0
		expectPanic(t, func() {
			ReadOnce(readerFunc(func(p []byte) (int, error) { calls++; p[0] = 9; return n, nil }), dst)
		})
		if calls != 1 || dst[0] != 9 {
			t.Fatal("post-call validation incorrectly undid effects")
		}
	}
}

func TestCopySliceOuterStorageAndAliases(t *testing.T) {
	value := 3
	src := []*int{&value}
	out := CopySlice(src)
	if &out[0] == &src[0] || out[0] != src[0] || cap(out) != len(out) {
		t.Fatal("wrong outer copy or inner alias")
	}
	*out[0] = 7
	if *src[0] != 7 {
		t.Fatal("lost inner alias")
	}
	out[0] = nil
	if src[0] == nil {
		t.Fatal("outer slots still shared")
	}
}

func TestCopySliceEmptyForms(t *testing.T) {
	if CopySlice[int](nil) != nil {
		t.Fatal("nilness lost")
	}
	out := CopySlice(make([]int, 0, 8))
	if out == nil || len(out) != 0 || cap(out) != 0 {
		t.Fatal("empty nonnil/cap mismatch")
	}
}

func TestCloneWithOrderAndNil(t *testing.T) {
	seen := []int{}
	out := CloneWith([]int{3, 1, 2}, func(n int) int { seen = append(seen, n); return n * 10 })
	if !reflect.DeepEqual(seen, []int{3, 1, 2}) || !reflect.DeepEqual(out, []int{30, 10, 20}) {
		t.Fatalf("order/result: %v %v", seen, out)
	}
	calls := 0
	clone := func(n int) int { calls++; return n }
	if CloneWith[int](nil, clone) != nil || calls != 0 {
		t.Fatal("nil clone behavior")
	}
	if CloneWith([]int{}, clone) == nil || calls != 0 {
		t.Fatal("empty clone behavior")
	}
	expectPanic(t, func() { CloneWith[int](nil, nil) })
}

func TestCloneWithCallbackDefinesSharing(t *testing.T) {
	src := [][]int{{1, 2}, {3}}
	copied := CloneWith(src, func(row []int) []int { return CopySlice(row) })
	shared := CloneWith(src, func(row []int) []int { return row })
	copied[0][0] = 9
	if src[0][0] != 1 {
		t.Fatal("explicit copy shares storage")
	}
	shared[1][0] = 8
	if src[1][0] != 8 {
		t.Fatal("identity callback should preserve sharing")
	}
}
