package frontend

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"github.com/hxsf/goxide/internal/protocols"
)

// Positions are in the initial protocol collection view. The frontend maps
// them through the corresponding use source map, not the final generated Go.
// Imported JSON deliberately has no Position and remains an artifact error.
type protocolDeclarationError struct {
	Position token.Position
	Message  string
}

func (e *protocolDeclarationError) Error() string {
	if e.Position.IsValid() {
		return fmt.Sprintf("%s: %s", e.Position, e.Message)
	}
	return e.Message
}

func protocolMemberError(member protocols.Member, message string) error {
	return &protocolDeclarationError{Position: member.Position, Message: message}
}

func validateProtocolMetadata(meta protocols.Metadata, pkg *types.Package, imp types.Importer) error {
	return validateProtocolRecords(meta, pkg, imp, true)
}

func validateProtocolDeclarations(meta protocols.Metadata, pkg *types.Package, imp types.Importer) error {
	return validateProtocolRecords(meta, pkg, imp, false)
}

func validateProtocolRecords(meta protocols.Metadata, pkg *types.Package, imp types.Importer, published bool) error {
	if pkg == nil || meta.Version != protocols.MetadataVersion || meta.Path != pkg.Path() || meta.Package != pkg.Name() {
		return fmt.Errorf("protocol metadata package/schema identity mismatch")
	}
	definitions := map[protocols.ID]protocols.Definition{}
	ops := map[protocols.ID]*types.Named{}
	for _, d := range meta.Definitions {
		if _, duplicate := definitions[d.ID]; duplicate {
			return fmt.Errorf("duplicate protocol definition %s", d.ID.String())
		}
		actual, err := validateProtocolDefinition(d, pkg, imp, published)
		if err != nil {
			return err
		}
		definitions[d.ID], ops[d.ID] = d, actual
	}
	lookup := func(id protocols.ID) (protocols.Definition, *types.Named, error) {
		if d, ok := definitions[id]; ok {
			return d, ops[id], nil
		}
		if id.Owner == meta.Path {
			return protocols.Definition{}, nil, fmt.Errorf("missing canonical protocol definition %s", id.String())
		}
		owner, err := loadProtocolArtifact(id.Owner, imp)
		if err != nil {
			return protocols.Definition{}, nil, err
		}
		d, ok := lookupProtocolEvidence(owner, id)
		if !ok || d.ID != id {
			return protocols.Definition{}, nil, fmt.Errorf("missing canonical protocol definition %s", id.String())
		}
		ownerPkg, err := imp.Import(id.Owner)
		if err != nil {
			return protocols.Definition{}, nil, err
		}
		actual, err := validateProtocolDefinition(d, ownerPkg, imp, true)
		if err == nil {
			definitions[id], ops[id] = d, actual
		}
		return d, actual, err
	}
	impls := map[string]bool{}
	for _, impl := range meta.Implementations {
		key := impl.Protocol.String() + " for " + impl.Target.String()
		if impls[key] {
			return fmt.Errorf("duplicate protocol implementation %s", key)
		}
		impls[key] = true
		if impl.Target.Owner != meta.Path || !token.IsIdentifier(impl.Target.Name) || (published && !ast.IsExported(impl.Target.Name)) || impl.CarrierName != protocols.CarrierName(impl.Target) {
			return fmt.Errorf("protocol implementation target/carrier owner mismatch: %s", key)
		}
		target, err := protocolNamed(pkg, impl.Target.Name)
		if err != nil || target.TypeParams().Len() != 0 {
			return fmt.Errorf("protocol implementation requires canonical concrete target %s", impl.Target.String())
		}
		if _, ok := target.Underlying().(*types.Struct); !ok {
			return fmt.Errorf("protocol implementation target must be a defined struct: %s", key)
		}
		carrier, err := protocolNamed(pkg, impl.CarrierName)
		if err != nil || carrier.TypeParams().Len() != 0 {
			return fmt.Errorf("protocol carrier identity mismatch: %s", key)
		}
		shape, ok := carrier.Underlying().(*types.Struct)
		if !ok || shape.NumFields() != 0 {
			return fmt.Errorf("protocol carrier must have zero fields: %s", key)
		}
		anchor, err := protocolDirectMethod(target, protocols.AnchorName)
		if err != nil || anchor.Variadic() || anchor.TypeParams().Len() != 0 || anchor.Params().Len() != 1 || anchor.Results().Len() != 1 || !types.Identical(anchor.Params().At(0).Type(), target) || !types.Identical(anchor.Results().At(0).Type(), carrier) {
			return fmt.Errorf("protocol exact-Self anchor signature mismatch: %s", key)
		}
		d, operation, err := lookup(impl.Protocol)
		if err != nil {
			return err
		}
		instantiated, err := types.Instantiate(nil, operation, []types.Type{target}, true)
		if err != nil {
			return fmt.Errorf("protocol operation instantiation: %w", err)
		}
		constraint := instantiated.Underlying().(*types.Interface)
		if len(impl.Members) != len(d.Members) {
			return fmt.Errorf("protocol implementation member set mismatch: %s", key)
		}
		seen := map[string]bool{}
		for _, member := range impl.Members {
			if seen[member.Name] {
				return fmt.Errorf("duplicate protocol implementation member: %s", member.Name)
			}
			seen[member.Name] = true
			found := false
			for _, want := range d.Members {
				found = found || (member.Name == want.Name && member.GoName == want.GoName)
			}
			if !found {
				return fmt.Errorf("protocol implementation member identity mismatch: %s", member.Name)
			}
			actual, err := protocolDirectMethod(carrier, member.GoName)
			if err != nil {
				return protocolMemberError(member, fmt.Sprintf("protocol implementation member %s: %s", member.Name, err))
			}
			method := protocolInterfaceMethod(constraint, member.GoName)
			if method == nil || !types.Identical(actual, method.Type()) {
				return protocolMemberError(member, fmt.Sprintf("protocol implementation operation signature mismatch: %s for %s", member.Name, impl.Protocol.String()))
			}
		}
		if !types.Satisfies(carrier, constraint) {
			return fmt.Errorf("protocol carrier does not satisfy canonical operation signatures: %s", key)
		}
		if _, err := protocolDirectMethod(carrier, d.MarkerName); err != nil {
			return fmt.Errorf("protocol implementation marker must belong to its carrier: %s", key)
		}
	}
	functions := map[string]bool{}
	for _, function := range meta.Functions {
		if functions[function.Name] || !token.IsIdentifier(function.Name) || (published && !ast.IsExported(function.Name)) || len(function.Bounds) == 0 || len(function.HiddenParams) == 0 {
			return fmt.Errorf("invalid or duplicate protocol generic function: %s", function.Name)
		}
		functions[function.Name] = true
		obj, ok := pkg.Scope().Lookup(function.Name).(*types.Func)
		if !ok || obj.Pkg() != pkg {
			return fmt.Errorf("protocol generic function owner mismatch: %s", function.Name)
		}
		sig, ok := obj.Type().(*types.Signature)
		if !ok || sig.Recv() != nil || sig.TypeParams().Len() != len(function.SourceParams)+len(function.HiddenParams) {
			return fmt.Errorf("protocol generic function parameter arity mismatch: %s", function.Name)
		}
		parameters := map[string]*types.TypeParam{}
		hidden := map[string]bool{}
		names := append(append([]string(nil), function.SourceParams...), function.HiddenParams...)
		for i, name := range names {
			if !token.IsIdentifier(name) || parameters[name] != nil || sig.TypeParams().At(i).Obj().Name() != name {
				return fmt.Errorf("protocol generic parameter identity/order mismatch: %s", function.Name)
			}
			parameters[name] = sig.TypeParams().At(i)
			if i >= len(function.SourceParams) {
				hidden[name] = true
			}
		}
		if err := validateTypeInfoSupport(imp); err != nil {
			return err
		}
		typeinfo, err := imp.Import(protocols.TypeInfoPackage)
		if err != nil {
			return err
		}
		carrierFor := typeinfo.Scope().Lookup("CarrierFor").Type()
		boundParameters := map[string]bool{}
		usedHidden := map[string]bool{}
		for _, bound := range function.Bounds {
			self, carrier := parameters[bound.TypeParam], parameters[bound.CarrierParam]
			if bound.Function != function.Name || self == nil || carrier == nil || hidden[bound.TypeParam] || !hidden[bound.CarrierParam] || boundParameters[bound.TypeParam] || usedHidden[bound.CarrierParam] {
				return fmt.Errorf("protocol generic bound parameter mismatch: %s", function.Name)
			}
			boundParameters[bound.TypeParam], usedHidden[bound.CarrierParam] = true, true
			d, operation, err := lookup(bound.Protocol.ID)
			if err != nil {
				return err
			}
			if !protocolDefinitionsEqual(d, bound.Protocol) {
				return fmt.Errorf("protocol generic bound definition mismatch: %s", function.Name)
			}
			wantSelf, err := types.Instantiate(nil, carrierFor, []types.Type{self, carrier}, false)
			if err != nil || !types.Identical(self.Constraint().Underlying(), wantSelf.Underlying()) {
				return fmt.Errorf("protocol generic exact-Self carrier constraint mismatch: %s", function.Name)
			}
			wantOps, err := types.Instantiate(nil, operation, []types.Type{self}, true)
			if err != nil {
				return err
			}
			empty := types.NewUnion([]*types.Term{types.NewTerm(true, types.NewStruct(nil, nil))})
			wantCarrier := types.NewInterfaceType(nil, []types.Type{empty, wantOps}).Complete()
			if !types.Identical(carrier.Constraint().Underlying(), wantCarrier) {
				return fmt.Errorf("protocol generic operation/empty-carrier constraint mismatch: %s", function.Name)
			}
		}
		if len(usedHidden) != len(hidden) {
			return fmt.Errorf("unregistered hidden protocol evidence parameter: %s", function.Name)
		}
	}
	return nil
}

