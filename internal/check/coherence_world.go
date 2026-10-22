package check

import (
	"go/types"
	"sort"
	"strings"
)

type coherenceFutureBody struct {
	goal    *TraitBound
	owner   *types.Package // nil is a fresh downstream package.
	newRoot int            // -1 uses an existing owner; otherwise a fresh root.
}

func (c *checker) coherenceDependencies() map[*types.Package]map[*types.Package]bool {
	edges := map[*types.Package]map[*types.Package]bool{}
	files := map[string]*types.Package{}
	paths := map[string]*types.Package{}
	add := func(from, to *types.Package) {
		if from == nil || to == nil || from == to {
			return
		}
		if edges[from] == nil {
			edges[from] = map[*types.Package]bool{}
		}
		edges[from][to] = true
	}
	for source, pkg := range c.packages {
		paths[pkg.Path()] = pkg
		if canonical := c.out.PackagePaths[pkg.Path()]; canonical != "" {
			paths[canonical] = pkg
		}
		for _, file := range source.Files {
			files[file.Name] = pkg
		}
	}
	var native func(*types.Package)
	visited := map[*types.Package]bool{}
	native = func(pkg *types.Package) {
		if visited[pkg] {
			return
		}
		visited[pkg], paths[pkg.Path()] = true, pkg
		for _, dependency := range pkg.Imports() {
			add(pkg, dependency)
			native(dependency)
		}
	}
	for _, pkg := range c.imported {
		native(pkg)
	}
	for source, pkg := range c.packages {
		for _, file := range source.Files {
			for _, use := range file.Uses {
				if use.Resolved != nil {
					add(pkg, files[use.Resolved.Decl.File])
				}
				path := strings.Join(use.Target, "/")
				for path != "" {
					if target := paths[path]; target != nil {
						add(pkg, target)
						break
					}
					cut := strings.LastIndex(path, "/")
					if cut < 0 {
						break
					}
					path = path[:cut]
				}
			}
		}
	}
	// Canonical types also cover names reached through reexports or inferred
	// private types, without relying on private visibility to close the world.
	for _, impl := range c.implOrder {
		seen := map[types.Type]bool{}
		var visit func(types.Type)
		visit = func(typ types.Type) {
			if typ == nil || seen[typ] {
				return
			}
			seen[typ] = true
			if named, ok := types.Unalias(typ).(*types.Named); ok {
				add(impl.Owner, named.Obj().Pkg())
			}
			if array, ok := c.out.arrayShape(typ); ok {
				visit(array.Elem)
				return
			}
			typeChildren(typ, func(child types.Type) bool { visit(child); return false })
		}
		visit(impl.Target)
		for _, input := range impl.Inputs {
			visit(input)
		}
		if impl.Trait != nil {
			add(impl.Owner, impl.Trait.Type.Obj().Pkg())
		}
		for _, requirement := range impl.Requirements {
			add(impl.Owner, requirement.Trait.Type.Obj().Pkg())
			visit(requirement.Target)
			for _, input := range requirement.Inputs {
				visit(input)
			}
		}
	}
	return edges
}

func (r *coherenceState) depends(from, to *types.Package) bool {
	seen := map[*types.Package]bool{}
	var visit func(*types.Package) bool
	visit = func(pkg *types.Package) bool {
		if pkg == to {
			return true
		}
		if seen[pkg] {
			return false
		}
		seen[pkg] = true
		for next := range r.dependencies[pkg] {
			if visit(next) {
				return true
			}
		}
		return false
	}
	return visit(from)
}

func (s *coherenceConstraints) futureBodies(goal *TraitBound, fixed *types.Package) []coherenceFutureBody {
	owners := map[*types.Package]bool{nil: true}
	owners[goal.Trait.Type.Obj().Pkg()] = true
	for _, pkg := range s.c.packages {
		owners[pkg] = true
	}
	for _, q := range s.c.boundHeadPermissions(goal) {
		if named := ownerRoot(s.applied(q)); named != nil {
			owners[named.Obj().Pkg()] = true
		}
	}
	var order []*types.Package
	for owner := range owners {
		if owner != fixed {
			order = append(order, owner)
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i] == nil {
			return order[j] != nil
		}
		return order[j] != nil && order[i].Path() < order[j].Path()
	})
	var result []coherenceFutureBody
	for _, owner := range order {
		for index := -1; index < 1+len(goal.Inputs); index++ {
			branch := coherenceFutureBody{goal, owner, index}
			if s.futureAllowed(branch) {
				result = append(result, branch)
			}
		}
	}
	return result
}

