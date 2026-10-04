package goxidesync

import (
	"fmt"
	"sync"
	"testing"
)

func TestDoResultsAndAlias(t *testing.T) {
	s := NewLock[int32](0)
	alias := s
	n := alias.Do(func(p *int32) int32 { *p += 1; return *p })
	label := s.Do(func(p *int32) string { return fmt.Sprint(*p) })
	valid := s.Do[bool](func(p *int32) bool { return *p == 1 })
	if n != 1 || label != "1" || !valid {
		t.Fatal(n, label, valid)
	}
	s.Do(func(p *int32) struct{} { *p += 1; return struct{}{} })
	if got := alias.Do(func(p *int32) int32 { return *p }); got != 2 {
		t.Fatal(got)
	}
}
func TestDoPanicUnlocks(t *testing.T) {
	s := NewLock(0)
	func() {
		defer func() {
			if r := recover(); r != "probe" {
				t.Fatalf("panic: %v", r)
			}
		}()
		s.Do(func(p *int) int { panic("probe") })
	}()
	if !s.mu.TryLock() {
		t.Fatal("lock remained held")
	}
	s.mu.Unlock()
	if got := s.Do(func(p *int) int { return *p + 7 }); got != 7 {
		t.Fatal(got)
	}
}
func TestRWResultsAndPanicUnlock(t *testing.T) {
	s := NewRWLock(1)
	n := s.Write(func(p *int) int { *p += 2; return *p })
	label := s.Read(func(p *int) string { return fmt.Sprint(*p) })
	if n != 3 || label != "3" {
		t.Fatal(n, label)
	}
	for _, run := range []func(){
		func() { s.Read(func(p *int) bool { panic("read") }) },
		func() { s.Write(func(p *int) bool { panic("write") }) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("expected panic")
				}
			}()
			run()
		}()
		if !s.mu.TryLock() {
			t.Fatal("RW lock remained held")
		}
		s.mu.Unlock()
	}
}
func TestConcurrentSharedHandle(t *testing.T) {
	s := NewLock(0)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		alias := s
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 32; j++ {
				alias.Do(func(p *int) struct{} { *p += 1; return struct{}{} })
			}
		}()
	}
	wg.Wait()
	if got := s.Do(func(p *int) int { return *p }); got != 256 {
		t.Fatal(got)
	}
}
