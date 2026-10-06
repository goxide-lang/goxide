package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestASTOracleControls(t *testing.T) {
	for _, c := range []struct {
		name, before, after string
		equal               bool
	}{
		{"operator", "var n = a+b", "var n = a-b", false},
		{"channel-direction", "var c chan<- int", "var c <-chan int", false},
		{"type-alias", "type T = int", "type T int", false},
		{"ellipsis", "var x = f(a...)", "var x = f(a)", false},
		{"trailing-comma", "var x = f(a)", "var x = f(\n a,\n)", true},
		{"child-order", "var x = f(a,b)", "var x = f(b,a)", false},
		{"comments-and-positions", "var x = f(a)", "// comment\n\nvar x = f( a )", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			paths := []string{filepath.Join(dir, "before.go"), filepath.Join(dir, "after.go")}
			for i, source := range []string{c.before, c.after} {
				if err := os.WriteFile(paths[i], []byte("package p\n"+source+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			code := compare(paths, &stdout, &stderr)
			wantCode := 1
			if c.equal {
				wantCode = 0
			}
			if code != wantCode || stderr.Len() != 0 {
				t.Fatalf("exit=%d want=%d stderr=%s", code, wantCode, stderr.String())
			}
			var result struct{ Identical bool }
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || result.Identical != c.equal {
				t.Fatalf("bad result %s, error=%v", stdout.String(), err)
			}
		})
	}
}

func TestASTOracleMalformedInput(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.go")
	if err := os.WriteFile(bad, []byte("package"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	if code := compare([]string{bad, bad}, &out, &errout); code != 2 || out.Len() != 0 || errout.Len() == 0 {
		t.Fatalf("malformed input: exit=%d stdout=%s stderr=%s", code, &out, &errout)
	}
}
