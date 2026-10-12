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

## Scope for the initial inventory

- Sort each `Of`-related condition into existing type/compiler guarantee, a concrete candidate for an earlier static diagnostic, debug assertion candidate, or intentionally unchecked condition.
- Keep Goxide `.gox` lowering/metadata validation distinct from ordinary Go use of `typeinfo.Of`, which bypasses Goxide checks.
- Record gaps and decisions without adding runtime reflection, duplicate empty-carrier checks, a general debug mode, or new checks in this documentation pass.
