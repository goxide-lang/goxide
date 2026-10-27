package lsp

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/source"
	"github.com/goxide-lang/goxide/internal/typecheck"
)

type candidateRequest struct {
	ctx      context.Context
	current  *publishedAnalysis
	edits    []analysis.SourceEdit
	strict   bool
	validate func(context.Context, *analysis.Result) error
	done     chan error
}

// validateCandidate uses the worker's selected workspace without publishing a
// new generation. The callback runs before the private macro session and source
// lease close; neither it nor its result may retain the candidate's importer.
func (s *Server) validateCandidate(ctx context.Context, current *publishedAnalysis, edits []analysis.SourceEdit, strict bool, validate func(context.Context, *analysis.Result) error) error {
	if _, err := s.guardSourceEdits(current, edits); err != nil {
		return err
	}
	if !strict && validate == nil {
		return fmt.Errorf("incomplete candidate requires an explicit validation policy")
	}
	request := candidateRequest{ctx: ctx, current: current, edits: append([]analysis.SourceEdit(nil), edits...), strict: strict, validate: validate, done: make(chan error, 1)}
	s.mu.Lock()
	queue := s.candidates
	s.mu.Unlock()
	if queue == nil {
		return fmt.Errorf("candidate analysis worker is unavailable")
	}
	select {
	case queue <- request:
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
	select {
	case err := <-request.done:
		if err != nil {
			return err
		}
		_, err = s.guardSourceEdits(current, edits)
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// Only the analysis worker calls checkCandidate. Keeping candidate work on the
// same worker prevents preparation, native-root activation or workspace cleanup
// from invalidating its snapshot or macro host while validation is running.
func (s *Server) checkCandidate(w *workspace, request candidateRequest) {
	ctx, cancel := context.WithCancel(request.ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	s.mu.Lock()
	valid := w != nil && request.current != nil && request.current.epoch == s.epoch && !s.shutdown && !s.reload
	if valid {
		s.workCancel = cancel
	}
	s.mu.Unlock()
	if !valid {
		request.done <- staleResult()
		return
	}
	err := s.analyzeCandidate(ctx, w, request)
	s.mu.Lock()
	s.workCancel = nil
	valid = request.current.epoch == s.epoch && !s.shutdown
	s.mu.Unlock()
	if !valid {
		err = staleResult()
	} else if ctx.Err() != nil {
		err = ctx.Err()
	}
	request.done <- err
}

func (s *Server) analyzeCandidate(ctx context.Context, w *workspace, request candidateRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := s.guardSourceEdits(request.current, request.edits); err != nil {
		return err
	}
	documents := request.current.documents
	documents.Documents = maps.Clone(documents.Documents)
	groups := map[string][]analysis.SourceEdit{}
	for _, edit := range request.edits {
		name := request.current.names[edit.File]
		if w.macroSource(name) {
			return fmt.Errorf("candidate edits cannot change a prepared procedural macro source")
		}
		groups[edit.File] = append(groups[edit.File], edit)
	}
	for key, edits := range groups {
		name, text := request.current.names[key], request.current.indexes[key].Text()
		sort.Slice(edits, func(i, j int) bool { return edits[i].Span.Start > edits[j].Span.Start })
		for _, edit := range edits {
			text = text[:edit.Span.Start] + edit.NewText + text[edit.Span.End:]
		}
		document, open, err := documentAtPath(documents.Documents, name)
		if err != nil {
			return err
		}
		if !open {
			document.URI, err = PathToURI(name)
			if err != nil {
				return err
			}
		}
		document.Text, document.Index = text, source.New(text)
		documents.Documents[document.URI] = document
	}
	snapshot, err := w.snapshot(ctx, documents)
	if err != nil {
		return err
	}
	defer snapshot.close()
	snapshot.input.Options.Editor = true
	snapshot.input.Options.PreserveSyntax = true
	snapshot.input.Options.Previous = nil
	// Candidate validation cannot publish diagnostics or update a prepared
	// project's source inventory through caller-provided compilation callbacks.
	snapshot.input.Options.OnProcDiagnostic = nil
	snapshot.input.Modules = append([]analysis.Module(nil), snapshot.input.Modules...)
	for i := range snapshot.input.Modules {
		snapshot.input.Modules[i].OnProcDiagnostic = nil
		snapshot.input.Modules[i].OnSourcePackages = nil
	}
	result, err := analysis.Analyze(ctx, snapshot.input)
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("candidate analysis produced no result")
	}
	if request.strict && result.Err != nil {
		return fmt.Errorf("candidate source is invalid: %w", result.Err)
	}
	if result.Err == nil {
		generated, err := lower.Generate(ctx, result, lower.GenerationOptions{})
		if err != nil {
			return fmt.Errorf("candidate generation failed: %w", err)
		}
		if err := typecheck.CheckGeneratedGraph(ctx, typecheck.GeneratedSources(result, generated), result.ExternalImporter(), result.Config.Sizes()); err != nil {
			return fmt.Errorf("candidate generated Go is invalid: %w", err)
		}
	}
	if request.validate != nil {
		if err := request.validate(ctx, result); err != nil {
			return err
		}
	}
	return ctx.Err()
}
