package traitpaths

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"unsafe"
)

func TestCopyProtocols(t *testing.T) {
	cases := []struct {
		name  string
		adapt func(*Source) io.Reader
		fast  bool
	}{
		{"raw", func(s *Source) io.Reader { return s }, true},
		{"value", func(s *Source) io.Reader { return Value{s} }, false},
		{"pointer", func(s *Source) io.Reader { return Pointer(s) }, false},
		{"defined", func(s *Source) io.Reader { return (*Defined)(s) }, false},
		{"interface", func(s *Source) io.Reader { return InterfaceValue{s} }, false},
		{"explicit-full", func(s *Source) io.Reader { return Full{s} }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &Source{Data: []byte("abc")}
			var out bytes.Buffer
			n, err := io.Copy(&out, c.adapt(s))
			if err != nil || n != 3 || out.String() != "abc" {
				t.Fatal(n, err, out.String())
			}
			if (s.Writes == 1) != c.fast {
				t.Fatal("dispatch", s.Reads, s.Writes)
			}
			t.Logf("Read=%d WriteTo=%d", s.Reads, s.Writes)
		})
	}
}

type causeError struct{ cause error }

func (e *causeError) Error() string { return "cause" }
func (e *causeError) Unwrap() error { return e.cause }

type errorView struct{ target error }

func (v errorView) Error() string { return v.target.Error() }

type errorFull struct{ target *causeError }

func (v errorFull) Error() string { return v.target.Error() }
func (v errorFull) Unwrap() error { return v.target.Unwrap() }
func TestErrorProtocol(t *testing.T) {
	sentinel := errors.New("sentinel")
	raw := &causeError{sentinel}
	for _, c := range []struct {
		name string
		v    error
		want bool
	}{{"raw", raw, true}, {"value", errorView{raw}, false}, {"pointer", &errorView{raw}, false}, {"explicit-full", errorFull{raw}, true}} {
		got := errors.Is(c.v, sentinel)
		if got != c.want {
			t.Fatal(c.name, got)
		}
		t.Logf("%s Is=%v", c.name, got)
	}
}

type httpView struct{ target http.ResponseWriter }

func (v httpView) Header() http.Header         { return v.target.Header() }
func (v httpView) Write(p []byte) (int, error) { return v.target.Write(p) }
func (v httpView) WriteHeader(n int)           { v.target.WriteHeader(n) }

type httpFull struct{ httpView }

func (v httpFull) Flush() { v.target.(http.Flusher).Flush() }
func TestHTTPProtocol(t *testing.T) {
	raw := httptest.NewRecorder()
	for _, c := range []struct {
		name string
		v    http.ResponseWriter
		want bool
	}{{"raw", raw, true}, {"value", httpView{raw}, false}, {"pointer", &httpView{raw}, false}, {"explicit-full", httpFull{httpView{raw}}, true}} {
		err := http.NewResponseController(c.v).Flush()
		if (err == nil) != c.want {
			t.Fatal(c.name, err)
		}
		t.Logf("%s Flush=%v", c.name, err)
	}
	// Explicit adapter above is for a known Flusher, not every ResponseWriter.
}
func TestAutomaticForwardingBypassesImpl(t *testing.T) {
	a := &Source{Data: []byte("abc")}
	var out bytes.Buffer
	io.Copy(&out, Upper{a})
	if out.String() != "ABC" {
		t.Fatal(out.String())
	}
	out.Reset()
	a.Reset()
	io.Copy(&out, LeakyUpper{Upper{a}})
	if out.String() != "abc" {
		t.Fatal(out.String())
	}
	t.Log("forwarded WriterTo bypasses custom uppercase Read")
	s := &Source{}
	if CustomStep(s, 3) != 6 {
		t.Fatal("custom impl lost")
	}
	s.Reset()
	if StepImpl(s, 3) != 3 {
		t.Fatal("native implementation")
	}
}
func TestIdentityAndStorage(t *testing.T) {
	s := &Source{}
	v := Value{s}
	d := (*Defined)(s)
	v.Step(1)
	d.Step(2)
	if s.Pos != 3 {
		t.Fatal("copied state")
	}
	if any(s) == any(v) || any(s) == any(d) {
		t.Fatal("dynamic identity unexpectedly equal")
	}
	if (Value{s}) != v {
		t.Fatal("value equality")
	}
	p1, p2 := Pointer(s), Pointer(s)
	if p1 == p2 {
		t.Fatal("pointer wrapper identities")
	}
	var nilSource *Source
	if any(Value{nilSource}) == nil {
		t.Fatal("nil target is not nil view")
	}
	t.Logf("sizes: pointer=%d value=%d pointer-wrapper=%d defined=%d interface-holder=%d dict=%d", unsafe.Sizeof(s), unsafe.Sizeof(v), unsafe.Sizeof(p1), unsafe.Sizeof(d), unsafe.Sizeof(InterfaceValue{}), unsafe.Sizeof(SourceDict))
	t.Logf("dynamic types: %v %v %v", reflect.TypeOf(s), reflect.TypeOf(v), reflect.TypeOf(d))
}
