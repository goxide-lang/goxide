// Package goxideruntime provides compiler support for generated Go. These names
// are lowering details, not source-language protocols or a stable public ABI.
package goxideruntime

// Comparable preserves the Go constraint's identity when a source package
// declares its own identifier named comparable.
type Comparable interface{ comparable }
