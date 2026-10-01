// Package traitpaths compares hand-written lowering strategies, not hgo output.
package traitpaths

import (
	"bytes"
	"io"
)

type Source struct {
	Data               []byte
	Pos, Reads, Writes int
}

func (s *Source) Read(p []byte) (int, error) {
	s.Reads++
	if s.Pos == len(s.Data) {
		return 0, io.EOF
	}
	n := copy(p, s.Data[s.Pos:])
	s.Pos += n
	return n, nil
}
func (s *Source) WriteTo(w io.Writer) (int64, error) {
	s.Writes++
	n, err := w.Write(s.Data[s.Pos:])
	s.Pos += n
	if err == nil && s.Pos < len(s.Data) {
		err = io.ErrShortWrite
	}
	return int64(n), err
}
func (s *Source) Reset()         { s.Pos = 0; s.Reads = 0; s.Writes = 0 }
func (s *Source) Step(n int) int { s.Pos += n; return s.Pos }

// A second runtime type makes the polymorphic benchmark genuinely polymorphic.
type Other struct{ Source }

func (s *Other) Step(n int) int { s.Pos += n; return s.Pos }

type Stepper interface{ Step(int) int }

type Value struct{ Target *Source }

func (v Value) Read(p []byte) (int, error) { return v.Target.Read(p) }
func (v Value) Step(n int) int             { return v.Target.Step(n) }

// Pointer wrapper uses the exact same field and method bodies as Value.
func Pointer(s *Source) *Value { return &Value{Target: s} }

// Distinct defined struct type: no added layout, but a different method set/type.
// Ordinary Go pointer conversion, no unsafe.Pointer; impossible for arbitrary
// interface targets, defined pointer receiver bases or unconstrained T.
type Defined Source

func (v *Defined) Read(p []byte) (int, error) { return (*Source)(v).Read(p) }
func (v *Defined) Step(n int) int             { return (*Source)(v).Step(n) }

type InterfaceValue struct{ Target io.Reader }

func (v InterfaceValue) Read(p []byte) (int, error) { return v.Target.Read(p) }

// Extra protocols are explicit, and valid here only for pure adoption.
type Full struct{ Target *Source }

func (v Full) Read(p []byte) (int, error)         { return v.Target.Read(p) }
func (v Full) WriteTo(w io.Writer) (int64, error) { return v.Target.WriteTo(w) }

func StepImpl(s *Source, n int) int   { return s.Step(n) }
func CustomStep(s *Source, n int) int { return s.Step(2 * n) }

type Dict[T any] struct{ Step func(T, int) int }

func Apply[T any](d Dict[T], v T, n int) int { return d.Step(v, n) }

var SourceDict = Dict[*Source]{Step: StepImpl}

// Automatically forwarding raw WriteTo bypasses the custom Read transformation.
type Upper struct{ Target *Source }

func (v Upper) Read(p []byte) (int, error) {
	n, err := v.Target.Read(p)
	copy(p[:n], bytes.ToUpper(p[:n]))
	return n, err
}

type LeakyUpper struct{ Upper }

func (v LeakyUpper) WriteTo(w io.Writer) (int64, error) { return v.Target.WriteTo(w) }
