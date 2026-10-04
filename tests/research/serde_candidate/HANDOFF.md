# Isolated serde candidate handoff

Import only the files explicitly listed in provenance.json into the separate serde repository. Exclude this handoff, provenance.json, compiler files and compiler history. Verify SHA256 first. The root is module github.com/goxide-lang/serde; json/ retains Go package name serdejson. Source changes are directory relocation and core import-path rewriting only; the MPL 2.0 LICENSE is byte-identical to the existing repository file. No hgo.json or project manifest is included.

The candidate passed library race/vet in isolation. Compiler imports, metadata identity, fixtures, CLI and browser exports still require migration and integration acceptance. This handoff is not production runtime vendoring or a released dependency version. Do not publish the private provenance or compiler repository history.
