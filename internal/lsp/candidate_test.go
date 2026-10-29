package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func writeCandidateRuntime(t *testing.T, root string) {
	t.Helper()
	setCandidateSDKIdentity(t)
	if err := testdeps.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.org/candidate-action\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
}

func setCandidateSDKIdentity(t *testing.T) {
	t.Helper()
	identity, err := procmacro.ReadSDKIdentity(t.Context(), filepath.Join(runtime.GOROOT(), "bin", "go"), ".", project.GoEnv(true))
	if err != nil {
		t.Fatal(err)
	}
	previous := procmacro.CompilerSDKIdentity
	procmacro.CompilerSDKIdentity = identity
	t.Cleanup(func() { procmacro.CompilerSDKIdentity = previous })
}

func candidateFixture(t *testing.T) (*Server, *publishedAnalysis, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageFile(t, root, "go.mod", "module example.org/candidate\ngo 1.27\n")
	writeLanguageFile(t, root, "main.gox", "package main\nfunc main(){println(answer())}\n")
	writeLanguageFile(t, root, "lib.gox", "package main\n// 😀\nfunc answer()int{return 41}\n")
	server, peer := startBudgetServer(t, root, "off")
	openLanguageDocument(t, peer, filepath.Join(root, "main.gox"), "package main\nfunc main(){println(answer())}\n")
	current, err := server.awaitComplete(t.Context())
	if err != nil || current.result == nil || current.result.Err != nil || current.generateError != nil {
		t.Fatalf("candidate fixture did not complete: %+v, %v", current, err)
	}
	return server, current, root
}

func candidateEdit(t *testing.T, current *publishedAnalysis, suffix, old, replacement string) analysis.SourceEdit {
	t.Helper()
	for key, index := range current.indexes {
		if strings.HasSuffix(key, "/"+suffix) {
			start := strings.Index(index.Text(), old)
			if start < 0 {
				t.Fatalf("candidate token %q absent from %s", old, key)
			}
			return analysis.SourceEdit{File: key, Span: syntaxSpan(start, start+len(old)), NewText: replacement}
		}
	}
	t.Fatalf("candidate file %s is absent", suffix)
	return analysis.SourceEdit{}
}

func TestCandidateUsesPrivateClosedFileOverlay(t *testing.T) {
	server, current, root := candidateFixture(t)
	edit := candidateEdit(t, current, "lib.gox", "41", "42")
	called := false
	err := server.validateCandidate(t.Context(), current, []analysis.SourceEdit{edit}, true, func(ctx context.Context, result *analysis.Result) error {
		called = true
		if result == current.result || string(result.Sources[edit.File].Content) != strings.Replace(current.indexes[edit.File].Text(), "41", "42", 1) {
			t.Error("candidate did not use independent edited source")
		}
		return ctx.Err()
	})
	if err != nil || !called {
		t.Fatalf("candidate validation: called=%v err=%v", called, err)
	}
	server.mu.Lock()
	unchanged := server.current == current && server.epoch == current.epoch
	server.mu.Unlock()
	if !unchanged || current.indexes[edit.File].Text() != "package main\n// 😀\nfunc answer()int{return 41}\n" {
		t.Fatal("candidate changed the published generation")
	}
	uri, _ := PathToURI(filepath.Join(root, "lib.gox"))
	if _, open := server.documents.Get(uri); open {
		t.Fatal("candidate opened a closed document in the live store")
	}
	data, err := os.ReadFile(filepath.Join(root, "lib.gox"))
	if err != nil || string(data) != current.indexes[edit.File].Text() {
		t.Fatalf("candidate changed authored disk: %s, %v", data, err)
	}
}

func TestCandidateRejectsSourceAndGeneratedFailures(t *testing.T) {
	for _, test := range []struct{ name, old, replacement, diagnostic string }{
		{"source", "41", "unknown", "unresolved name"},
		{"generated", "return 41", "", "missing return"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, current, _ := candidateFixture(t)
			edit := candidateEdit(t, current, "lib.gox", test.old, test.replacement)
			called := false
			err := server.validateCandidate(t.Context(), current, []analysis.SourceEdit{edit}, true, func(context.Context, *analysis.Result) error { called = true; return nil })
			if err == nil || !strings.Contains(err.Error(), test.diagnostic) || called {
				t.Fatalf("invalid candidate: callback=%v error=%v", called, err)
			}
		})
	}
}

func TestCandidatePartialRequiresValidationPolicy(t *testing.T) {
	server, current, _ := candidateFixture(t)
	edit := candidateEdit(t, current, "lib.gox", "41", "unknown")
	if err := server.validateCandidate(t.Context(), current, []analysis.SourceEdit{edit}, false, nil); err == nil {
		t.Fatal("partial candidate accepted without a policy")
	}
	policyErr := errors.New("unexpected candidate diagnostic")
	err := server.validateCandidate(t.Context(), current, []analysis.SourceEdit{edit}, false, func(_ context.Context, result *analysis.Result) error {
		if result.Err == nil || len(result.Diagnostics) == 0 {
			t.Fatal("partial candidate lost source diagnostics")
		}
		return policyErr
	})
	if !errors.Is(err, policyErr) {
		t.Fatalf("partial candidate ignored policy: %v", err)
	}
}

