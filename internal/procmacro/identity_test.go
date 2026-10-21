package procmacro_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/procmacro"
)

func TestSDKIdentityUsesProtocolContent(t *testing.T) {
	consumer, sdk := sdkIdentityFixture(t)
	initial := readSDKIdentity(t, consumer)
	writeIdentityFile(t, sdk, "README.md", "unrelated documentation\n")
	writeIdentityFile(t, sdk, "proc_macro/protocol_test.go", "package proc_macro\nconst OnlyTest = true\n")
	writeIdentityFile(t, consumer, "go.mod", sdkConsumerMod("v0.2.0"))
	if got := readSDKIdentity(t, consumer); got != initial {
		t.Fatalf("version, documentation, or test changed SDK identity: %s != %s", got, initial)
	}
	for _, name := range []string{"token/token.go", "proc_macro/protocol.go", "internal/lex/lexer.go"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(sdk, name)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			writeIdentityFile(t, sdk, name, string(before)+"\nconst ChangedProtocol = true\n")
			if got := readSDKIdentity(t, consumer); got == initial {
				t.Fatalf("changing %s did not change SDK identity", name)
			}
			writeIdentityFile(t, sdk, name, string(before))
		})
	}
	writeIdentityFile(t, sdk, "token/extra.go", "package token\nconst ExtraToken = true\n")
	if got := readSDKIdentity(t, consumer); got == initial {
		t.Fatal("adding protocol source did not change SDK identity")
	}
}

func TestSDKIdentityRejectsSourceSymlink(t *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			consumer, sdk := sdkIdentityFixture(t)
			link := filepath.Join(sdk, "token", "linked.go")
			target := filepath.Join(t.TempDir(), "linked.go")
			writeIdentityFile(t, filepath.Dir(target), "linked.go", "package token\n")
			if kind == "directory" {
				link = filepath.Join(sdk, "token")
				target = filepath.Join(t.TempDir(), "token")
				if err := os.Rename(link, target); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, link); err != nil {
				if runtime.GOOS == "windows" {
					t.Skipf("symlinks unavailable: %v", err)
				}
				t.Fatal(err)
			}
			_, err := procmacro.ReadSDKIdentity(context.Background(), identityGoTool(), consumer, identityGoEnv())
			if err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("source symlink error = %v", err)
			}
		})
	}
}

func TestSDKIdentityRejectsPackageModuleOverride(t *testing.T) {
	consumer, sdk := sdkIdentityFixture(t)
	token := filepath.Join(filepath.Dir(sdk), "token")
	if err := os.Rename(filepath.Join(sdk, "token"), token); err != nil {
		t.Fatal(err)
	}
	writeIdentityFile(t, token, "go.mod", "module github.com/goxide-lang/syntax/token\n\ngo 1.27\n")
	writeIdentityFile(t, consumer, "go.mod", sdkConsumerMod("v0.1.0")+"\nrequire github.com/goxide-lang/syntax/token v0.1.0\nreplace github.com/goxide-lang/syntax/token => ../token\n")
	_, err := procmacro.ReadSDKIdentity(context.Background(), identityGoTool(), consumer, identityGoEnv())
	if err == nil || !strings.Contains(err.Error(), "must be supplied by module "+procmacro.SDKModule) {
		t.Fatalf("overridden SDK package error = %v", err)
	}
}

func TestValidateSDKIdentityUsesCompilerStamp(t *testing.T) {
	previous := procmacro.CompilerSDKIdentity
	t.Cleanup(func() { procmacro.CompilerSDKIdentity = previous })
	procmacro.CompilerSDKIdentity = ""
	if err := procmacro.ValidateSDKIdentity("selected"); err == nil || !strings.Contains(err.Error(), "scripts/build.go") {
		t.Fatalf("unstamped compiler error = %v", err)
	}
	procmacro.CompilerSDKIdentity = "linked"
	if err := procmacro.ValidateSDKIdentity("selected"); err == nil || !strings.Contains(err.Error(), "content mismatch") {
		t.Fatalf("different SDK content error = %v", err)
	}
	if err := procmacro.ValidateSDKIdentity("linked"); err != nil {
		t.Fatal(err)
	}
}

func sdkIdentityFixture(t *testing.T) (consumer, sdk string) {
	t.Helper()
	root := t.TempDir()
	consumer, sdk = filepath.Join(root, "consumer"), filepath.Join(root, "sdk")
	writeIdentityFile(t, consumer, "go.mod", sdkConsumerMod("v0.1.0"))
	writeIdentityFile(t, sdk, "go.mod", "module github.com/goxide-lang/syntax\n\ngo 1.27\n")
	writeIdentityFile(t, sdk, "proc_macro/protocol.go", `package proc_macro
import "github.com/goxide-lang/syntax/token"
type Output struct { Tokens token.Stream }
`)
	writeIdentityFile(t, sdk, "token/token.go", `package token
import "github.com/goxide-lang/syntax/internal/lex"
type Stream []int
const Vocabulary = lex.Vocabulary
`)
	writeIdentityFile(t, sdk, "internal/lex/lexer.go", "package lex\nconst Vocabulary = 1\n")
	return consumer, sdk
}

func sdkConsumerMod(version string) string {
	return "module example.org/consumer\n\ngo 1.27\n\nrequire github.com/goxide-lang/syntax " + version + "\nreplace github.com/goxide-lang/syntax => ../sdk\n"
}

func writeIdentityFile(t *testing.T, root, name, contents string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readSDKIdentity(t *testing.T, consumer string) string {
	t.Helper()
	identity, err := procmacro.ReadSDKIdentity(context.Background(), identityGoTool(), consumer, identityGoEnv())
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func identityGoTool() string { return filepath.Join(runtime.GOROOT(), "bin", "go") }

func identityGoEnv() []string {
	return append(os.Environ(), "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS=", "GOPROXY=off", "GOSUMDB=off")
}
