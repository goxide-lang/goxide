package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScriptTrustIdentityMonorepo(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "apps", "app")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	git := filepath.Join(repo, ".git")
	if err := os.Mkdir(git, 0755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(git, "config")
	if err := os.WriteFile(config, []byte("[remote origin]\nurl=first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := projectTrustIdentity(root, "example.org/app")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte("new-branch"), 0600); err != nil {
		t.Fatal(err)
	}
	same, err := projectTrustIdentity(root, "example.org/app")
	if err != nil || same != first {
		t.Fatalf("HEAD changed project trust: %s %v", same, err)
	}
	if err := os.WriteFile(config, []byte("[remote origin]\nurl=second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := projectTrustIdentity(root, "example.org/app")
	if err != nil || changed == first {
		t.Fatalf("repository identity not bound: %s %v", changed, err)
	}
}
