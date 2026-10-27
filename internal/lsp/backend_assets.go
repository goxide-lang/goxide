package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/goxide-lang/goxide/internal/compile"
)

type singleFileAssets struct {
	directives string
	files      map[string]bool
	err        error
}

// Function body edits keep the same asset inventory. Configuration and resource
// events replace workspace, and rebuilding its private Go view clears this cache.
func (s *Server) syncSingleFileAssets(ctx context.Context, current *publishedAnalysis, w *workspace, projection *goProjection) error {
	if w.native == nil {
		return nil
	}
	name, module, _ := w.native.SingleFile()
	if name == "" {
		return nil
	}
	var uri, text string
	for key, original := range current.names {
		if original == name {
			uri = projection.uris[key]
			text = projection.files[uri]
			break
		}
	}
	if uri == "" {
		return nil
	}
	directives := embedDirectives(text)
	if w.assets != nil && w.assets.directives == directives {
		return w.assets.err
	}
	target, err := URIToPath(uri)
	if err != nil {
		return err
	}
	wanted := map[string]bool{}
	var resources []string
	if directives != "" {
		temporary, err := os.MkdirTemp(s.goView, ".embed-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temporary)
		input := filepath.Join(temporary, "selected.go")
		if err := os.WriteFile(input, []byte(text), 0600); err != nil {
			return err
		}
		// A named-file Go query resolves assets in the author's package without
		// selecting its siblings. The virtual name is never written there.
		virtual := filepath.Join(filepath.Dir(name), "goxide_embed_"+filepath.Base(temporary)+".go")
		if _, err := os.Lstat(virtual); err == nil || !os.IsNotExist(err) {
			return fmt.Errorf("single-file resource query path is already occupied: %s", virtual)
		}
		overlay, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{virtual: input}})
		if err != nil {
			return err
		}
		overlayPath := filepath.Join(temporary, "overlay.json")
		if err := os.WriteFile(overlayPath, overlay, 0600); err != nil {
			return err
		}
		cfg := w.native.Config()
		command := exec.CommandContext(ctx, cfg.Environment.GoTool, "list", "-e", "-json", "-mod=readonly", "-buildvcs=false", "-modfile="+w.native.GoModFile(), "-overlay="+overlayPath, "-tags="+strings.Join(s.options.Compiler.Tags, ","), virtual)
		command.Dir = filepath.Dir(name)
		if module == "" {
			command.Dir = filepath.Join(s.goView, "module")
		}
		command.Env = compile.NativeEnvironment(cfg.Environment.ExecEnv())
		command.WaitDelay = time.Second
		data, err := command.Output()
		if err != nil {
			if failure, ok := err.(*exec.ExitError); ok {
				return fmt.Errorf("single-file resource inventory: %w: %s", err, bytes.TrimSpace(failure.Stderr))
			}
			return err
		}
		var metadata struct{ EmbedFiles []string }
		if err := json.Unmarshal(data, &metadata); err != nil {
			return fmt.Errorf("single-file resource inventory: %w", err)
		}
		resources = metadata.EmbedFiles
	}
	for _, relative := range resources {
		if !filepath.IsLocal(relative) {
			return fmt.Errorf("single-file resource is outside its package: %s", relative)
		}
		destination := filepath.Join(filepath.Dir(target), relative)
		if destination == target {
			continue // The selected Go source already has its own current overlay.
		}
		if nativeResourceSource(relative) {
			err := fmt.Errorf("single-file Go service cannot project embedded native source %s without adding it to the package; compiler diagnostics and Goxide source queries remain available", relative)
			w.assets = &singleFileAssets{directives: directives, files: priorAssetFiles(w), err: err}
			return err
		}
		if destination == filepath.Join(s.goView, "module", "go.mod") || destination == filepath.Join(s.goView, "module", "go.sum") {
			err := fmt.Errorf("single-file Go service cannot replace its private module configuration with embedded resource %s", relative)
			w.assets = &singleFileAssets{directives: directives, files: priorAssetFiles(w), err: err}
			return err
		}
		wanted[destination] = true
	}
	for _, relative := range resources {
		destination := filepath.Join(filepath.Dir(target), relative)
		if !wanted[destination] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(name), relative))
		if err != nil {
			return err
		}
		if current, err := os.ReadFile(destination); err == nil && bytes.Equal(current, data) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(destination, data, 0644); err != nil {
			return err
		}
	}
	for name := range priorAssetFiles(w) {
		if wanted[name] {
			continue
		}
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			return err
		}
		for directory := filepath.Dir(name); directory != filepath.Dir(target); directory = filepath.Dir(directory) {
			entries, err := os.ReadDir(directory)
			if err != nil || len(entries) != 0 {
				break
			}
			if err := os.Remove(directory); err != nil {
				return err
			}
		}
	}
	w.assets = &singleFileAssets{directives: directives, files: wanted}
	return nil
}

func priorAssetFiles(w *workspace) map[string]bool {
	if w.assets != nil {
		return w.assets.files
	}
	return nil
}

func embedDirectives(text string) string {
	set := token.NewFileSet()
	file := set.AddFile("", -1, len(text))
	var scan scanner.Scanner
	scan.Init(file, []byte(text), nil, scanner.ScanComments)
	var directives []string
	for {
		_, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		if kind == token.COMMENT && strings.HasPrefix(literal, "//go:embed") {
			directives = append(directives, literal)
		}
	}
	return strings.Join(directives, "\n")
}

// These extensions can become extra compiler inputs in an ordinary Go package.
// Conservatively reject them as resources instead of altering their bytes or
// pretending that a package view implements Go's named-file selection.
func nativeResourceSource(name string) bool {
	base := filepath.Base(name)
	if strings.HasPrefix(base, ".") || strings.HasPrefix(base, "_") {
		return false
	}
	switch filepath.Ext(name) {
	case ".go", ".c", ".cc", ".cpp", ".cxx", ".m", ".h", ".hh", ".hpp", ".hxx", ".f", ".F", ".for", ".f90", ".s", ".S", ".swig", ".swigcxx", ".syso":
		return true
	}
	return false
}