func validateProtocolDefinition(d protocols.Definition, pkg *types.Package, imp types.Importer, published bool) (*types.Named, error) {
	if (published && !d.Public && !d.ExportedEvidence) || !token.IsIdentifier(d.Name) || d.Owner == "" || len(d.Members) == 0 {
		return nil, fmt.Errorf("invalid exported protocol definition: %s", d.ID.String())
	}
	opsName, markerName := protocols.DefinitionNames(d.ID, d.Public || d.ExportedEvidence)
	if d.OpsName != opsName || d.MarkerName != markerName {
		return nil, fmt.Errorf("protocol generated identity differs from canonical owner: %s", d.ID.String())
	}
	origin := pkg
	if d.Owner != pkg.Path() {
		var err error
		origin, err = imp.Import(d.Owner)
		if err != nil {
			return nil, err
		}
		if origin == nil || origin.Path() != d.Owner {
			return nil, fmt.Errorf("protocol definition owner unavailable: %s", d.Owner)
		}
	}
	ops, err := protocolNamed(origin, d.OpsName)
	if err != nil || ops.TypeParams().Len() != 1 || !types.Identical(ops.TypeParams().At(0).Constraint(), types.NewInterfaceType(nil, nil).Complete()) {
		return nil, fmt.Errorf("protocol operation interface requires canonical [Self any]: %s", d.ID.String())
	}
	if origin != pkg {
		route, ok := pkg.Scope().Lookup(d.OpsName).(*types.TypeName)
		if !ok || !route.IsAlias() || !sameUseType(route.Type(), ops) {
			return nil, fmt.Errorf("protocol re-export operation alias identity mismatch: %s", d.ID.String())
		}
	}
	iface, ok := ops.Underlying().(*types.Interface)
	if !ok {
		return nil, fmt.Errorf("protocol operations must be an interface: %s", d.ID.String())
	}
	iface.Complete()
	if iface.NumMethods() != len(d.Members)+1 || iface.NumEmbeddeds() != 0 {
		return nil, fmt.Errorf("protocol operation member set mismatch: %s", d.ID.String())
	}
	marker := protocolInterfaceMethod(iface, d.MarkerName)
	if marker == nil || marker.Pkg() != origin {
		return nil, fmt.Errorf("protocol canonical marker missing: %s", d.ID.String())
	}
	markerSig := marker.Type().(*types.Signature)
	if markerSig.TypeParams().Len() != 0 || markerSig.Variadic() || markerSig.Params().Len() != 0 || markerSig.Results().Len() != 0 {
		return nil, fmt.Errorf("protocol canonical marker signature mismatch: %s", d.ID.String())
	}
	names := map[string]bool{}
	for _, member := range d.Members {
		if !token.IsIdentifier(member.Name) || member.GoName != member.Name || names[member.Name] || member.GoName == d.MarkerName || member.GoName == protocols.AnchorName {
			return nil, fmt.Errorf("invalid protocol member identity: %s", member.Name)
		}
		names[member.Name] = true
		method := protocolInterfaceMethod(iface, member.GoName)
		if method == nil || method.Pkg() != origin {
			return nil, fmt.Errorf("protocol associated operation missing: %s", member.Name)
		}
		if method.Type().(*types.Signature).TypeParams().Len() != 0 {
			return nil, fmt.Errorf("independently generic protocol operations unsupported: %s", member.Name)
		}
	}
	return ops, nil
}

