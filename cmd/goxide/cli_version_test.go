package main

import (
	"bytes"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

func TestCLIVersion(t *testing.T) {
	originalVersion := version
	t.Cleanup(func() { version = originalVersion })
	for _, buildVersion := range []string{"dev", "v0.0.1"} {
		version = buildVersion
		var out, stderr bytes.Buffer
		if err := run([]string{"--version"}, &out, &stderr); err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("goxide %s (%s)\n", buildVersion, runtime.Version()); out.String() != want || stderr.Len() != 0 {
			t.Fatalf("stdout %q, stderr %q; want %q", out.String(), stderr.String(), want)
		}
	}
	for _, args := range [][]string{
		{"--version", "input.gox"}, {"--version=false"}, {"--version=bad"},
		{"-version"}, {"version"}, {"build", "--version"},
	} {
		var out, stderr bytes.Buffer
		if err := run(args, &out, &stderr); err == nil || out.Len() != 0 {
			t.Errorf("%q: error %v, stdout %q; want rejection without version output", args, err, out.String())
		}
	}
	var help, stderr bytes.Buffer
	if err := run([]string{"--help"}, &help, &stderr); err != nil || !strings.Contains(help.String(), "--version") {
		t.Fatalf("root help: %v\n%s", err, help.String())
	}
}
