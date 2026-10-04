# serde

Unreleased migration candidate. The root package supplies format-independent Serializer, Deserializer, Visitor, container and error protocols. The `github.com/goxide-lang/serde/json` subpackage retains the Go package name `serdejson` and implements JSON over those protocols.

Run `go test -race ./...` and `go vet ./...` with Go 1.27 or newer. No compiler, generated application or final project manifest is included. This library test does not prove completed compiler migration. No stable release is claimed.
