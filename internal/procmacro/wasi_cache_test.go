//go:build !js

package procmacro

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/goxide-lang/syntax/token"
)

func TestWASICacheAcrossHostsAndProcesses(t *testing.T) {
	request, wasm, want := wasiSessionResponse(t)
	run := func(wasm, want []byte) {
		t.Helper()
		host := wasiDiskCacheHost(t)
		if host.diskCache == nil {
			t.Fatal("host did not enable the private disk cache")
		}
		// The fixture traps if an invocation reuses its mutable global.
		for range 2 {
			got, err := host.Run(wasm, request)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("cached module changed fresh-instance output: %v\n%s", err, got)
			}
		}
		if stats := host.Stats(); stats.Invocations != 2 || stats.MemoryHits != 1 || stats.ModuleLoads != 1 || !stats.DiskCache || stats.CacheFallback {
			t.Fatalf("cached invocation statistics = %+v", stats)
		}
		if err := host.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("GOXIDE_TEST_WASI_CACHE_CHILD") == "1" {
		run(wasm, want)
		return
	}
	directory := t.TempDir()
	t.Setenv("GOXIDE_WASM_CACHE", directory)
	run(wasm, want)
	files := wasiCacheSnapshot(t, directory)
	if len(files) == 0 {
		t.Fatal("cold invocation did not persist compiled code")
	}
	// Mark existing entries so even a byte-identical rewrite is observable.
	stamp := time.Unix(946684800, 0)
	for name := range files {
		if err := os.Chtimes(name, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	before := wasiCacheSnapshot(t, directory)
	run(wasm, want)
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), binary, "-test.run=^TestWASICacheAcrossHostsAndProcesses$")
	command.Env = append(os.Environ(), "GOXIDE_TEST_WASI_CACHE_CHILD=1")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("cached subprocess: %v\n%s", err, output)
	}
	if after := wasiCacheSnapshot(t, directory); !reflect.DeepEqual(before, after) {
		t.Fatal("new hosts or processes rewrote an already compiled artifact")
	}
	changed, err := token.ParseText("43", request.Context().CallSite())
	if err != nil {
		t.Fatal(err)
	}
	var response bytes.Buffer
	if err := pm.WriteResponse(&response, request, pm.Success(changed)); err != nil {
		t.Fatal(err)
	}
	run(wasiResponseModule(response.Bytes()), response.Bytes())
	after := wasiCacheSnapshot(t, directory)
	if len(after) <= len(before) {
		t.Fatal("changed WASM did not create a distinct compiled artifact")
	}
	for name, value := range before {
		if !reflect.DeepEqual(value, after[name]) {
			t.Fatal("changed WASM replaced another module's cache entry")
		}
	}
}

func TestWASICacheUnavailableFallsBack(t *testing.T) {
	for _, failure := range []string{"root_is_file", "identity_is_file", "truncated_entry"} {
		t.Run(failure, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "cache")
			t.Setenv("GOXIDE_WASM_CACHE", directory)
			request, wasm, want := wasiSessionResponse(t)
			switch failure {
			case "root_is_file":
				if err := os.WriteFile(directory, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "identity_is_file":
				host := wasiDiskCacheHost(t)
				if host.diskCache == nil || host.cacheDir == "" {
					t.Fatal("host did not create an identity-scoped cache")
				}
				identity := host.cacheDir
				if err := host.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.RemoveAll(identity); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(identity, nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "truncated_entry":
				host := wasiDiskCacheHost(t)
				if _, err := host.Run(wasm, request); err != nil {
					t.Fatal(err)
				}
				if err := host.Close(); err != nil {
					t.Fatal(err)
				}
				files := wasiCacheSnapshot(t, directory)
				if len(files) == 0 {
					t.Fatal("no compiled artifact to truncate")
				}
				for name := range files {
					if err := os.WriteFile(name, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			host := wasiDiskCacheHost(t)
			got, err := host.Run(wasm, request)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("unavailable disk cache changed macro output: %v\n%s", err, got)
			}
			if host.diskCache != nil {
				t.Fatal("failed disk cache remained active")
			}
			loads := 1
			if failure == "truncated_entry" {
				loads = 2
			}
			if stats := host.Stats(); stats.Invocations != 1 || stats.ModuleLoads != loads || stats.DiskCache || !stats.CacheFallback {
				t.Fatalf("fallback invocation statistics = %+v", stats)
			}
		})
	}
}

func wasiDiskCacheHost(t *testing.T) *WASIHost {
	t.Helper()
	host, err := NewWASIHostWithBudgets(t.Context(), MaxInvocationTime*wasiTestBudgetMultiplier, MaxCompilationTime*wasiTestBudgetMultiplier)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
	})
	return host
}

type wasiCacheFile struct {
	data    string
	modTime int64
}

func wasiCacheSnapshot(t *testing.T, directory string) map[string]wasiCacheFile {
	t.Helper()
	files := map[string]wasiCacheFile{}
	err := filepath.WalkDir(directory, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		files[name] = wasiCacheFile{string(data), info.ModTime().UnixNano()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
