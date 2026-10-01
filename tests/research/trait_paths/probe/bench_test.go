package traitpaths

import (
	"io"
	"testing"
)

var sink int
var retained Stepper
var retainedReader io.Reader
var retainedDict struct {
	Target *Source
	D      Dict[*Source]
}

func BenchmarkLocal(b *testing.B) {
	b.Run("raw", func(b *testing.B) {
		s := &Source{}
		n := 0
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			n = s.Step(1)
		}
		sink = n
	})
	b.Run("value", func(b *testing.B) {
		s := &Source{}
		v := Value{s}
		n := 0
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			n = v.Step(1)
		}
		sink = n
	})
	b.Run("pointer", func(b *testing.B) {
		s := &Source{}
		v := Pointer(s)
		n := 0
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			n = v.Step(1)
		}
		sink = n
	})
	b.Run("defined", func(b *testing.B) {
		s := &Source{}
		v := (*Defined)(s)
		n := 0
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			n = v.Step(1)
		}
		sink = n
	})
	b.Run("direct-function", func(b *testing.B) {
		s := &Source{}
		n := 0
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			n = StepImpl(s, 1)
		}
		sink = n
	})
	b.Run("typed-dictionary", func(b *testing.B) {
		s := &Source{}
		n := 0
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			n = Apply(SourceDict, s, 1)
		}
		sink = n
	})
	b.Run("static-dictionary", func(b *testing.B) {
		s := &Source{}
		d := Dict[*Source]{Step: StepImpl}
		n := 0
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			n = Apply(d, s, 1)
		}
		sink = n
	})

	b.Run("known-interface", func(b *testing.B) {
		s := &Source{}
		var v Stepper = Value{s}
		n := 0
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			n = v.Step(1)
		}
		sink = n
	})
}

// Retention is a real escape through a global interface. All paths reuse the
// same pre-existing target; only adaptation is measured, no fresh target cost.
func BenchmarkRetain(b *testing.B) {
	s := &Source{}
	b.Run("raw", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			retained = s
		}
	})
	b.Run("value", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			retained = Value{s}
		}
	})
	b.Run("pointer", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			retained = Pointer(s)
		}
	})
	b.Run("defined", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			retained = (*Defined)(s)
		}
	})
	b.Run("interface-holder", func(b *testing.B) {
		var v io.Reader = s
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			retainedReader = InterfaceValue{v}
		}
	})
	b.Run("typed-dictionary", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			retainedDict.Target = s
			retainedDict.D = SourceDict
		}
	})
}
func BenchmarkPolymorphic(b *testing.B) {
	s, o := &Source{}, &Other{}
	choices := []Stepper{s, o, Value{s}, Pointer(s), (*Defined)(s)}
	n := 0
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n = choices[i%len(choices)].Step(1)
	}
	sink = n
}
func BenchmarkCopy(b *testing.B) {
	data := make([]byte, 4096)
	cases := []struct {
		name  string
		adapt func(*Source) io.Reader
	}{{"raw", func(s *Source) io.Reader { return s }}, {"value", func(s *Source) io.Reader { return Value{s} }}, {"pointer", func(s *Source) io.Reader { return Pointer(s) }}, {"defined", func(s *Source) io.Reader { return (*Defined)(s) }}, {"interface-holder", func(s *Source) io.Reader { return InterfaceValue{s} }}, {"explicit-full", func(s *Source) io.Reader { return Full{s} }}}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			s := &Source{Data: data}
			b.ReportAllocs()
			b.SetBytes(int64(len(data)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.Reset()
				n, err := io.Copy(io.Discard, c.adapt(s))
				if err != nil || n != int64(len(data)) {
					b.Fatal(n, err)
				}
			}
		})
	}
}
