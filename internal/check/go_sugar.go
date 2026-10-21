package check

import (
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SugarMember is a checked Go-only forwarding method. It does not add an
// inherent member or establish a source trait implementation.
type SugarMember struct {
	Impl      *Impl
	Member    *Member
	Receiver  types.Type
	Name      string
	Signature *types.Signature // Receiver is kept separately.
}

func (c *checker) prepareGoSugar() {
	file, pkg, bounds, active := c.file, c.pkg, c.bounds, c.activeBounds
	impl, trait, self := c.currentImpl, c.currentTrait, c.currentSelf
	defer func() {
		c.file, c.pkg, c.bounds, c.activeBounds = file, pkg, bounds, active
		c.currentImpl, c.currentTrait, c.currentSelf = impl, trait, self
	}()
	c.bounds, c.activeBounds = map[*types.TypeParam][]*TraitBound{}, nil
	c.currentImpl, c.currentTrait, c.currentSelf = nil, nil, nil
	type key struct {
		target *types.Named
		name   string
	}
	explicit, fixed := map[key]bool{}, map[key]bool{}
	for _, implementation := range c.implOrder {
		target := ordinaryGoReceiverTarget(implementation)
		if target == nil {
			continue
		}
		if implementation.Trait == nil {
			if !CanUseGoReceiver(implementation) {
				continue
			}
			for _, member := range implementation.Methods {
				if !member.Static {
					explicit[key{target, member.GoName}] = true
				}
			}
			continue
		}
		if implementation.Trait.Runtime && CanUseGoReceiver(implementation) {
			fixed[key{target, "As" + sugarExportedName(implementation.Trait.Symbol.GoName)}] = true
		}
		if len(implementation.TypeParams) == 0 && target.TypeParams().Len() == 0 {
			fixed[key{target, "GoxideInternalTypeCarrierForSelf"}] = true
		}
	}
	candidates := map[key][]*SugarMember{}
	var order []key
	for _, implementation := range c.implOrder {
		target := ordinaryGoReceiverTarget(implementation)
		if target == nil || implementation.Trait == nil || !c.out.ConfigByFile[implementation.File].GoSugarEnabled() {
			continue
		}
		c.file, c.pkg = c.out.Resolve.Files[implementation.File], implementation.Owner
		// A source prerequisite on an unbound parameter cannot add a Go method
		// to every instantiation of the receiver.
		selected := c.matchImpl(implementation, universalGoTarget(implementation))
		if selected == nil {
			continue
		}
		for _, member := range selected.Methods {
			if member.Static {
				continue
			}
			name := sugarExportedName(member.GoName)
			identity := key{target, name}
			owned := explicit[identity]
			for i := 0; i < target.NumMethods(); i++ {
				owned = owned || target.Method(i).Name() == name
			}
			if owned {
				continue
			}
			receiver := selected.Target
			if member.Receiver != ValueRef {
				receiver = types.NewPointer(receiver)
			}
			candidate := &SugarMember{Impl: selected, Member: member, Receiver: receiver, Name: name, Signature: member.Signature}
			if len(candidates[identity]) == 0 {
				order = append(order, identity)
			}
			candidates[identity] = append(candidates[identity], candidate)
		}
	}
	for _, identity := range order {
		members := candidates[identity]
		first := members[0]
		c.file, c.pkg = c.out.Resolve.Files[first.Impl.File], first.Impl.Owner
		var conflict string
		if fixed[identity] {
			conflict = "fixed generated member"
		} else if structure, ok := identity.target.Underlying().(*types.Struct); ok {
			for i := 0; i < structure.NumFields(); i++ {
				if structure.Field(i).Name() == identity.name {
					conflict = "field"
				}
			}
		}
		if conflict != "" {
			c.diag(first.Member.Decl, TraitInvalid, "Go sugar method "+identity.name+" conflicts with "+conflict)
			continue
		}
		if len(members) > 1 {
			names := make([]string, len(members))
			for i, member := range members {
				names[i] = member.Impl.Trait.Symbol.Name.Name
			}
			c.diag(first.Member.Decl, TraitInvalid, "Go sugar method "+identity.name+" is provided by multiple traits: "+strings.Join(names, ", "))
			continue
		}
		c.out.Sugar = append(c.out.Sugar, first)
	}
	c.goInterop = newGoInterop(c)
}

func sugarExportedName(name string) string {
	name = strings.TrimLeft(name, "_")
	if name == "" {
		return "Value"
	}
	first, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(first)) + name[size:]
}
