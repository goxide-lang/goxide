package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/source"
	"github.com/goxide-lang/goxide/internal/typecheck"
)

type ServerOptions struct {
	Directory string
	Version   string
	Compiler  compile.Options
	Gopls     string
	Log       io.Writer
}

type publishedAnalysis struct {
	complete         bool
	backendError     error
	generateError    error
	diskNames        map[string]string
	sourceMaps       map[string]*source.Map
	goDiagnostics    map[string][]wireDiagnostic
	goProjection     *goProjection
	epoch            uint64
	result           *analysis.Result
	queries          *analysis.Queries
	names            map[string]string
	keys             map[string]string
	indexes          map[string]*source.Index
	documents        DocumentSnapshot
	documentRevision uint64
	generated        map[string][]byte
}

type Server struct {
	backend               *goBackend
	backendMaintenance    *goBackend
	maintenanceDeadline   time.Time
	goView                string
	goViewSource          string
	goFailure             string
	options               ServerOptions
	connection            *Connection
	ctx                   context.Context
	cancel                context.CancelFunc
	documents             *DocumentStore
	work                  chan struct{}
	workers               sync.WaitGroup
	mu                    sync.Mutex
	initialized, shutdown bool
	encoding              source.Encoding
	directory             string
	epoch, sequence       uint64
	documentRevision      uint64
	reload                bool
	macroSource           func(string) bool
	watchSource           func(string) bool
	preparing             bool
	watchRoot             string
	workCancel            context.CancelFunc
	changed               chan struct{}
	current               *publishedAnalysis
	publishedURIs         map[string]bool
}

