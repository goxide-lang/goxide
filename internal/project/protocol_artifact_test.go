package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishedSourceRejectsProtocolArtifact(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "model.gox"), []byte("package model\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validatePublished(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".goxide.protocols.json"), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validatePublished(root); err == nil || !strings.Contains(err.Error(), "published goxide source mixes generated artifacts") {
		t.Fatalf("generated protocol artifact must not be accepted as published source: %v", err)
	}
}
