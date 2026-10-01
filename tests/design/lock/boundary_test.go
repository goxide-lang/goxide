package hgosync

import "testing"

// These passing Go probes document normal behavior and evidence for risk warnings.
// They are not hgo rejection tests; unknown effects must not imply an error.
// All accesses are sequential: race detection cannot establish borrow validity.
type borrowedCounterView struct{ target *int }

func (v borrowedCounterView) Increment() int {
	*v.target += 1
	return *v.target
}

func TestGoBorrowViewMethodEscapes(t *testing.T) {
	s := NewLock(1)
	method := s.Do(func(p *int) func() int {
		return borrowedCounterView{target: p}.Increment
	})
	if got := method(); got != 2 {
		t.Fatal(got)
	}
	if got := s.Do(func(p *int) int { return *p }); got != 2 {
		t.Fatal("escaped method did not retain the original storage", got)
	}
}

func TestGoLockShallowAliases(t *testing.T) {
	type state struct{ data []int }
	original := []int{1}
	s := NewLock(state{data: original})
	original[0] = 2
	copy := s.Do(func(p *state) state { return *p })
	if copy.data[0] != 2 {
		t.Fatal("constructor unexpectedly copied backing storage")
	}
	copy.data[0] = 3
	if got := s.Do(func(p *state) int { return p.data[0] }); got != 3 || original[0] != 3 {
		t.Fatal("returned struct unexpectedly copied backing storage", got, original)
	}
}

func TestGoReadAllowsMutation(t *testing.T) {
	s := NewRWLock(1)
	s.Read(func(p *int) struct{} { *p = 2; return struct{}{} })
	if got := s.Read(func(p *int) int { return *p }); got != 2 {
		t.Fatal(got)
	}
}

func TestGoBorrowEscapesViaPanic(t *testing.T) {
	s := NewLock(1)
	var escaped *int
	func() {
		defer func() {
			value := recover()
			var ok bool
			escaped, ok = value.(*int)
			if !ok || escaped == nil {
				t.Fatalf("unexpected panic payload: %v", value)
			}
		}()
		s.Do(func(p *int) int { panic(p) })
	}()
	*escaped = 4
	if got := s.Do(func(p *int) int { return *p }); got != 4 {
		t.Fatal("panic payload did not retain original storage", got)
	}
}

// A Go error interface is not evidence that its payload is borrow-free.
type borrowedError struct{ target *int }

func (e borrowedError) Error() string {
	if *e.target == 1 {
		return "before"
	}
	return "after"
}

func TestGoErrorRetainsBorrow(t *testing.T) {
	s := NewLock(1)
	err := s.Do(func(p *int) error { return borrowedError{target: p} })
	if err.Error() != "before" {
		t.Fatal("unexpected initial error")
	}
	s.Do(func(p *int) struct{} { *p = 2; return struct{}{} })
	if err.Error() != "after" {
		t.Fatal("error did not retain original storage")
	}
}

func TestGoScalarArraySnapshotIndependent(t *testing.T) {
	s := NewRWLock([3]int{1, 2, 3})
	snapshot := s.Read(func(p *[3]int) [3]int { return *p })
	done := make(chan int, 1)
	go func(values [3]int) {
		done <- values[0] + values[1] + values[2]
	}(snapshot)
	s.Write(func(p *[3]int) struct{} { p[0] = 100; return struct{}{} })
	if got := <-done; got != 6 {
		t.Fatal("snapshot changed with lock state", got)
	}
	if got := s.Read(func(p *[3]int) int { return p[0] }); got != 100 {
		t.Fatal(got)
	}
}

func newLocalPointer() *int {
	value := 7
	return &value
}

func TestGoLocalPointerReturn(t *testing.T) {
	p := newLocalPointer()
	alias := p
	*alias += 1
	if *p != 8 {
		t.Fatal("ordinary local pointer return lost storage", *p)
	}
}

func TestGoReadOnlyPointerSharing(t *testing.T) {
	value := 9
	p := &value
	results := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func(shared *int) { results <- *shared }(p)
	}
	for i := 0; i < 2; i++ {
		if got := <-results; got != 9 {
			t.Fatal(got)
		}
	}
}

func TestReturnedPointerUsedUnderSameLock(t *testing.T) {
	s := NewLock(1)
	p := s.Do(func(value *int) *int { return value })
	s.Do(func(value *int) struct{} {
		if value != p {
			t.Fatal("different protected storage")
		}
		*p += 1
		return struct{}{}
	})
	if got := s.Do(func(value *int) int { return *value }); got != 2 {
		t.Fatal(got)
	}
}
