# Enum representation and match lowering

`Lower(filename, source)` returns generated Go and `Decl` / `Variant` metadata.
`LowerPackage` also accepts independently collected package declarations. Source
positions and copied token origins survive lowering.

```goxide
enum Event { Idle, Item(int), Failed(string), }
func update(e &mut Event) {
    match *e {
        Event::Item(ref mut n) => { *n += 1 },
        Event::Failed(ref message) => { _ = *message },
        Event::Idle => {},
    }
}
```

An enum is a nominal Go struct containing a private integer tag and a separate,
inline, typed slot for each payload variant. Same-typed variants have distinct
slots. Constructors initialize the tag and active slot. Assignment preserves Go
value-copy semantics, including ordinary shallow copies of slices, maps, and
pointers inside supported payloads. Local enum zero values are invalid; standard
`Option[T]` has zero-value `None`, while zero-value `Result[T,E]` is invalid.

`Decl.Tag` reads the tag; `Decl.Validator` checks the active tag and supported
nested enum values. Each payload variant has a checked value getter and a checked
`*Enum -> *Payload` projector. A projector returns the address of the actual
inline slot and rejects a nil owner or wrong active tag when called. Public enums
export constructors, validation, tag/value/projector helpers, and transparent
payload type aliases needed to preserve private payload identity across package
boundaries. There are no interface case values or exported case wrappers.

Ordinary `match e` saves one value snapshot. Payload names bind copied values by
default; `ref p` and `ref mut p` request references to the original place. Basic
`match &e` and `match &mut e` select read-reference and write-reference defaults.
Patterns support one payload identifier or `_`, whole-value bindings and `_`, and
source-order guards. This is not full nested-pattern or recursive match ergonomics.
Unguarded arms determine exhaustiveness and unreachable-arm errors. Wildcard-only
matches resolve the enum identity through later type analysis.

A borrowed match evaluates the original place once, saves its value and tag, then
captures the selected variant's required payload handle before any user guard.
Later arms use the entry value snapshot for value bindings and reuse that handle
for reference bindings. Generated reference requests are bound to registered
projectors through the existing checked-reference analysis; they do not grant
permission to arbitrary native pointers. The final Go projector introduces no
runtime reference wrapper or extra function frame around an arm.

There is no new lifetime, escape, exclusive-borrow, generation, or owner-replacement
check. References may be copied, saved, returned and captured according to ordinary
reference permissions. An owner assignment changes its actual inline slots; an
old pointer remains a pointer to its original slot. Garbage collection keeps an
escaped slot's allocation alive. Replacing an owner does not make an old slot
reference track a newly allocated owner or validate its current active variant.

Validation occurs before any guard or body, including catch-all arms. It does not
validate every assignment or function boundary. Branches use `if` statements,
without hidden loops or closures, preserving arm `return`, `defer`, `break`, and
`continue`. Go may still require a trailing return after an exhaustive source
match because it does not prove the generated conditional chain exhaustive.

The shared standard `Option` / `Result` family uses `std/sum` ABI 3. Builtin
constructors, tag readers, getters, projectors, validation and propagation all use
that canonical type identity. Contextual patterns are serialized as versioned
compiler IR containing exact copied token origins and binding/place mode; the
propagation stage resolves their real type before completing lowering.

Payload support remains the finite existing set: scalar values, supported local
structs, arrays, slices, maps, and acyclic nested enums. Unsupported recursive
projection graphs, enum-bearing pointers, unknown external object graphs, source
reference payload types, and user-defined generic enum declarations remain
explicitly diagnosed. This change does not silently broaden those qualifications.
