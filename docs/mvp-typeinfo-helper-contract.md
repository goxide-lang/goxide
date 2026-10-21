# MVP typeinfo helper contract inventory

This is a narrow inventory for [issue #22](https://github.com/goxide-lang/goxide/issues/22), based on compiler `cf56a52` and the selected `github.com/goxide-lang/std` version `v0.0.0-20261009104839-c02a09d8c417`. It covers `typeinfo.Of` and only the `CarrierFor`/anchor contract needed to judge it. It does not design a general helper-checking framework or implement checks.

## Contract evidence

At std commit [`c02a09d`](https://github.com/goxide-lang/std/blob/c02a09d8c417b35dc7e432fc484172fab6db6182/typeinfo/typeinfo.go), the declarations are:

```go
type CarrierFor[T any, S ~struct{}] interface {
    GoxideInternalTypeCarrierForSelf(T) S
}

func Of[T CarrierFor[T, S], S ~struct{}]() S {
    var zero S
    return zero
}
```

`S ~struct{}` permits only types whose underlying type is the zero-field struct; it does not mean any zero-size type. `Of` returns the carrier zero value and does not construct `T`, call the anchor, or inspect a runtime value. See the [std regression](https://github.com/goxide-lang/std/blob/c02a09d8c417b35dc7e432fc484172fab6db6182/typeinfo/typeinfo_test.go), which covers inferred/explicit `S`, a type alias, pointer receivers, no anchor invocation, and rejection of wrong Self, promoted parent, wrong method, nonempty/interface carrier, wrong carrier identity, and pointer-only method set.

The compiler checks the selected package/module identity and ABI 1, the named `CarrierFor` shape, exact anchor signature, and `Of` signature in [`protocol_typeinfo.go`](../internal/frontend/protocol_typeinfo.go#L14). Its protocol metadata validator independently checks generated carrier identity, zero fields, exact target anchor and operation signatures in [`protocol_validation.go`](../internal/frontend/protocol_validation.go#L80). Acceptance coverage includes a plain Go consumer, a handwritten conforming carrier whose anchor panics if executed, and rejection of a replaced std package with a wrong `Of` result ([test](../tests/acceptance/static_protocol_test.go#L107), [ABI mismatch case](../tests/acceptance/static_protocol_test.go#L277)).

## Lightweight decision inventory

| Condition | Decision | Evidence and boundary |
| --- | --- | --- |
| Carrier method has exact `T` input and returns the selected `S`; promoted parent or pointer-only methods must not silently qualify. | Existing Go type-system guarantee. | `CarrierFor[T,S]` requires the exact method signature; the std regression has wrong-Self, promoted-parent, wrong-method, wrong-carrier, and pointer-method-set negatives. No extra assertion is justified. |
| `S` is an empty struct carrier. | Existing Go type-system guarantee for the shape; existing compiler metadata check for generated identity. | `S ~struct{}` excludes fields while permitting named empty-struct types. Compiler metadata independently checks that its generated named carrier has zero fields and matches the recorded target/identity. Do not add a runtime size/reflection check: zero-size non-struct types are not equivalent to `~struct{}`. |
| `Of` has no value parameters and returns the zero value of `S`, without constructing `T` or executing the anchor. | Existing helper contract, enforced by Go type checking and selected-ABI validation; keep regression coverage. | The implementation uses `var zero S; return zero`; std tests use a panicking/counting anchor. Compiler ABI validation checks canonical package function, arity, type parameters, constraints, and result. No runtime assertion candidate exists because there is no runtime value to inspect. |
| Selected `typeinfo` package really implements the compiler's ABI. | Existing compile-time check. | [`validateTypeInfoSupport`](../internal/frontend/protocol_typeinfo.go#L14) checks import path, module origin, ABI constant, `CarrierFor`, anchor, and `Of` signature. Replaced dependency with a wrong `Of` result is rejected in the acceptance test. This check belongs to Goxide compilation, not direct `go build`. |
| A structurally conforming ordinary Go carrier proves a Goxide `impl` exists. | Intentionally unchecked by `typeinfo.Of`; Goxide impl membership instead requires validated compiler metadata. | The std package explicitly says structural constraints do not prove compiler provenance. A handwritten Go carrier is accepted by the plain-Go consumer test; it does not register source-language impl evidence. Adding a provenance rule to `Of` would break that supported library contract. |
| Runtime correctness/behavior of an arbitrary carrier operation. | No debug assertion proposed. | `Of` only supplies a zero-value method receiver; the operation itself is ordinary Go method behavior. Runtime reflection, anchor invocation, or debug-only result checks would change or exceed this helper contract. No debug build switch is established by this path. |

The source-language path is `U::Member()` / `T::Member()` lowered by the compiler to `typeinfo.Of[Receiver]().Member()`. Generic bounds use the canonical package import and `CarrierFor` ([lowering](../internal/protocols/lower.go#L100), [associated-call record](../internal/protocols/ir.go#L81)). Goxide validates protocol declarations and metadata before trusting generated evidence; the ordinary Go call path does not run that validation. It still gets Go's compile-time `CarrierFor` constraints. The negative acceptance case checks that an ABI mismatch names the real `.gox` file and `typeinfo`/`Of`, but it does not pin an exact line/column; this inventory makes no stronger source-position claim.

### Scope and open edge

This review finds no additional executable `typeinfo` helper in the current associated-protocol route beyond `Of`; `CarrierFor` and its anchor are constraint/evidence surface rather than helpers to execute. The issue title says "等" ("such as"), so **the unresolved scope decision** is whether a later #22 phase should inventory unrelated built-in helper families. This Draft keeps them out until one is named; it does not infer a universal framework from this single contract.

No new checks are proposed from the evidence reviewed here. No runtime code is changed. No full test suite was run as part of this documentation-only inventory.