func TestCandidateActionUsesCurrentBindings(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text := "package main\r\ntrait Read {func read(self &Self, value int) int}\r\ntype Value struct{}\r\nfunc main(){_=\"😀\"}; impl Read for Value{}\r\n"
	name := filepath.Join(root, "main.gox")
	writeLanguageFile(t, root, "main.gox", text)
	writeCandidateRuntime(t, root)
	server, peer := startBudgetServer(t, name, "off")
	openLanguageDocument(t, peer, name, text)
	current, err := server.awaitComplete(t.Context())
	if err != nil || current.queries == nil {
		t.Fatalf("candidate source unavailable: %v; diagnostics=%v; log=%s", err, peer.notifications, peer.log)
	}
	key := current.keys[name]
	position := strings.Index(text, "impl Read")
	actions, err := current.queries.CodeActions(key, syntaxSpan(position, position))
	if err != nil || len(actions) != 1 {
		t.Fatalf("actions: %v, %v", actions, err)
	}
	for _, action := range actions {
		err := server.validateCandidate(t.Context(), current, action.Edits, false, func(_ context.Context, result *analysis.Result) error {
			return current.queries.ValidateCodeAction(result, action)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCandidateUsesFreshMacroSession(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	text := "package main\nuse std;\n#[derive(std::derive::Clone)]\ntype Value struct{N int}\nfunc main(){println(41)}\n"
	name := filepath.Join(root, "main.gox")
	writeLanguageFile(t, root, "main.gox", text)
	setCandidateSDKIdentity(t)
	writeLanguageFile(t, root, "goxide.toml", "[go_mod]\nmodule='example.org/candidate-macro'\ngo='1.27'\n[[bin]]\nname='app'\npath='.'\n"+testdeps.FormalDependencies(t))
	server, peer := startBudgetServer(t, root, "off")
	openLanguageDocument(t, peer, name, text)
	current, err := server.awaitComplete(t.Context())
	if err != nil || current.result == nil || current.result.Err != nil || current.generateError != nil {
		uri, _ := PathToURI(name)
		peer.call(t, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": uri}})
		var sourceErr error
		if current.result != nil {
			sourceErr = current.result.Err
		}
		t.Fatalf("macro fixture did not complete: analysis=%v generated=%v request=%v; diagnostics=%v", sourceErr, current.generateError, err, peer.notifications)
	}
	edit := candidateEdit(t, current, "main.gox", "41", "42")
	// The completed live analysis has already closed its macro session. Each
	// validation must expand this same procedural derive in a fresh session.
	for range 2 {
		if err := server.validateCandidate(t.Context(), current, []analysis.SourceEdit{edit}, true, func(_ context.Context, candidate *analysis.Result) error {
			return current.queries.ValidateBindings(candidate, []analysis.SourceEdit{edit})
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCandidateCancellationReleasesWorker(t *testing.T) {
	for _, invalidate := range []bool{false, true} {
		t.Run(map[bool]string{false: "request", true: "generation"}[invalidate], func(t *testing.T) {
			server, current, _ := candidateFixture(t)
			edit := candidateEdit(t, current, "lib.gox", "41", "42")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := make(chan struct{})
			released := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- server.validateCandidate(ctx, current, []analysis.SourceEdit{edit}, true, func(ctx context.Context, _ *analysis.Result) error {
					close(started)
					<-ctx.Done()
					close(released)
					return ctx.Err()
				})
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("candidate callback did not start")
			}
			// A canceled queued request must not wait for the active validation.
			queued, abort := context.WithCancel(t.Context())
			abort()
			if err := server.validateCandidate(queued, current, []analysis.SourceEdit{edit}, true, nil); !errors.Is(err, context.Canceled) {
				t.Fatalf("queued cancellation: %v", err)
			}
			if invalidate {
				server.mu.Lock()
				server.invalidateLocked(server.sequence+1, false)
				server.mu.Unlock()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if invalidate && !isStaleEdit(err) || !invalidate && !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled candidate: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("candidate request did not cancel")
			}
			select {
			case <-released:
			case <-time.After(5 * time.Second):
				t.Fatal("candidate retained its workspace lease")
			}
			current, err := server.awaitComplete(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			edit = candidateEdit(t, current, "lib.gox", "41", "42")
			if err := server.validateCandidate(t.Context(), current, []analysis.SourceEdit{edit}, true, nil); err != nil {
				t.Fatalf("worker did not resume after canceled validation: %v", err)
			}
		})
	}
}
