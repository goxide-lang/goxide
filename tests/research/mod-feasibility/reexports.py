#!/usr/bin/env python3
"""Finite binding-graph experiment, NOT a source parser or production resolver.

Each source module contributes public bindings to stable declaration identities.
Glob/explicit exports add edges. A monotone fixed point propagates identities;
ambiguity is diagnosed after convergence, independent of visitation order.
"""
from dataclasses import dataclass
import unittest


@dataclass(frozen=True)
class Def:
    identity: str
    kind: str = 'type'
    public: bool = True


@dataclass(frozen=True)
class Edge:
    source: str
    name: str = '*'  # Explicit name or a glob.
    alias: str = ''


def resolve(declarations, exports):
    """declarations[module][name]=Def; exports[module]=list[Edge].

    Scope/path authorization is NOT modeled here: caller must have authorized
    access to source modules. This models only public surface propagation.
    """
    identities = {}
    for names in declarations.values():
        for definition in names.values():
            prior = identities.setdefault(definition.identity, definition)
            if prior != definition:
                raise ValueError('inconsistent declaration identity')
    modules = set(declarations) | set(exports)
    surface = {m: {n: {d} for n, d in declarations.get(m, {}).items() if d.public}
               for m in modules}
    for edges in exports.values():
        for edge in edges:
            if edge.source not in modules:
                raise ValueError('unknown module')
            if edge.name == '*' and edge.alias:
                raise ValueError('glob cannot have one alias')
    changed = True
    while changed:
        changed = False
        for module in sorted(modules):
            for edge in exports.get(module, []):
                source = surface[edge.source]
                names = list(source) if edge.name == '*' else [edge.name]
                for name in names:
                    identities = source.get(name, set())
                    if not identities:
                        continue
                    dest = surface[module].setdefault(edge.alias or name, set())
                    before = len(dest)
                    dest.update(identities)
                    changed |= len(dest) != before
    for module in sorted(modules):
        for edge in exports.get(module, []):
            if edge.name != '*' and edge.name not in surface[edge.source]:
                raise ValueError('unresolved or non-public explicit export')
        for name, values in sorted(surface[module].items()):
            if len(values) != 1:
                raise ValueError(f'ambiguous {module}::{name}')
            # Reject an accidental collision with a module-local private binding.
            local = declarations.get(module, {}).get(name)
            if local and local not in values:
                raise ValueError('export collides with local declaration')
    # All exports in an empty glob-only cycle converge to empty. This model
    # accepts that no-op; production policy can reject the cycle with a warning.
    return {m: {n: next(iter(ds)) for n, ds in names.items()}
            for m, names in surface.items()}


def go_facade(bindings):
    """Classify exact Go aliases vs explicit adapters; do not generate code."""
    exact, adapters = {}, {}
    for name, definition in bindings.items():
        if definition.kind in ('type', 'constant'):
            exact[name] = definition.identity
        elif definition.kind == 'function':
            adapters[name] = definition.identity
        else:
            raise ValueError(f'Go facade cannot transparently re-export {definition.kind}')
    return exact, adapters


class Reexports(unittest.TestCase):
    def test_explicit_alias_preserves_identity(self):
        d = Def('source::T')
        result = resolve({'source': {'T': d}}, {'api': [Edge('source', 'T', 'Renamed')]})
        self.assertIs(result['api']['Renamed'], d)

    def test_glob_and_transitive(self):
        d = Def('source::T')
        result = resolve({'source': {'T': d}}, {'middle': [Edge('source')], 'api': [Edge('middle')]})
        self.assertIs(result['api']['T'], d)

    def test_duplicate_same_identity_coalesces(self):
        d = Def('source::T')
        result = resolve({'a': {'T': d}, 'b': {'T': d}}, {'api': [Edge('a'), Edge('b')]})
        self.assertEqual(result['api'], {'T': d})

    def test_conflict_distinct_globs(self):
        with self.assertRaisesRegex(ValueError, 'ambiguous'):
            resolve({'a': {'T': Def('a::T')}, 'b': {'T': Def('b::T')}},
                    {'api': [Edge('a'), Edge('b')]})

    def test_explicit_does_not_silently_override_glob(self):
        with self.assertRaisesRegex(ValueError, 'ambiguous'):
            resolve({'a': {'T': Def('a::T')}, 'b': {'T': Def('b::T')}},
                    {'api': [Edge('a'), Edge('b', 'T')]})

    def test_private_excluded_from_glob(self):
        result = resolve({'source': {'secret': Def('source::secret', public=False)}},
                         {'api': [Edge('source')]})
        self.assertEqual(result['api'], {})

    def test_private_explicit_rejected(self):
        with self.assertRaisesRegex(ValueError, 'non-public'):
            resolve({'source': {'secret': Def('source::secret', public=False)}},
                    {'api': [Edge('source', 'secret')]})

    def test_private_local_collision(self):
        with self.assertRaisesRegex(ValueError, 'collides'):
            resolve({'source': {'T': Def('source::T')},
                     'api': {'T': Def('api::T', public=False)}}, {'api': [Edge('source')]})

    def test_pure_explicit_cycle_rejected(self):
        with self.assertRaisesRegex(ValueError, 'unresolved'):
            resolve({}, {'a': [Edge('b', 'T')], 'b': [Edge('a', 'T')]})

    def test_glob_cycle_with_seed_terminates(self):
        d = Def('a::T')
        result = resolve({'a': {'T': d}}, {'a': [Edge('b')], 'b': [Edge('a')]})
        self.assertEqual(result['b'], {'T': d})

    def test_empty_glob_cycle_is_empty_model_only(self):
        result = resolve({}, {'a': [Edge('b')], 'b': [Edge('a')]})
        self.assertEqual(result, {'a': {}, 'b': {}})

    def test_visitation_order_independent(self):
        decls = {'a': {'T': Def('a::T')}, 'b': {'U': Def('b::U')}}
        self.assertEqual(resolve(decls, {'api': [Edge('a'), Edge('b')]}),
                         resolve(decls, {'api': [Edge('b'), Edge('a')]}))

    def test_function_is_adapter_not_alias(self):
        exact, adapters = go_facade({'T': Def('p::T'), 'F': Def('p::F', 'function')})
        self.assertEqual(exact, {'T': 'p::T'})
        self.assertEqual(adapters, {'F': 'p::F'})

    def test_multiple_conflicts_have_stable_first_diagnostic(self):
        decls = {'left': {'Z': Def('left::Z'), 'A': Def('left::A')},
                 'right': {'Z': Def('right::Z'), 'A': Def('right::A')}}
        with self.assertRaisesRegex(ValueError, 'ambiguous api::A'):
            resolve(decls, {'api': [Edge('left'), Edge('right')]})

    def test_same_identity_inconsistent_metadata_rejected(self):
        with self.assertRaisesRegex(ValueError, 'inconsistent declaration identity'):
            resolve({'a': {'T': Def('same', 'type')},
                     'b': {'T': Def('same', 'function')}}, {})

    def test_mutable_variable_and_macro_go_facade_rejected(self):
        for kind in ('variable', 'macro'):
            with self.assertRaisesRegex(ValueError, 'cannot transparently'):
                go_facade({'X': Def('p::X', kind)})


if __name__ == '__main__':
    unittest.main(verbosity=2)
