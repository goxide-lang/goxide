package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/procmacro"
	"github.com/goxide-lang/goxide/internal/project"
)

// workspace owns one selected compiler configuration. Only the analysis worker
// prepares, replaces, or closes it; requests read published analysis facts.
type workspace struct {
	owned        map[string]string
	ownedLoaded  bool
	project      *compile.Project
	native       *compile.NativeProject
	host         *procmacro.WASIHost
	root, goRoot string
}

type workspaceSnapshot struct {
	input analysis.Input
	names map[string]string
	close func()
}

func prepareWorkspace(ctx context.Context, input string, options compile.Options) (*workspace, error) {
	options.Editor = true
	info, err := os.Stat(input)
	if err != nil {
		return nil, err
	}
	directory := input
	if !info.IsDir() {
		directory = filepath.Dir(input)
	}
	root, err := project.DiscoverProject(directory)
	if err != nil {
		return nil, err
	}
	host, err := procmacro.NewWASIHost(context.WithoutCancel(ctx))
	if err != nil {
		return nil, err
	}
	session, err := host.NewSession(ctx)
	if err != nil {
		host.Close()
		return nil, err
	}
	defer session.Close()
	options.RunProcMacro = session.Run
	w := &workspace{host: host}
	if root != "" {
		// A library-only project has a single unambiguous editor target. Other
		// selections use the same explicit target flags as build/run/test.
		if options.Bin == "" && options.Example == "" && !options.Lib {
			manifest, err := project.Read(root)
			if err != nil {
				host.Close()
				return nil, err
			}
			options.Lib = manifest.Lib != nil && len(manifest.Bins) == 0 && len(manifest.Examples) == 0
		}
		w.project, err = compile.Prepare(ctx, directory, options)
		if err == nil {
			w.root, w.goRoot = root, w.project.GoRoot()
		}
	} else {
		w.native, err = compile.PrepareNative(ctx, input, compile.NativeOptions{Options: options, Automatic: true})
		if err == nil {
			w.root, w.goRoot = w.native.Root(), w.native.GoRoot()
		}
	}
	if err != nil {
		host.Close()
		return nil, err
	}
	return w, nil
}

func (w *workspace) ownsSource(name string) bool {
	if w.native != nil {
		return w.native.OwnsSource(name)
	}
	return w.project.OwnsSource(name)
}
func (w *workspace) macroSource(name string) bool {
	if w.native != nil {
		return w.native.MacroSource(name)
	}
	return w.project.MacroSource(name)
}

func (w *workspace) watches(name string) bool {
	// Configuration changes can alter selection itself (including a newly
	// created nested module), so they must not use the old source-file filter.
	switch filepath.Base(name) {
	case "go.mod", "go.sum", "goxide.toml", "goxide.lock", "goxide.roots.toml":
		return true
	}
	if strings.HasSuffix(name, "_gox.go") && w.ownsSource(strings.TrimSuffix(name, "_gox.go")+".gox") {
		// Owned outputs are excluded from source input, but newly created and
		// changed outputs must refresh the backend's disk masking inventory.
		return true
	}
	if filepath.Ext(name) == ".gox" || filepath.Ext(name) == ".go" {
		return w.ownsSource(name)
	}
	relative, err := filepath.Rel(w.root, name)
	return err == nil && (relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func (w *workspace) snapshot(ctx context.Context, documents DocumentSnapshot) (*workspaceSnapshot, error) {
	overlays := map[string][]byte{}
	for uri, document := range documents.Documents {
		name, err := URIToPath(uri)
		if err != nil {
			return nil, err
		}
		if w.ownsSource(name) && (filepath.Ext(name) == ".gox" || filepath.Ext(name) == ".go") {
			overlays[name] = []byte(document.Text)
		}
	}
	var snapshot *compile.Snapshot
	var err error
	if w.native != nil {
		snapshot, err = w.native.Snapshot(ctx, overlays)
	} else {
		snapshot, err = w.project.Snapshot(ctx, overlays)
	}
	if err != nil {
		return nil, err
	}
	session, err := w.host.NewSession(snapshot.Context)
	if err != nil {
		snapshot.Close()
		return nil, err
	}
	snapshot.BindProcMacros(session.Run)
	return &workspaceSnapshot{input: snapshot.Input, names: snapshot.SourceNames, close: func() { session.Close(); snapshot.Close() }}, nil
}

func (w *workspace) activate(ctx context.Context, cause error) (bool, error) {
	if w.native == nil {
		return false, nil
	}
	session, err := w.host.NewSession(ctx)
	if err != nil {
		return false, err
	}
	defer session.Close()
	// Dependency preparation uses its own current-session runner. NativeProject
	// keeps its construction options; binding is refreshed before activation.
	activated, err := w.native.Activate(ctx, cause, session.Run)
	if activated && err == nil {
		w.goRoot = w.native.GoRoot()
	}
	return activated, err
}

func (w *workspace) close() {
	if w == nil {
		return
	}
	if w.project != nil {
		_ = w.project.Close()
	}
	if w.native != nil {
		_ = w.native.Close()
	}
	if w.host != nil {
		_ = w.host.Close()
	}
}
