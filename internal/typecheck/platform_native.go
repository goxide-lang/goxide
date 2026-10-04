//go:build !js || !wasm

package typecheck

import "go/types"

func platformImporter() types.Importer { return nil }
