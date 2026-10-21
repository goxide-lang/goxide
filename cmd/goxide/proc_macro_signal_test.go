package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testCLIInterruptDuringPreparation(t *testing.T, binary, tool string, env []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("os.Interrupt cannot be delivered to a child process on Windows")
	}
	root := t.TempDir()
	ready, release, stopped := filepath.Join(root, "ready"), filepath.Join(root, "release"), filepath.Join(root, "stopped")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	wrapper := filepath.Join(root, "go-wrapper")
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = list ]; then
 case "$PWD" in
  */.generation-*)
   parent=$PPID
   remaining=200
   : > %s
   while [ ! -e %s ] && kill -0 "$parent" 2>/dev/null && [ "$remaining" -gt 0 ]; do
    sleep 0.01
    remaining=$((remaining - 1))
   done
   : > %s
   exit 0
   ;;
 esac
fi
exec %s "$@"
`, quote(ready), quote(release), quote(stopped), quote(tool))
	for name, data := range map[string]string{
		"goxide.toml": "[go_mod]\nmodule=\"example.org/interrupt-preparation\"\ngo=\"1.27\"\n[[bin]]\nname=\"app\"\npath=\".\"\n",
		"main.gox":    "package main\nfunc main() {}\n",
		"go-wrapper":  script,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(data), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	log, err := os.Create(filepath.Join(root, "cli.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "build", "--offline", "--go", wrapper, root)
	command.Dir, command.Env = root, env
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		// Release the Go subprocess even when its parent CLI was interrupted.
		_ = os.WriteFile(release, nil, 0o600)
		_ = command.Process.Kill()
		if _, err := os.Stat(ready); err != nil {
			return
		}
		// Keep the release marker alive until the wrapper has left its loop.
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			if _, err := os.Stat(stopped); err == nil {
				return
			}
			select {
			case <-deadline.C:
				t.Error("paused Go wrapper did not acknowledge cleanup")
				return
			case <-tick.C:
			}
		}
	}()
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-done:
			data, _ := os.ReadFile(log.Name())
			t.Fatalf("CLI stopped before preparation pause: %v\n%s", err, data)
		case <-ctx.Done():
			t.Fatal("CLI did not reach preparation pause")
		case <-tick.C:
		}
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "interrupt") {
			t.Fatalf("CLI did not retain normal interrupt behavior: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CLI swallowed SIGINT outside procedural macro execution")
	}
}