func (s *coherenceConstraints) futureAllowed(future coherenceFutureBody) bool {
	traitOwner := future.goal.Trait.Type.Obj().Pkg()
	if future.owner != nil && future.owner != traitOwner && s.c.coherence.depends(traitOwner, future.owner) {
		return false
	}
	head := s.c.boundHeadPermissions(future.goal)
	for i, q := range head {
		head[i] = s.applied(q)
	}
	if future.newRoot >= 0 {
		// A new local root can replace an unresolved whole head position, through
		// native pointer chains only. It cannot replace a nested type argument.
		parameter := uncoveredParameter(head[future.newRoot])
		if parameter == nil || !s.variables[parameter] || s.c.out.isArray(parameter) {
			return false
		}
		for i := 0; i < future.newRoot; i++ {
			if !s.futurePattern(head[i], future.owner, false, map[types.Type]bool{}) {
				return false
			}
		}
		return true
	}
	owned := future.owner != nil && future.owner == traitOwner
	for _, q := range head {
		if root := ownerRoot(q); root != nil && future.owner != nil && root.Obj().Pkg() == future.owner {
			owned = true
		}
		if !s.futurePattern(q, future.owner, false, map[types.Type]bool{}) {
			return false
		}
	}
	return owned
}

// An unnameable subtree must be generalized by a parameter. Existing-root
// extensions reserve only declarations in which every such use is covered.
func (s *coherenceConstraints) futurePattern(q *permission, owner *types.Package, covered bool, seen map[types.Type]bool) bool {
	q = s.resolve(q)
	if q == nil || seen[q.typ] {
		return true
	}
	seen[q.typ] = true
	if named, ok := types.Unalias(q.typ).(*types.Named); ok {
		pkg := named.Obj().Pkg()
		if pkg != nil && owner != nil && pkg != owner && s.c.coherence.depends(pkg, owner) {
			return covered
		}
		for i := 0; i < named.TypeArgs().Len(); i++ {
			if !s.futurePattern(sourceAt(q.args, i, named.TypeArgs().At(i)), owner, true, seen) {
				return false
			}
		}
		return true
	}
	if q.ref != ValueRef {
		return s.futurePattern(sourceChild(q.elem, pointerElement(q.typ)), owner, true, seen)
	}
	if pointer, ok := types.Unalias(q.typ).(*types.Pointer); ok {
		return s.futurePattern(sourceChild(q.elem, pointer.Elem()), owner, covered, seen)
	}
	if _, ok := types.Unalias(q.typ).(*types.TypeParam); ok && !s.c.out.isArray(q.typ) {
		// The existence query can choose a nameable concrete type here. Later
		// conjuncts recheck this condition after any further substitutions.
		return true
	}
	if array, ok := s.c.out.arrayShape(q.typ); ok {
		return s.futurePattern(sourceChild(q.elem, array.Elem), owner, true, seen)
	}
	return !typeChildren(q.typ, func(child types.Type) bool { return !s.futurePattern(s.source(child), owner, true, seen) })
}

func (s *coherenceConstraints) futureValid() bool {
	roots := map[*types.TypeParam]*types.Package{}
	edges := map[*types.Package][]*types.Package{}
	for _, branch := range s.future {
		if !s.futureAllowed(branch) {
			return false
		}
		if branch.newRoot >= 0 {
			root := uncoveredParameter(s.applied(s.c.boundHeadPermissions(branch.goal)[branch.newRoot]))
			if previous, found := roots[root]; found && previous != branch.owner {
				return false
			}
			roots[root] = branch.owner
		}
		traitOwner := branch.goal.Trait.Type.Obj().Pkg()
		if branch.owner != traitOwner {
			edges[branch.owner] = append(edges[branch.owner], traitOwner)
		}
	}
	// Individually legal added imports must also form one legal future graph.
	state := map[*types.Package]uint8{}
	var visit func(*types.Package) bool
	visit = func(pkg *types.Package) bool {
		if state[pkg] == 1 {
			return false
		}
		if state[pkg] == 2 {
			return true
		}
		state[pkg] = 1
		for dependency := range s.c.coherence.dependencies[pkg] {
			if !visit(dependency) {
				return false
			}
		}
		for _, dependency := range edges[pkg] {
			if !visit(dependency) {
				return false
			}
		}
		state[pkg] = 2
		return true
	}
	for pkg := range edges {
		if !visit(pkg) {
			return false
		}
	}
	return true
}