func Serve(ctx context.Context, input io.ReadCloser, output io.Writer, options ServerOptions) error {
	if options.Version == "" {
		options.Version = "dev"
	}
	if options.Compiler.GoTool == "" {
		options.Compiler.GoTool = "go"
	}
	if options.Log == nil {
		options.Log = io.Discard
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Server{options: options, connection: NewConnection(input, output), ctx: ctx, cancel: cancel, documents: NewDocumentStore(source.UTF16), encoding: source.UTF16, work: make(chan struct{}, 1), changed: make(chan struct{}), publishedURIs: map[string]bool{}}
	s.workers.Go(s.analyze)
	err := s.connection.Serve(ctx, s.request, s.notification)
	cancel()
	s.mu.Lock()
	if s.workCancel != nil {
		s.workCancel()
	}
	s.mu.Unlock()
	s.workers.Wait()
	s.backend.close()
	if s.goView != "" {
		_ = os.RemoveAll(s.goView)
	}
	return err
}

func invalidParams(err error) error { return &RPCError{Code: CodeInvalidParams, Message: err.Error()} }
func staleResult() error {
	return &RPCError{Code: CodeContentModified, Message: "document or project configuration changed"}
}
func (s *Server) signalLocked() { close(s.changed); s.changed = make(chan struct{}) }
func (s *Server) invalidateLocked(sequence uint64, reload bool) {
	s.epoch++
	s.backendMaintenance = nil
	s.maintenanceDeadline = time.Time{}
	if s.backend != nil {
		s.backend.coordinator.Invalidate(s.epoch)
	}
	s.sequence = sequence
	s.reload = s.reload || reload
	if s.workCancel != nil {
		s.workCancel()
	}
	s.signalLocked()
	select {
	case s.work <- struct{}{}:
	default:
	}
}

func (s *Server) request(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method == "initialize" {
		return s.initialize(raw)
	}
	s.mu.Lock()
	initialized, shutdown := s.initialized, s.shutdown
	s.mu.Unlock()
	if !initialized {
		return nil, &RPCError{Code: -32002, Message: "server is not initialized"}
	}
	if shutdown {
		return nil, &RPCError{Code: CodeInvalidRequest, Message: "server is shutting down"}
	}
	if method == "shutdown" {
		s.mu.Lock()
		s.shutdown = true
		if s.workCancel != nil {
			s.workCancel()
		}
		s.signalLocked()
		s.mu.Unlock()
		return nil, nil
	}
	switch method {
	case "goxide/generatedSource":
		return s.generatedSource(ctx, raw)
	case "textDocument/hover", "textDocument/definition", "textDocument/completion", "textDocument/documentSymbol":
	default:
		return nil, &RPCError{Code: CodeMethodNotFound, Message: "unsupported method: " + method}
	}
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position source.Position `json:"position"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	filename, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, invalidParams(err)
	}
	current, err := s.await(ctx)
	if err != nil {
		return nil, err
	}
	key := current.keys[filepath.Clean(filename)]
	if key == "" {
		return nil, nil
	}
	index := current.indexes[key]
	if index == nil {
		return nil, nil
	}
	offset, err := index.Offset(params.Position, s.encoding)
	if err != nil {
		return nil, invalidParams(err)
	}
	var answer any
	needsGo := filepath.Ext(key) == ".go"
	if !needsGo {
		switch method {
		case "textDocument/hover":
			hover, queryErr := current.queries.Hover(key, uint32(offset))
			err = queryErr
			if hover != nil {
				if location, ok := s.location(current, hover.Range); ok {
					answer = map[string]any{"contents": map[string]string{"kind": "markdown", "value": "```goxide\n" + hover.Text + "\n```"}, "range": location.Range}
				}
			}
		case "textDocument/definition":
			definitions, queryErr := current.queries.Definition(key, uint32(offset))
			err = queryErr
			locations := []wireLocation{}
			for _, definition := range definitions {
				needsGo = needsGo || definition.Go != nil
				if location, ok := s.location(current, definition); ok {
					locations = append(locations, location)
				}
			}
			answer = locations
		case "textDocument/completion":
			completions, queryErr := current.queries.Complete(key, uint32(offset))
			err = queryErr
			items := []map[string]any{}
			for _, completion := range completions {
				items = append(items, map[string]any{"label": completion.Name, "detail": completion.Detail, "kind": completionKind(completion.Kind)})
			}
			answer = map[string]any{"isIncomplete": false, "items": items}
		case "textDocument/documentSymbol":
			symbols, queryErr := current.queries.Symbols(key)
			err = queryErr
			answer = s.symbols(current, symbols)
		}
	}
	if needsGo {
		current, err = s.awaitComplete(ctx)
		if err != nil {
			return nil, err
		}
		if current.goProjection != nil {
			answer, err = s.goQuery(ctx, current, key, offset, method)
		}
	}
	var unavailable *analysis.QueryUnavailable
	if errors.As(err, &unavailable) {
		return nil, &RPCError{Code: CodeContentModified, Message: unavailable.Reason}
	}
	s.mu.Lock()
	valid := s.epoch == current.epoch && !s.shutdown
	s.mu.Unlock()
	if !valid {
		return nil, staleResult()
	}
	return answer, err
}

func (s *Server) initialize(raw json.RawMessage) (any, error) {
	var params struct {
		RootURI          string `json:"rootUri"`
		WorkspaceFolders []struct {
			URI string `json:"uri"`
		} `json:"workspaceFolders"`
		Capabilities struct {
			General struct {
				PositionEncodings []source.Encoding `json:"positionEncodings"`
			} `json:"general"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	directory := s.options.Directory
	if directory == "" {
		uri := params.RootURI
		if len(params.WorkspaceFolders) > 0 {
			uri = params.WorkspaceFolders[0].URI
		}
		if len(params.WorkspaceFolders) > 1 {
			return nil, invalidParams(fmt.Errorf("select one project folder per Goxide session"))
		}
		if uri != "" {
			var err error
			directory, err = URIToPath(uri)
			if err != nil {
				return nil, invalidParams(err)
			}
		} else {
			directory, _ = os.Getwd()
		}
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return nil, invalidParams(err)
	}
	encoding := source.UTF16
	for _, candidate := range params.Capabilities.General.PositionEncodings {
		if candidate == source.UTF8 || candidate == source.UTF16 || candidate == source.UTF32 {
			encoding = candidate
			break
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.initialized {
		return nil, &RPCError{Code: CodeInvalidRequest, Message: "server is already initialized"}
	}
	s.directory, s.encoding, s.documents = directory, encoding, NewDocumentStore(encoding)
	s.initialized = true
	s.invalidateLocked(0, true)
	return map[string]any{"capabilities": map[string]any{"positionEncoding": encoding, "textDocumentSync": map[string]any{"openClose": true, "change": 2, "save": map[string]any{"includeText": false}}, "hoverProvider": true, "definitionProvider": true, "completionProvider": map[string]any{"triggerCharacters": []string{".", ":"}}, "documentSymbolProvider": true}, "serverInfo": map[string]string{"name": "goxide", "version": s.options.Version}}, nil
}

func (s *Server) notification(ctx context.Context, method string, raw json.RawMessage) error {
	if method == "exit" {
		_ = s.connection.Close()
		return nil
	}
	if method == "initialized" || method == "$/setTrace" {
		return nil
	}
	s.mu.Lock()
	initialized, shutdown := s.initialized, s.shutdown
	s.mu.Unlock()
	if !initialized || shutdown {
		return nil
	}
	var params struct {
		TextDocument struct {
			URI     string `json:"uri"`
			Version int    `json:"version"`
			Text    string `json:"text"`
		} `json:"textDocument"`
		ContentChanges []ContentChange `json:"contentChanges"`
		Changes        []struct {
			URI  string `json:"uri"`
			Type int    `json:"type"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		fmt.Fprintln(s.options.Log, "LSP notification:", err)
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutdown {
		return nil
	}
	changed, reload := false, false
	var err error
	switch method {
	case "textDocument/didOpen":
		_, err = s.documents.Open(params.TextDocument.URI, params.TextDocument.Version, params.TextDocument.Text)
		changed = err == nil
		if changed {
			s.documentRevision++
		}
	case "textDocument/didChange":
		_, changed, err = s.documents.Change(params.TextDocument.URI, params.TextDocument.Version, params.ContentChanges)
		if err == nil {
			s.documentRevision++
			if !changed {
				// Positions still describe the same bytes, so analysis and Go
				// overlays remain valid. New requests bind the new document
				// version; requests received before it cannot acquire that bind.
				s.sequence = ReceiveSequence(ctx)
				s.signalLocked()
			}
		}
	case "textDocument/didClose":
		_, changed = s.documents.Close(params.TextDocument.URI)
		reload = changed // Closing reveals the current disk bytes, including prior saves.
		if changed {
			s.documentRevision++
		}
	case "textDocument/didSave":
		return nil // Unchanged saves do not start another analysis.
	case "workspace/didChangeWatchedFiles":
		for _, change := range params.Changes {
			name, pathErr := URIToPath(change.URI)
			if pathErr != nil || s.ignoredWatch(name) {
				continue
			}
			if s.watchSource == nil || s.watchSource(name) {
				changed, reload = true, true
				break
			}
		}
	case "workspace/didChangeConfiguration":
		changed, reload = true, true
	default:
		return nil
	}
	if err != nil {
		fmt.Fprintln(s.options.Log, "LSP notification:", err)
		return nil
	}
	if changed {
		reload = reload || s.preparing
		if name, pathErr := URIToPath(params.TextDocument.URI); pathErr == nil && s.macroSource != nil && s.macroSource(name) {
			reload = true
		}
		s.invalidateLocked(ReceiveSequence(ctx), reload)
	}
	return nil
}

func (s *Server) await(ctx context.Context) (*publishedAnalysis, error) {
	return s.awaitAnalysis(ctx, false)
}

func (s *Server) awaitComplete(ctx context.Context) (*publishedAnalysis, error) {
	return s.awaitAnalysis(ctx, true)
}

func (s *Server) awaitAnalysis(ctx context.Context, complete bool) (*publishedAnalysis, error) {
	for {
		s.mu.Lock()
		if sequence := ReceiveSequence(ctx); sequence != 0 && sequence < s.sequence {
			s.mu.Unlock()
			return nil, staleResult()
		}
		if s.shutdown {
			s.mu.Unlock()
			return nil, context.Canceled
		}
		current, epoch, changed := s.current, s.epoch, s.changed
		if current != nil && current.epoch == epoch && (!complete || current.complete) {
			if current.documentRevision != s.documentRevision {
				// A version-only edit may happen before or after the analysis
				// worker publishes. Rebind lazily without mutating snapshots
				// retained by in-flight requests or copying every query's maps.
				rebound := *current
				rebound.documents = s.documents.Snapshot()
				rebound.documentRevision = s.documentRevision
				current, s.current = &rebound, &rebound
			}
			s.mu.Unlock()
			return current, nil
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		case <-changed:
		}
	}
}

func (s *Server) analyze() {
	var workspace *workspace
	defer func() { workspace.close() }()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.work:
		}
		// Coalesce typing without delaying the reader or cancellation path.
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		s.mu.Lock()
		if s.shutdown {
			s.mu.Unlock()
			continue
		}
		select {
		case <-s.work:
		default:
		}
		epoch, reload, directory := s.epoch, s.reload, s.directory
		s.reload = false
		ctx, cancel := context.WithCancel(s.ctx)
		s.workCancel = cancel
		documents := s.documents.Snapshot()
		documentRevision := s.documentRevision
		previous := s.current
		maintenance := s.backendMaintenance
		deadline := s.maintenanceDeadline
		reuse := maintenance != nil && !reload && workspace != nil && previous != nil && previous.complete && previous.epoch == epoch && previous.goProjection != nil && previous.goProjection.backend == maintenance
		if !reuse {
			s.backendMaintenance = nil
			s.maintenanceDeadline = time.Time{}
		}
		if reload || workspace == nil {
			s.preparing, s.macroSource, s.watchSource = true, nil, nil
		}
		s.mu.Unlock()
		if reuse {
			current := *previous
			current.documents, current.documentRevision = documents, documentRevision
			s.refreshGo(ctx, &current, workspace, deadline)
			if ctx.Err() == nil {
				s.publish(&current, current.generateError)
			}
			cancel()
			continue
		}
		if reload {
			s.mu.Lock()
			backend := s.backend
			s.backend = nil
			s.mu.Unlock()
			backend.close()
			workspace.close()
			workspace = nil
		}
		var err error
		newWorkspace := workspace == nil
		if workspace == nil {
			options := s.options.Compiler
			options.SourceOverlays = map[string][]byte{}
			for uri, document := range documents.Documents {
				if name, pathErr := URIToPath(uri); pathErr == nil {
					options.SourceOverlays[name] = []byte(document.Text)
				}
			}
			workspace, err = prepareWorkspace(ctx, directory, options)
			if err == nil {
				s.setWorkspaceCallbacks(workspace)
			}
		}
		if err != nil {
			s.publishFailure(epoch, documents, err)
			cancel()
			continue
		}
		snapshot, result, analysisErr := s.analyzeWorkspace(ctx, workspace, documents, previous, reload)
		err = analysisErr
		if snapshot == nil {
			s.publishFailure(epoch, documents, err)
			s.setWorkspaceCallbacks(nil)
			workspace.close()
			workspace = nil
			cancel()
			continue
		}
		if ctx.Err() != nil {
			snapshot.close()
			if newWorkspace {
				workspace.close()
				workspace = nil
			}
			cancel()
			continue
		}
		current := &publishedAnalysis{epoch: epoch, result: result, names: snapshot.names, keys: map[string]string{}, indexes: map[string]*source.Index{}, documents: documents, documentRevision: documentRevision, diskNames: snapshot.input.DiskNames}
		if result != nil {
			current.queries = analysis.NewQueries(result)
			for key, input := range result.Sources {
				current.indexes[key] = source.New(string(input.Content))
				name := current.names[key]
				if name != "" {
					current.keys[filepath.Clean(name)] = key
				}
			}
		}
		s.publish(current, analysisErr)
		projection := result
		if result != nil && result.Err != nil {
			projection = result.Projection
		}
		complete := *current
		complete.complete = true
		complete.sourceMaps = map[string]*source.Map{}
		var generateErr error
		if projection != nil && projection.Checked != nil {
			complete.generated, generateErr = lower.Generate(ctx, projection, lower.GenerationOptions{SourceMaps: complete.sourceMaps})
			if generateErr == nil && !projection.ProjectionOnly {
				generateErr = typecheck.CheckGeneratedGraph(ctx, typecheck.GeneratedSources(projection, complete.generated), projection.ExternalImporter(), projection.Config.Sizes())
			}
		}
		if ctx.Err() == nil {
			complete.generateError = generateErr
			s.refreshGo(ctx, &complete, workspace, time.Time{})
			if ctx.Err() == nil {
				s.publish(&complete, generateErr)
			}
		}

		snapshot.close()
		cancel()
	}
}

func (s *Server) generatedSource(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	current, err := s.awaitComplete(ctx)
	if err != nil {
		return nil, err
	}
	key := strings.TrimPrefix(params.URI, "goxide-generated:")
	if key == params.URI {
		name, err := URIToPath(params.URI)
		if err != nil {
			return nil, invalidParams(err)
		}
		key = current.keys[filepath.Clean(name)]
	}
	data, ok := current.generated[key]
	if !ok {
		return nil, invalidParams(fmt.Errorf("no current generated source for %s", params.URI))
	}
	s.mu.Lock()
	valid := s.epoch == current.epoch && !s.shutdown
	s.mu.Unlock()
	if !valid {
		return nil, staleResult()
	}
	return map[string]string{"uri": "goxide-generated:" + key, "content": string(data)}, nil
}

func completionKind(kind string) int {
	switch kind {
	case "function", "func", "method":
		return 3
	case "field":
		return 5
	case "type", "struct", "enum", "trait":
		return 7
	case "constant", "const":
		return 21
	case "module", "package":
		return 9
	default:
		return 6
	}
}
func symbolKind(kind string) int {
	switch kind {
	case "function", "func":
		return 12
	case "method":
		return 6
	case "field":
		return 8
	case "struct":
		return 23
	case "enum":
		return 10
	case "trait", "interface":
		return 11
	case "constant", "const":
		return 14
	case "module", "package":
		return 4
	default:
		return 13
	}
}

type wireLocation struct {
	URI   string       `json:"uri"`
	Range source.Range `json:"range"`
}

func (s *Server) location(current *publishedAnalysis, location analysis.Location) (wireLocation, bool) {
	name, index := current.names[location.File], current.indexes[location.File]
	if name == "" || index == nil {
		return wireLocation{}, false
	}
	uri, err := PathToURI(name)
	if err != nil {
		return wireLocation{}, false
	}
	start, err := index.Position(int(location.Span.Start), s.encoding)
	if err != nil {
		return wireLocation{}, false
	}
	end, err := index.Position(int(location.Span.End), s.encoding)
	if err != nil {
		return wireLocation{}, false
	}
	return wireLocation{URI: uri, Range: source.Range{Start: start, End: end}}, true
}
func (s *Server) symbols(current *publishedAnalysis, symbols []analysis.DocumentSymbol) []map[string]any {
	result := []map[string]any{}
	for _, symbol := range symbols {
		whole, ok := s.location(current, symbol.Range)
		if !ok {
			continue
		}
		selection, ok := s.location(current, symbol.Selection)
		if !ok {
			continue
		}
		result = append(result, map[string]any{"name": symbol.Name, "detail": symbol.Detail, "kind": symbolKind(symbol.Kind), "range": whole.Range, "selectionRange": selection.Range, "children": s.symbols(current, symbol.Children)})
	}
	return result
}
