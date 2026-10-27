package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lsp/gopls"
	"github.com/goxide-lang/goxide/internal/source"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

type goBackend struct {
	client       *gopls.Client
	coordinator  *gopls.Coordinator
	root         string
	preparedRoot string
	// Guarded by Server.mu. Budget maintenance stops new admissions and waits
	// for these already admitted queries before shutting down the process.
	activeQueries int
	idle          chan struct{}
	retiring      bool
}

const (
	goRequestLimit       = 2048
	goMaintenanceTimeout = 5 * time.Second
)

type goProjection struct {
	indexes map[string]*source.Index
	backend *goBackend
	files   map[string]string
	keys    map[string]string // Generated/native Go URI to source key.
	uris    map[string]string // Source key to generated/native Go URI.
	maps    map[string]*source.Map
}

func (b *goBackend) close() {
	b.closeContext(context.Background())
}

func (b *goBackend) closeContext(ctx context.Context) {
	if b == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_ = b.client.Close(ctx)
}

// acquireGo preserves the source epoch while a budget-exhausted backend is
// replaced. A completed diagnostics batch and the eight request workers may
// cross the limit; shutdown adds one final RPC. No new query is admitted to a
// retiring process, and a query that already succeeded is not retried.
func (s *Server) acquireGo(ctx context.Context, original *publishedAnalysis) (*publishedAnalysis, func(), error) {
	waiting := false
	attempted := false
	var originalBackend *goBackend
	if original.goProjection != nil {
		originalBackend = original.goProjection.backend
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		current, err := s.awaitComplete(ctx)
		if err != nil {
			return nil, nil, err
		}
		s.mu.Lock()
		if current.epoch != original.epoch || s.epoch != original.epoch || s.shutdown {
			s.mu.Unlock()
			return nil, nil, staleResult()
		}
		if current.goProjection == nil || current.backendError != nil && attempted && s.backendMaintenance == nil {
			s.mu.Unlock()
			return current, func() {}, current.backendError
		}
		backend := current.goProjection.backend
		// A fresh process's initial diagnostics can themselves be larger than
		// the limit. Each waiting query still gets one admission after rebuild;
		// otherwise a workspace with many open files could rebuild forever.
		if s.backendMaintenance == nil && (current.backendError != nil || backend == originalBackend && backend.client.RequestCount() >= goRequestLimit) {
			s.backendMaintenance = backend
			backend.retiring = true
			s.maintenanceDeadline = time.Now().Add(goMaintenanceTimeout)
			select {
			case s.work <- struct{}{}:
			default:
			}
			s.signalLocked()
		}
		if s.backendMaintenance == nil {
			if backend.activeQueries == 0 {
				backend.idle = make(chan struct{})
			}
			backend.activeQueries++
			s.mu.Unlock()
			return current, func() {
				s.mu.Lock()
				backend.activeQueries--
				if backend.activeQueries == 0 {
					close(backend.idle)
				}
				s.mu.Unlock()
			}, nil
		}
		attempted = true
		if !waiting {
			waitCtx, cancel := context.WithDeadline(ctx, s.maintenanceDeadline)
			defer cancel()
			ctx, waiting = waitCtx, true
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, nil, ctx.Err()
		case <-s.ctx.Done():
			return nil, nil, s.ctx.Err()
		case <-changed:
		}
	}
}

