# Isolated std candidate handoff

This directory transfers only the five reviewed library files listed in provenance.json between execution environments. It is not compiler runtime vendoring and must not become a production replace target.

Import exactly LICENSE, README.md, go.mod, sum/sum.go and sum/sum_test.go into the separate std repository after verifying each recorded SHA256. Do not publish this handoff document, provenance.json, compiler files or compiler history. The retained LICENSE is byte-identical to the existing MPL 2.0 text. The local candidate root commit was 6be6fc5d4240d1b108c265ce1fb877f12c09b532; the destination repository has its own history.

Validation already performed: candidate race/vet, separate Go producer/consumer identity checks, js/wasm library build, and independent review. This is not evidence of completed production hgo migration.
