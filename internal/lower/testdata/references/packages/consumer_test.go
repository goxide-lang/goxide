package main

import (
	"testing"

	"referencefixture/kit"
)

func TestNativeGoUsesOrdinaryPointers(t *testing.T) {
	store := kit.Store{Value: 1}
	view := kit.Read(&store)
	*view = 8
	if store.Value != 8 || kit.Edit(&store) != view {
		t.Fatalf("generated API changed pointer identity: %#v", store)
	}
}
