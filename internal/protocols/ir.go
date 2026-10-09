// Package protocols implements source declarations and explicit implementation
// evidence for associated protocols. Ordinary Go interfaces are not protocols.
package protocols

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"

	"github.com/hxsf/goxide/internal/sourcemap"
)

const AnchorName = "GoxideInternalTypeCarrierForSelf"
const TypeInfoPackage = "github.com/goxide-lang/std/typeinfo"
const MetadataVersion = 1

// ID denotes declaration ownership, independently of an importing file's route.
type ID struct {
	Owner string
	Name  string
}

func (id ID) String() string { return id.Owner + "::" + id.Name }

type Member struct {
	Name, GoName, Params, Results string
	Signature                     *ast.FuncType  `json:"-"`
	Position                      token.Position `json:"-"`
}

type Definition struct {
	ID
	Public           bool
	ExportedEvidence bool
	OpsName          string
	MarkerName       string
	Members          []Member
	// Route is the file-local Go spelling of OpsName for an imported protocol.
	Route string `json:"-"`
	File  string `json:"-"`
}

type Implementation struct {
	Protocol    ID
	Target      ID
	CarrierName string
	Members     []Member
	File        string `json:"-"`
}

type Bound struct {
	Function, TypeParam, CarrierParam string
	Protocol                          Definition
	File                              string `json:"-"`
}

type GenericFunction struct {
	Name                       string
	SourceParams, HiddenParams []string
	Bounds                     []Bound
}

type Metadata struct {
	Version         int
	Path, Package   string
	Definitions     []Definition
	Implementations []Implementation
	Functions       []GenericFunction
}

func (m Metadata) Present() bool {
	return len(m.Definitions)+len(m.Implementations)+len(m.Functions) != 0
}
func (m Metadata) Marker() string {
	b, _ := json.Marshal(m)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// Associated records the selected source operation. Dispatch always uses
// typeinfo.Of[Receiver]().Member.GoName; Receiver is U or T, never the carrier.
type Associated struct {
	File                    string
	Protocol                ID
	Target                  ID
	Receiver, CarrierName   string
	TypeParam, CarrierParam string
	Member                  Member
	Generic                 bool
}

type Options struct {
	Single         bool
	LookupProtocol func(file, spelling string) (Definition, bool, error)
	Associated     []Associated
}

type Package struct {
	Owner           string
	Name            string
	Definitions     []Definition
	Implementations []Implementation
	GenericBindings map[string]map[string][]Bound
	Functions       []GenericFunction
	options         Options
	declarations    map[string]Definition
	targets         map[string]target
}

type Result struct {
	Source               []byte
	LateDeclarations     []byte
	GenericFunctions     map[string]bool
	GeneratedSymbols     []string
	AnchorTargets        []string
	GeneratedIdentifiers map[string]bool
}

type target struct {
	Name, File string
	Alias      bool
	Kind       string
}

// CollectPackage is package-wide: forward declarations and implementations in
// sibling files share identity, while each declaration retains its own file.
func CollectPackage(owner string, sources map[string][]byte, options Options) (*Package, error) {
	return collectPackage(owner, sources, options)
}

// LowerFile reparses the supplied source, so macro expansion does not leave
// stale offsets. It may be called on a cloned map for declaration collection.
func (p *Package) LowerFile(file string, source []byte, mapping *sourcemap.Map) (Result, error) {
	return p.lowerFile(file, source, mapping)
}

func (p *Package) ExportMetadata() Metadata {
	m := Metadata{Version: MetadataVersion, Path: p.Owner, Package: p.Name}
	for _, d := range p.Definitions {
		if d.Public || d.ExportedEvidence {
			m.Definitions = append(m.Definitions, d)
		}
	}
	for _, i := range p.Implementations {
		if ast.IsExported(i.Target.Name) {
			m.Implementations = append(m.Implementations, i)
		}
	}
	for _, f := range p.Functions {
		if ast.IsExported(f.Name) {
			m.Functions = append(m.Functions, f)
		}
	}
	return m
}

// SemanticMetadata includes private declarations for local validation. Export
// filtering is a publication concern and must not erase local impl evidence.
func (p *Package) SemanticMetadata() Metadata {
	return Metadata{Version: MetadataVersion, Path: p.Owner, Package: p.Name, Definitions: p.Definitions, Implementations: p.Implementations, Functions: p.Functions}
}

func (p *Package) LookupAssociated(file, function, receiver, member string) (Associated, bool, error) {
	if p == nil {
		return Associated{}, false, nil
	}
	for _, b := range p.GenericBindings[file][function] {
		if b.TypeParam != receiver {
			continue
		}
		for _, m := range b.Protocol.Members {
			if m.Name == member {
				return Associated{Protocol: b.Protocol.ID, Receiver: receiver, TypeParam: b.TypeParam, CarrierParam: b.CarrierParam, Member: m, Generic: true}, true, nil
			}
		}
		return Associated{}, false, fmt.Errorf("protocol %s has no associated member %s", b.Protocol.ID.String(), member)
	}
	var found *Associated
	for _, impl := range p.Implementations {
		if impl.Target.Name != receiver {
			continue
		}
		for _, m := range impl.Members {
			if m.Name != member {
				continue
			}
			if found != nil {
				return Associated{}, false, fmt.Errorf("ambiguous associated member %s::%s", receiver, member)
			}
			a := Associated{Protocol: impl.Protocol, Target: impl.Target, Receiver: receiver, CarrierName: impl.CarrierName, Member: m}
			found = &a
		}
	}
	if found != nil {
		return *found, true, nil
	}
	for _, a := range p.options.Associated {
		if a.File == file && a.Receiver == receiver && a.Member.Name == member {
			return a, true, nil
		}
	}
	return Associated{}, false, nil
}

func OpsName(id ID, public bool) string    { return generatedName(id, "Ops", public) }
func MarkerName(id ID, public bool) string { return generatedName(id, "Evidence", public) }
func DefinitionNames(id ID, public bool) (string, string) {
	return OpsName(id, public), MarkerName(id, public)
}
func CarrierName(id ID) string { return generatedName(id, "Carrier", ast.IsExported(id.Name)) }

func generatedName(id ID, suffix string, public bool) string {
	digest := sha256.Sum256([]byte(id.String()))
	prefix := "__goxide_protocol_"
	if public {
		prefix = "GoxideCompilerProtocol_"
	}
	return fmt.Sprintf("%s%x_%s", prefix, digest[:], suffix)
}
