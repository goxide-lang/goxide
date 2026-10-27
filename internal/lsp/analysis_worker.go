package lsp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
)

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