func protocolNamed(pkg *types.Package, name string) (*types.Named, error) {
	obj, ok := pkg.Scope().Lookup(name).(*types.TypeName)
	if !ok || obj.IsAlias() {
		return nil, fmt.Errorf("protocol canonical named type missing: %s.%s", pkg.Path(), name)
	}
	named, ok := obj.Type().(*types.Named)
	if !ok || named.Obj().Pkg() != pkg || named.Obj().Name() != name || named.TypeArgs().Len() != 0 {
		return nil, fmt.Errorf("protocol canonical named type identity mismatch: %s.%s", pkg.Path(), name)
	}
	return named, nil
}

func protocolDirectMethod(named *types.Named, name string) (*types.Signature, error) {
	selection := types.NewMethodSet(named).Lookup(named.Obj().Pkg(), name)
	if selection == nil || len(selection.Index()) != 1 {
		return nil, fmt.Errorf("protocol method must be declared on its canonical receiver: %s.%s", named.Obj().Name(), name)
	}
	method, ok := selection.Obj().(*types.Func)
	if !ok || method.Pkg() != named.Obj().Pkg() {
		return nil, fmt.Errorf("protocol method owner mismatch: %s", name)
	}
	sig, ok := method.Type().(*types.Signature)
	if !ok || sig.Recv() == nil || !types.Identical(sig.Recv().Type(), named) {
		return nil, fmt.Errorf("protocol method receiver mismatch: %s", name)
	}
	return sig, nil
}

func protocolInterfaceMethod(iface *types.Interface, name string) *types.Func {
	for i := 0; i < iface.NumMethods(); i++ {
		if iface.Method(i).Name() == name {
			return iface.Method(i)
		}
	}
	return nil
}
