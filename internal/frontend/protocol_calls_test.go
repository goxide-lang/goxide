package frontend

import (
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/protocols"
	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestProtocolSourceInstantiationEvidence(t *testing.T) {
	const owner = "example.test/protocolcalls"
	const prefix = `package calls
type U struct{}
type Ops struct{}
func (U) GoxideInternalTypeCarrierForSelf(U) Ops { panic("signature only") }
func (Ops) Abc() U { return U{} }
func F[T interface{ GoxideInternalTypeCarrierForSelf(T) S }, S interface{ ~struct{}; Abc() T }]() T { var ops S; return ops.Abc() }
`
	p := protocols.Definition{ID: protocols.ID{Owner: owner, Name: "P"}}
	q := protocols.Definition{ID: protocols.ID{Owner: owner, Name: "Q"}}
	function := func(name string, d protocols.Definition) protocols.GenericFunction {
		return protocols.GenericFunction{Name: name, SourceParams: []string{"T"}, HiddenParams: []string{"S"}, Bounds: []protocols.Bound{{Function: name, TypeParam: "T", CarrierParam: "S", Protocol: d}}}
	}
	for _, tc := range []struct {
		name, tail, want string
		native, missing  bool
		private          bool
		forward          *protocols.Definition
	}{
		{name: "inferred suffix call", tail: "func use() { _ = F[U]() }"},
		{name: "inferred suffix function value", tail: "var use = F[U]"},
		{name: "private function explicit suffix", tail: "var use = f[U, Ops]", private: true, want: "generated carrier parameters"},
		{name: "explicit suffix call", tail: "func use() { _ = F[U, Ops]() }", want: "generated carrier parameters"},
		{name: "explicit suffix function value", tail: "var use = F[U, Ops]", want: "generated carrier parameters"},
		{name: "transparent carrier alias does not expose suffix", tail: "type Witness = Ops\nvar use = F[U, Witness]", want: "generated carrier parameters"},
		{name: "ordinary Go accepts explicit suffix", tail: "var use = F[U, Ops]", native: true},
		{name: "structural Go carrier has no source impl", tail: "var use = F[U]", missing: true, want: "no explicit implementation"},
		{name: "generic forward", tail: "func G[T interface{ GoxideInternalTypeCarrierForSelf(T) S }, S interface{ ~struct{}; Abc() T }]() T { return F[T]() }", forward: &p},
		{name: "same shape different protocol cannot forward", tail: "func G[T interface{ GoxideInternalTypeCarrierForSelf(T) S }, S interface{ ~struct{}; Abc() T }]() T { return F[T]() }", forward: &q, want: "no explicit implementation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := "calls.gox"
			if tc.native {
				file = "calls.go"
			}
			source := []byte(prefix + tc.tail + "\n")
			functionName := "F"
			if tc.private {
				functionName = "f"
				source = []byte(strings.Replace(string(source), "func F[", "func f[", 1))
			}
			local := &protocols.Package{Owner: owner, Name: "calls", Definitions: []protocols.Definition{p, q}, Functions: []protocols.GenericFunction{function(functionName, p)}}
			if !tc.missing {
				local.Implementations = []protocols.Implementation{{Target: protocols.ID{Owner: owner, Name: "U"}, Protocol: p.ID}}
			}
			if tc.forward != nil {
				local.Functions = append(local.Functions, function("G", *tc.forward))
			}
			sources := map[string][]byte{file: source}
			u := &usePackage{original: sources}
			err := u.validateProtocolSourceCalls(owner, sources, map[string]*sourcemap.Map{file: sourcemap.Identity(file, source)}, local, nil, nil)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), file+":") {
				t.Fatalf("want mapped diagnostic containing %q, got %v", tc.want, err)
			}
		})
	}
}