func (s *Server) retireGo(ctx context.Context, backend *goBackend) error {
	// The single worker owns backend replacement. Query admission is already
	// stopped by maintenance or by the changed input epoch.
	s.mu.Lock()
	backend.retiring = true
	idle, active := backend.idle, backend.activeQueries
	s.mu.Unlock()
	if active != 0 {
		select {
		case <-idle:
		case <-ctx.Done():
			// An admitted RPC may itself be stuck. Expiring maintenance must
			// still kill its process, rather than retaining it indefinitely.
			_ = backend.client.Close(ctx)
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		_ = backend.client.Close(ctx)
		return err
	}
	_ = backend.client.Close(ctx)
	select {
	case <-backend.client.Done():
		s.mu.Lock()
		if s.backend == backend {
			s.backend = nil
		}
		s.mu.Unlock()
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) startGo(ctx context.Context, root string, current *publishedAnalysis, w *workspace) (*goBackend, error) {
	executable := s.options.Gopls
	if executable == "off" {
		return nil, nil
	}
	if executable == "" {
		executable = "gopls"
	}
	var env []string
	if current.result != nil && current.result.Config.Environment != nil {
		env = current.result.Config.Environment.ExecEnv()
	}
	client, err := gopls.Start(s.ctx, gopls.Options{Path: executable, Dir: root, Env: env})
	if err != nil {
		return nil, err
	}
	backend := &goBackend{client: client, coordinator: gopls.NewCoordinator(client), root: root}
	uri, err := PathToURI(root)
	if err != nil {
		backend.closeContext(ctx)
		return nil, err
	}
	settings := map[string]any{"pullDiagnostics": true, "fileWatcher": "off", "diagnosticsTrigger": "Save", "diagnosticsDelay": "1s"}
	var flags []string
	if w.native != nil && w.native.NativeRoot() != "" {
		flags = append(flags, "-modfile="+w.native.GoModFile())
	}
	if len(s.options.Compiler.Tags) > 0 {
		flags = append(flags, "-tags="+strings.Join(s.options.Compiler.Tags, ","))
	}
	if len(flags) > 0 {
		settings["buildFlags"] = flags
	}
	result, err := client.Initialize(ctx, map[string]any{
		"processId": os.Getpid(), "rootUri": uri, "workspaceFolders": []any{map[string]any{"uri": uri, "name": "goxide"}},
		"capabilities":          map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-16"}}, "workspace": map[string]any{"configuration": true}, "textDocument": map[string]any{"publishDiagnostics": map[string]any{"versionSupport": true}}},
		"initializationOptions": settings,
	})
	if err != nil {
		backend.closeContext(ctx)
		return nil, err
	}
	var initialized struct {
		Capabilities struct {
			DiagnosticProvider json.RawMessage `json:"diagnosticProvider"`
			PositionEncoding   string          `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(result, &initialized); err != nil {
		backend.closeContext(ctx)
		return nil, err
	}
	if len(initialized.Capabilities.DiagnosticProvider) == 0 || string(initialized.Capabilities.DiagnosticProvider) == "null" {
		backend.closeContext(ctx)
		return nil, fmt.Errorf("gopls does not provide the required pull diagnostic capability")
	}
	if encoding := initialized.Capabilities.PositionEncoding; encoding != "" && encoding != "utf-16" {
		backend.closeContext(ctx)
		return nil, fmt.Errorf("gopls selected unsupported backend position encoding %s", encoding)
	}
	return backend, nil
}

func (s *Server) prepareGoView(w *workspace) (string, error) {
	if w.native != nil && w.native.NativeRoot() != "" {
		return w.native.NativeRoot(), nil
	}
	if s.goView == "" {
		directory, err := os.MkdirTemp("", "goxide-lsp-view-")
		if err != nil {
			return "", err
		}
		s.goView = directory
	}
	// The view outlives individual compiler preparations. Its stable paths are
	// private to this server and cannot be deleted by a replaced workspace.
	if s.goViewSource == filepath.Dir(w.goRoot) {
		return filepath.Join(s.goView, "module"), nil
	}
	if err := syncPrivateView(filepath.Dir(w.goRoot), s.goView); err != nil {
		return "", err
	}
	s.goViewSource = filepath.Dir(w.goRoot)
	return filepath.Join(s.goView, "module"), nil
}

func syncPrivateView(from, to string) error {
	wanted := map[string]bool{}
	err := filepath.WalkDir(from, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("private Go view contains a non-regular file: %s", name)
		}
		wanted[relative] = true
		data, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		destination := filepath.Join(to, relative)
		if old, err := os.ReadFile(destination); err == nil && bytes.Equal(old, data) {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		return os.WriteFile(destination, data, 0644)
	})
	if err != nil {
		return err
	}
	var directories []string
	err = filepath.WalkDir(to, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name != to {
				directories = append(directories, name)
			}
			return nil
		}
		relative, err := filepath.Rel(to, name)
		if err != nil {
			return err
		}
		if !wanted[relative] {
			return os.Remove(name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		entries, err := os.ReadDir(directories[i])
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			if err := os.Remove(directories[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Server) projectGo(current *publishedAnalysis, w *workspace, maps map[string]*source.Map) (*goProjection, map[string]string, error) {
	p := &goProjection{files: map[string]string{}, keys: map[string]string{}, uris: map[string]string{}, maps: maps, indexes: map[string]*source.Index{}}
	result := current.result
	if result == nil {
		return p, nil, nil
	}
	nativePath := func(key string) string {
		if w.native != nil && w.native.NativeRoot() != "" && w.native.OwnsTargetSource(current.names[key]) {
			return current.names[key]
		}
		disk := current.diskNames[key]
		if disk == "" {
			disk = result.DiskNames[key]
		}
		if disk == "" {
			return ""
		}
		if w.native != nil && w.native.NativeRoot() != "" {
			return disk
		}
		relative, err := filepath.Rel(filepath.Dir(w.goRoot), disk)
		if err != nil {
			return ""
		}
		return filepath.Join(s.goView, relative)
	}
	for key, input := range result.Sources {
		if filepath.Ext(key) != ".go" {
			continue
		}
		name := nativePath(key)
		if name == "" {
			continue
		}
		uri, err := PathToURI(name)
		if err != nil {
			return nil, nil, err
		}
		p.files[uri], p.keys[uri], p.uris[key] = string(input.Content), key, uri
	}
	for key, data := range current.generated {
		name := ""
		destination := result.DiskOutputs[key]
		if destination == "" && result.Projection != nil {
			destination = result.Projection.DiskOutputs[key]
		}
		if destination == "" {
			continue
		}
		if w.native != nil && w.native.NativeRoot() != "" {
			name = destination
			if w.native.OwnsTargetSource(current.names[key]) {
				name = strings.TrimSuffix(current.names[key], ".gox") + "_gox.go"
			}
		} else {
			relative, err := filepath.Rel(filepath.Dir(w.goRoot), destination)
			if err != nil {
				return nil, nil, err
			}
			name = filepath.Join(s.goView, relative)
		}
		if name == "" {
			continue
		}
		{
			if data, err := os.ReadFile(name); err == nil && !source.IsGenerated(name, data) {
				return nil, nil, fmt.Errorf("Go projection would mask an authored file: %s", name)
			} else if err != nil && !os.IsNotExist(err) {
				return nil, nil, err
			}
		}
		uri, err := PathToURI(name)
		if err != nil {
			return nil, nil, err
		}
		p.files[uri], p.keys[uri], p.uris[key] = string(data), key, uri
	}
	masked := map[string]string{}
	if w.native != nil && w.native.NativeRoot() != "" {
		if !w.ownedLoaded {
			err := filepath.WalkDir(w.root, func(name string, entry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if entry.IsDir() {
					if name != w.root {
						if entry.Name() == "vendor" || entry.Name() == "testdata" || strings.HasPrefix(entry.Name(), ".") || strings.HasPrefix(entry.Name(), "_") {
							return filepath.SkipDir
						}
						for _, marker := range []string{"go.mod", "goxide.toml", ".goxide-layout.json"} {
							if _, err := os.Stat(filepath.Join(name, marker)); err == nil {
								return filepath.SkipDir
							} else if !os.IsNotExist(err) {
								return err
							}
						}
					}
					return nil
				}
				if !strings.HasSuffix(name, "_gox.go") {
					return nil
				}
				original := strings.TrimSuffix(name, "_gox.go") + ".gox"
				if !w.native.OwnsTargetSource(original) {
					return nil
				}
				data, err := os.ReadFile(name)
				if err != nil {
					return err
				}
				if !source.IsGenerated(name, data) {
					return nil
				}
				file, err := parser.ParseFile(token.NewFileSet(), name, data, parser.PackageClauseOnly)
				if err != nil {
					return err
				}
				uri, err := PathToURI(name)
				if err != nil {
					return err
				}
				masked[uri] = file.Name.Name
				return nil
			})
			if err != nil {
				return nil, nil, err
			}
			w.owned, w.ownedLoaded = masked, true
		} else {
			masked = w.owned
		}
	}
	for uri, text := range p.files {
		p.indexes[uri] = source.New(text)
	}
	return p, masked, nil
}

func (s *Server) syncGo(ctx context.Context, current *publishedAnalysis, w *workspace, maps map[string]*source.Map) (*goProjection, error) {
	if s.options.Gopls == "off" {
		return nil, nil
	}
	root, err := s.prepareGoView(w)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	backend := s.backend
	retiring := backend != nil && backend.retiring
	s.mu.Unlock()
	if backend != nil && (backend.root != root || backend.preparedRoot != w.goRoot || backend.client.Err() != nil || retiring || backend.client.RequestCount() >= goRequestLimit) {
		if retiring || backend.client.RequestCount() >= goRequestLimit {
			if err := s.retireGo(ctx, backend); err != nil {
				return nil, err
			}
		} else {
			backend.close()
		}
		backend = nil
	}
	if backend == nil {
		backend, err = s.startGo(ctx, root, current, w)
		if err != nil {
			return nil, err
		}
		if backend == nil {
			return nil, nil
		}
		backend.preparedRoot = w.goRoot
		s.mu.Lock()
		s.backend = backend
		backend.coordinator.Invalidate(s.epoch)
		s.mu.Unlock()
	}
	projection, masked, err := s.projectGo(current, w, maps)
	if err != nil {
		return nil, err
	}
	projection.backend = backend
	err = backend.coordinator.Sync(ctx, gopls.Projection{Epoch: current.epoch, Files: projection.files, Masked: masked})
	return projection, err
}

func (s *Server) backendPosition(current *publishedAnalysis, key string, offset int) (string, source.Position, bool) {
	p := current.goProjection
	if p == nil {
		return "", source.Position{}, false
	}
	uri := p.uris[key]
	if uri == "" {
		return "", source.Position{}, false
	}
	if filepath.Ext(key) == ".go" {
		position, err := p.indexes[uri].Position(offset, source.UTF16)
		return uri, position, err == nil
	}
	var best *source.Mapping
	for _, entry := range p.maps[key].Entries() {
		if entry.Kind == source.Synthetic || entry.Source.File != key || entry.Source.Span.Start > offset || entry.Source.Span.End <= offset {
			continue
		}
		if best == nil || entry.Source.Span.End-entry.Source.Span.Start < best.Source.Span.End-best.Source.Span.Start {
			copy := entry
			best = &copy
		}
	}
	if best == nil {
		return "", source.Position{}, false
	}
	generated := best.Generated.Start
	if best.Kind == source.Exact {
		generated += offset - best.Source.Span.Start
	}
	position, err := p.indexes[uri].Position(generated, source.UTF16)
	return uri, position, err == nil
}

func (s *Server) mapGoLocation(current *publishedAnalysis, location wireLocation) (wireLocation, bool) {
	p := current.goProjection
	if p == nil {
		return wireLocation{}, false
	}
	key := p.keys[location.URI]
	if key == "" {
		if s.encoding == source.UTF16 {
			return location, true
		}
		name, err := URIToPath(location.URI)
		if err != nil {
			return wireLocation{}, false
		}
		data, err := os.ReadFile(name)
		if err != nil {
			return wireLocation{}, false
		}
		index := source.New(string(data))
		start, end, err := index.Offsets(location.Range, source.UTF16)
		if err != nil {
			return wireLocation{}, false
		}
		location.Range.Start, err = index.Position(start, s.encoding)
		if err != nil {
			return wireLocation{}, false
		}
		location.Range.End, err = index.Position(end, s.encoding)
		return location, err == nil
	} // Unmodified external Go source.
	index := p.indexes[location.URI]
	start, end, err := index.Offsets(location.Range, source.UTF16)
	if err != nil {
		return wireLocation{}, false
	}
	if filepath.Ext(key) == ".go" {
		return s.location(current, analysis.Location{File: key, Span: syntaxSpan(start, end)})
	}
	mapping, ok := p.maps[key].Generated(source.ByteSpan{Start: start, End: end})
	if !ok || mapping.Kind == source.Synthetic {
		startPosition, startErr := index.Position(start, s.encoding)
		endPosition, endErr := index.Position(end, s.encoding)
		return wireLocation{URI: "goxide-generated:" + key, Range: source.Range{Start: startPosition, End: endPosition}}, startErr == nil && endErr == nil
	}
	return s.location(current, analysis.Location{File: mapping.Source.File, Span: syntaxSpan(mapping.Source.Span.Start, mapping.Source.Span.End)})
}

func (s *Server) goQuery(ctx context.Context, current *publishedAnalysis, key string, offset int, method string) (any, error) {
	current, release, err := s.acquireGo(ctx, current)
	if err != nil {
		return nil, err
	}
	defer release()
	if current.goProjection == nil {
		return nil, nil
	}
	uri, position, ok := s.backendPosition(current, key, offset)
	if !ok {
		return nil, nil
	}
	raw, err := current.goProjection.backend.coordinator.Query(ctx, current.epoch, method, map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position})
	if err != nil {
		if current.goProjection.backend.client.Err() != nil {
			s.mu.Lock()
			if s.epoch == current.epoch && !current.goProjection.backend.retiring {
				s.invalidateLocked(s.sequence, false)
			}
			s.mu.Unlock()
		}
		return nil, err
	}
	if string(raw) == "null" {
		return nil, nil
	}
	if method == "textDocument/definition" {
		var locations []wireLocation
		if err := json.Unmarshal(raw, &locations); err != nil {
			var single wireLocation
			if singleErr := json.Unmarshal(raw, &single); singleErr != nil {
				return nil, err
			}
			locations = []wireLocation{single}
		}
		mapped := []wireLocation{}
		for _, location := range locations {
			if location, ok := s.mapGoLocation(current, location); ok {
				mapped = append(mapped, location)
			}
		}
		return mapped, nil
	}
	if method == "textDocument/hover" {
		var hover struct {
			Contents json.RawMessage `json:"contents"`
			Range    *source.Range   `json:"range,omitempty"`
		}
		if err := json.Unmarshal(raw, &hover); err != nil {
			return nil, err
		}
		if hover.Range != nil {
			if mapped, ok := s.mapGoLocation(current, wireLocation{URI: uri, Range: *hover.Range}); ok {
				hover.Range = &mapped.Range
			} else {
				hover.Range = nil
			}
		}
		return hover, nil
	}
	if method == "textDocument/completion" {
		return s.goCompletions(current, key, raw)
	}
	if method == "textDocument/documentSymbol" {
		return s.goSymbols(current, uri, raw)
	}
	return nil, nil
}

// Pull only documents displayed by the client. Complete compiler diagnostics
// already cover the selected graph; open consumers are pulled even if unchanged.
func (s *Server) pullGoDiagnostics(ctx context.Context, current *publishedAnalysis) (map[string][]wireDiagnostic, error) {
	p := current.goProjection
	var uris []string
	for uri := range current.documents.Documents {
		name, err := URIToPath(uri)
		if err != nil {
			continue
		}
		if generated := p.uris[current.keys[filepath.Clean(name)]]; generated != "" {
			uris = append(uris, generated)
		}
	}
	sort.Strings(uris)
	reports, err := p.backend.coordinator.Diagnostics(ctx, current.epoch, uris)
	if err != nil {
		return nil, err
	}
	out := map[string][]wireDiagnostic{}
	for uri, raw := range reports {
		var report struct {
			Items []wireDiagnostic `json:"items"`
		}
		if err := json.Unmarshal(raw, &report); err != nil {
			return nil, err
		}
		for _, item := range report.Items {
			mapped, ok := s.mapGoLocation(current, wireLocation{URI: uri, Range: item.Range})
			if !ok {
				continue
			}
			item.Range, item.Source = mapped.Range, "gopls"
			related := item.Related[:0]
			for _, link := range item.Related {
				if location, ok := s.mapGoLocation(current, link.Location); ok {
					link.Location = location
					related = append(related, link)
				}
			}
			item.Related = related
			out[mapped.URI] = append(out[mapped.URI], item)
		}
	}
	return out, nil
}

func syntaxSpan(start, end int) syntax.Span {
	return syntax.Span{Start: uint32(start), End: uint32(end)}
}
