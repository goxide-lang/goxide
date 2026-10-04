package main

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"trial.example/app/logkit"
	cfg "trial.example/model/config"
)

func nativeStep(n int) (int, error) {
	if n < 0 {
		return 0, cfg.Negative
	}
	return n + 1, nil
}
func nativeSum(items []int) (int, error) {
	sum := 0
	for _, n := range items {
		v, e := nativeStep(n)
		if e != nil {
			return 0, e
		}
		sum += v
	}
	return sum, nil
}
func nativeProcess(items []int, multiplier int) (total int, trace []int, calls int, err error) {
	defer func() { trace = append(trace, 99) }()
	for i, n := range items {
		trace = append(trace, i)
		v, e := nativeStep(n)
		if e != nil {
			return 0, trace, calls, e
		}
		total += v * multiplier
		calls++
	}
	return
}
func checkPipeline(t *testing.T, data []byte) {
	t.Helper()
	if len(data) > 64 {
		data = data[:64]
	}
	items := make([]int, len(data))
	for i, b := range data {
		items[i] = int(int8(b))
	}
	for _, multiplier := range []int{1, 0, 3, -2} {
		mode := cfg.NewModeBatch(multiplier)
		if multiplier == 1 {
			mode = cfg.NewModeImmediate()
		}
		want, wt, wcalls, we := nativeProcess(items, multiplier)
		for _, enabled := range []bool{false, true} {
			logkit.Calls = 0
			trace := []int(nil)
			got, err := Process(items, mode, enabled, &trace)
			calls := 0
			if enabled {
				calls = wcalls
			}
			if got != want || !reflect.DeepEqual(trace, wt) || logkit.Calls != calls || errors.Is(err, cfg.Negative) != errors.Is(we, cfg.Negative) || (err == nil) != (we == nil) {
				t.Fatalf("input=%v multiplier=%d enabled=%v got=(%d,%v,%d,%v), want=(%d,%v,%d,%v)", items, multiplier, enabled, got, trace, logkit.Calls, err, want, wt, calls, we)
			}
			if err != nil {
				var f Failure
				if !errors.As(err, &f) || errors.Unwrap(err) != cfg.Negative {
					t.Fatalf("lost failure identity: %v", err)
				}
			}
		}
	}
}
func TestBoundedPipeline(t *testing.T) {
	alphabet := []byte{0, 1, 127, 128, 255}
	var visit func([]byte, int)
	visit = func(data []byte, depth int) {
		checkPipeline(t, data)
		if depth == 0 {
			return
		}
		for _, v := range alphabet {
			next := append(append([]byte(nil), data...), v)
			visit(next, depth-1)
		}
	}
	visit(nil, 4) // 781 inputs * 4 modes * 2 logging states.
}
func FuzzPipeline(f *testing.F) {
	for _, v := range [][]byte{nil, {0, 1, 127}, {255}, {1, 2, 128, 3}} {
		f.Add(v)
	}
	f.Fuzz(func(t *testing.T, data []byte) { checkPipeline(t, data) })
}
func TestContainerProperties(t *testing.T) {
	for _, items := range [][]int{nil, {}, {0}, {1, 2, 3}} {
		s := Snapshot{Data: cfg.Some(items)}
		c := CloneSnapshot(&s)
		if !Same(s, c) || (cfg.Items(c.Data) == nil) != (items == nil) {
			t.Fatalf("clone state: %s", DebugSnapshot(&s))
		}
		if len(items) > 0 {
			cfg.Items(c.Data)[0]++
			if Same(s, c) || cfg.Items(s.Data)[0] != items[0] {
				t.Fatal("clone aliased")
			}
		}
	}
}

var benchInt int
var benchErr error

func BenchmarkBatchSum(b *testing.B) {
	for _, size := range []int{1, 32, 1024} {
		items := make([]int, size)
		for i := range items {
			items[i] = 1000 + i
		}
		b.Run(fmt.Sprintf("hgo/%d", size), func(b *testing.B) {
			for b.Loop() {
				benchInt, benchErr = SumGoBoundary(items)
			}
		})
		b.Run(fmt.Sprintf("native/%d", size), func(b *testing.B) {
			for b.Loop() {
				benchInt, benchErr = nativeSum(items)
			}
		})
	}
}
func BenchmarkDispatch(b *testing.B) {
	m := cfg.NewModeBatch(1000)
	b.Run("hgo", func(b *testing.B) {
		for b.Loop() {
			benchInt = Dispatch(m)
		}
	})
	b.Run("native-same-abi", func(b *testing.B) {
		for b.Loop() {
			switch v := m.(type) {
			case cfg.HgoCase4_Mode_Immediate:
				benchInt = 1
			case cfg.HgoCase4_Mode_Batch:
				benchInt = v.Value
			default:
				panic("invalid")
			}
		}
	})
}

type typedNilCause struct{}

func (*typedNilCause) Error() string { return "typed nil cause" }
func TestTypedNilCause(t *testing.T) {
	var ptr *typedNilCause
	for _, cause := range []error{nil, ptr, cfg.Negative} {
		got := WrappedCause(cause)
		if got == nil {
			t.Fatal("Err collapsed into nil")
		}
		if errors.Unwrap(got) != cause {
			t.Fatalf("cause identity changed: %#v", got)
		}
		var typed *typedNilCause
		if cause != nil && cause != cfg.Negative && (!errors.As(got, &typed) || typed != nil) {
			t.Fatal("typed nil lost")
		}
	}
}
func TestJSONBusinessGrid(t *testing.T) {
	// Exercise actual JSON -> independent producer -> consumer match/? -> Error.
	for id := -3; id <= 9; id++ {
		for units := -1; units <= 7; units++ {
			for _, retry := range []string{"null", "0", "3"} {
				input := fmt.Sprintf(`{"Name":"%d","Mode":{"Batch":%d},"Retry":%s}`, id, units, retry)
				got, err := Run(input, false)
				if units <= 0 || units > 5 {
					if !errors.Is(err, OverCapacity) {
						t.Fatalf("%s: %v", input, err)
					}
				} else if err != nil || got.ID != id || got.Units != units || got.Port != 8080 {
					t.Fatalf("%s: %+v %v", input, got, err)
				}
			}
		}
	}
}
