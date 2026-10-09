package frontend

import (
	"errors"
	"strings"
	"testing"

	"github.com/hxsf/goxide/internal/sourcemap"
)

func TestProtocolSourceNameReservations(t *testing.T) {
	for _, name := range []string{protocolTypeAnchor, protocolMetadataMarker, "__goxide_protocol_local", "GoxideCompilerProtocol_owned"} {
		for _, newline := range []string{"\n", "\r\n"} {
			source := []byte("package p" + newline + "var " + name + " int" + newline)
			err := checkProtocolSourceNames(map[string][]byte{"input.gox": source})
			var diagnostic *sourcemap.Error
			if !errors.As(err, &diagnostic) || diagnostic.Origin == nil || diagnostic.Origin.Line != 2 || diagnostic.Origin.Column != 5 || string(source[diagnostic.Origin.Start:diagnostic.Origin.End]) != name {
				t.Fatalf("reservation lost exact source token: %q: %v", source, err)
			}
		}
	}
	if err := checkProtocolSourceNames(map[string][]byte{
		"input.gox": []byte("package p\n// GoxideInternalTypeCarrierForSelf\nvar text=\"__goxide_protocol_local\"\n"),
		"native.go": []byte("package p\ntype Carrier struct{}\ntype Value struct{}\nfunc(Value) GoxideInternalTypeCarrierForSelf()Carrier{return Carrier{}}\n"),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolGeneratedNameConflicts(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"type", "type GoxideCompilerProtocol_owned struct{}", "symbol conflicts"},
		{"function", "func GoxideCompilerProtocol_owned(){}", "symbol conflicts"},
		{"variable", "var GoxideCompilerProtocol_owned int", "symbol conflicts"},
		{"constant", "const GoxideCompilerProtocol_owned=1", "symbol conflicts"},
		{"import", "import GoxideCompilerProtocol_owned \"example.org/native\"", "symbol conflicts"},
		{"method", "type Value struct{};func(Value) GoxideInternalTypeCarrierForSelf(){}", "anchor conflicts"},
		{"pointer method", "type Value struct{};func(*Value) GoxideInternalTypeCarrierForSelf(){}", "anchor conflicts"},
		{"alias method", "type Value struct{};type Alias=Value;func(Alias) GoxideInternalTypeCarrierForSelf(){}", "anchor conflicts"},
		{"field", "type Value struct{GoxideInternalTypeCarrierForSelf int}", "anchor conflicts"},
		{"embedded field", "type Value struct{GoxideInternalTypeCarrierForSelf}", "anchor conflicts"},
		{"embedded pointer field", "type Value struct{*GoxideInternalTypeCarrierForSelf}", "anchor conflicts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := []byte("package p\n" + tc.body + "\n")
			err := checkProtocolGeneratedNames(map[string][]byte{"native.go": source}, []string{"GoxideCompilerProtocol_owned"}, []string{"Value"})
			var diagnostic *sourcemap.Error
			if !errors.As(err, &diagnostic) || !strings.Contains(err.Error(), tc.want) || diagnostic.Origin == nil || diagnostic.Origin.File != "native.go" || diagnostic.Origin.Line != 2 {
				t.Fatalf("expected located %s: %v", tc.want, err)
			}
		})
	}
}

func TestProtocolNativeCarriersAndAPIReferencesRemainLegal(t *testing.T) {
	sources := map[string][]byte{"native.go": []byte(`package p
import api "example.org/api"
type Native struct{}
type NativeCarrier struct{}
func(Native) GoxideInternalTypeCarrierForSelf()NativeCarrier{return NativeCarrier{}}
type GoxideCompilerProtocol_unrelated struct{}
func GoxideInternalTypeCarrierForSelf(){}
var _ api.GoxideCompilerProtocol_owned
`)}
	if err := checkProtocolGeneratedNames(sources, []string{"GoxideCompilerProtocol_owned"}, []string{"Other"}); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolNativeAnchorAliasAcrossFiles(t *testing.T) {
	sources := map[string][]byte{
		"alias.go":  []byte("package p\ntype Alias=Value\n"),
		"method.go": []byte("package p\nfunc(Alias) GoxideInternalTypeCarrierForSelf(){}\n"),
	}
	if err := checkProtocolGeneratedNames(sources, nil, []string{"Value"}); err == nil || !strings.Contains(err.Error(), "method.go:2:13:") {
		t.Fatalf("cross-file alias hid anchor conflict: %v", err)
	}
}
