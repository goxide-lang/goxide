# goxide std — candidate

Core types shared by hgo-generated Go packages. This unreleased candidate is not a v1.0 stability promise. It requires a compiler implementing the matching sum ABI.

`sum` provides Option/Result value cases, constructors and shape checks. Assignment has ordinary Go shallow-copy semantics. Cloning is explicit. A shape check does not validate application-specific payloads; generated callers must retain their registered payload checks.

The format-independent serialization protocol and JSON implementation belong to the separate `github.com/goxide-lang/serde` module, not this module. No experimental API is included.

The existing Mozilla Public License 2.0 text is retained in LICENSE. No release tag is provided by this candidate.
