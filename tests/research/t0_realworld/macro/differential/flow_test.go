package macroflow

import (
	"reflect"
	"testing"
)

func nativeFlow(seed uint32) (result int, trace []int) {
	mark := func(n int) int { trace = append(trace, n); return n }
	defer func() { mark(9) }()
	local := int(seed & 7)
	for i := 0; i < 4; i++ {
		if mark(i) == local {
			break
		}
		result += func() int {
			if !(mark(5)&int(seed) != 0) {
				return -1
			}
			value := mark(6)
			return value + value
		}()
	}
	if mark(7)&int(seed) != 0 {
		return result + mark(8), trace
	}
	return result, trace
}

func check(t *testing.T, seed uint32) {
	t.Helper()
	got, trace := MacroFlow(seed)
	want, expectedTrace := nativeFlow(seed)
	if got != want || !reflect.DeepEqual(trace, expectedTrace) {
		t.Fatalf("seed %d: got (%d,%v), want (%d,%v)", seed, got, trace, want, expectedTrace)
	}
}
func TestBoundedDifferential(t *testing.T) {
	for seed := uint32(0); seed < 4096; seed++ {
		check(t, seed)
	}
}
func FuzzFlow(f *testing.F) {
	for _, seed := range []uint32{0, 1, 7, 42, 255, 0xffffffff} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed uint32) { check(t, seed) })
}
func BenchmarkMacro(b *testing.B) {
	for b.Loop() {
		MacroFlow(42)
	}
}
func BenchmarkNative(b *testing.B) {
	for b.Loop() {
		nativeFlow(42)
	}
}
