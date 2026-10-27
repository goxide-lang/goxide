package lsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
)

// refreshGo never parses, expands, checks, or lowers source. Maintenance can
// reuse the published immutable source result even when generation reported a
// Go-only error. A changed input cancels the worker and prevents publication.
func (s *Server) refreshGo(ctx context.Context, current *publishedAnalysis, w *workspace, deadline time.Time) {
	workCtx := ctx
	s.mu.Lock()
	backend := s.backend
	if deadline.IsZero() {
		if backend != nil && (backend.retiring || backend.client.RequestCount()+uint64(backend.activeQueries) >= goRequestLimit) {
			deadline = time.Now().Add(goMaintenanceTimeout)
		}
	}
	s.mu.Unlock()
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		workCtx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	previous := current.goProjection
	if previous == nil && backend != nil && !deadline.IsZero() {
		// A changed input may first reach the limit during synchronization.
		// Its new source analysis is also reusable if initialization fails.
		previous = &goProjection{backend: backend}
	}
	current.goProjection, current.backendError = s.syncGo(workCtx, current, w, current.sourceMaps)
	if current.goProjection != nil && current.backendError == nil {
		var diagnostics map[string][]wireDiagnostic
		diagnostics, current.backendError = s.pullGoDiagnostics(workCtx, current)
		if current.backendError == nil {
			current.goDiagnostics = diagnostics
		}
	}
	if current.backendError != nil && current.goProjection == nil {
		// Keep a failed maintenance snapshot only as a retry locator. Its
		// backendError prevents queries from using the retired process. The
		// next Go request may attempt one rebuild without reanalyzing source.
		current.goProjection = previous
	}
	if current.backendError != nil && ctx.Err() == nil {
		message := "Go language service unavailable: " + current.backendError.Error()
		if s.goFailure != message {
			s.goFailure = message
			fmt.Fprintln(s.options.Log, message)
			_ = s.connection.Notify(ctx, "window/logMessage", map[string]any{"type": 2, "message": message})
		}
	}
}

func (s *Server) setWorkspaceCallbacks(w *workspace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.preparing = w == nil
	s.macroSource, s.watchSource = nil, nil
	if w != nil {
		s.macroSource, s.watchSource = w.macroSource, w.watches
		s.watchRoot = w.root
	}
}

// The watcher excludes this configuration's managed artifacts, including a
// custom target directory, before those files can trigger another preparation.
func (s *Server) ignoredWatch(name string) bool {
	root := s.watchRoot
	if root == "" {
		root = s.directory
		if filepath.Ext(root) == ".gox" || filepath.Ext(root) == ".go" || filepath.Base(root) == "goxide.toml" {
			root = filepath.Dir(root)
		}
	}
	target := s.options.Compiler.TargetDir
	if target == "" {
		target = os.Getenv("GOXIDE_TARGET_DIR")
	}
	if target == "" {
		target = "target"
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	if relative, err := filepath.Rel(target, name); err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return true
	}
	for _, part := range strings.Split(filepath.ToSlash(name), "/") {
		if part == ".git" || part == "node_modules" || strings.HasPrefix(part, ".generation-") || strings.HasPrefix(part, ".lsp-view-") {
			return true
		}
	}
	return false
}

func (s *Server) analyzeWorkspace(ctx context.Context, w *workspace, documents DocumentSnapshot, previous *publishedAnalysis, reload bool) (*workspaceSnapshot, *analysis.Result, error) {
	for {
		snapshot, err := w.snapshot(ctx, documents)
		if err != nil {
			return nil, nil, err
		}
		snapshot.input.Options.PreserveSyntax = true
		snapshot.input.Options.Editor = true
		if previous != nil && !reload {
			snapshot.input.Options.Previous = previous.result
		}
		result, err := analysis.Analyze(ctx, snapshot.input)
		cause := err
		if cause == nil && result != nil {
			cause = result.Err
		}
		var demand *compile.NativeRootDemand
		if w.native == nil || !errors.As(cause, &demand) || ctx.Err() != nil {
			return snapshot, result, err
		}
		// Root discovery stops before semantic checking. Release its read lease
		// before preparing the demanded source dependency and retrying that input.
		snapshot.close()
		s.setWorkspaceCallbacks(nil)
		activated, activateErr := w.activate(ctx, cause)
		if activateErr != nil {
			return nil, nil, activateErr
		}
		s.setWorkspaceCallbacks(w)
		if !activated {
			return snapshot, result, cause
		}
	}
}
