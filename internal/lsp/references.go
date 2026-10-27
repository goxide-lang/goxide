package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	gotoken "go/token"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/source"
)

func (s *Server) referencesAndRename(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position source.Position `json:"position"`
		NewName  string          `json:"newName"`
		Context  struct {
			IncludeDeclaration bool `json:"includeDeclaration"`
		} `json:"context"`
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
	index := current.indexes[key]
	if key == "" || index == nil {
		return nil, nil
	}
	offset, err := index.Offset(params.Position, s.encoding)
	if err != nil {
		return nil, invalidParams(err)
	}
	validName := analysis.ValidRename(params.NewName)
	if filepath.Ext(key) == ".go" {
		validName = gotoken.IsIdentifier(params.NewName) && params.NewName != "_"
	}
	if method == "textDocument/rename" && !validName {
		return nil, invalidParams(fmt.Errorf("%q is not an editable identifier", params.NewName))
	}
	needsGo := filepath.Ext(key) == ".go"
	var refs *analysis.References
	if !needsGo {
		if method == "textDocument/references" {
			refs, err = current.queries.FindReferences(key, uint32(offset), params.Context.IncludeDeclaration)
		} else {
			refs, err = current.queries.PrepareRename(key, uint32(offset))
		}
		var unavailable *analysis.QueryUnavailable
		if errors.As(err, &unavailable) && unavailable.Go {
			needsGo, err = true, nil
		}
		if err != nil {
			return nil, refactorFailure(err)
		}
		if refs != nil {
			needsGo = needsGo || refs.NeedsGo || !refs.Local && hasNativeSource(current)
		}
	}
	if refs == nil && !needsGo {
		return nil, nil
	}
	var planned []analysis.SourceEdit
	goName := params.NewName
	if method == "textDocument/rename" && refs != nil {
		planned, err = current.queries.Rename(key, uint32(offset), params.NewName)
		if err != nil {
			return nil, refactorFailure(err)
		}
		goName, err = current.queries.GoNameAfterRename(key, uint32(offset), params.NewName)
		if err != nil {
			return nil, refactorFailure(err)
		}
		if goName == refs.GoName {
			needsGo = false
		}
	}
	var goResult json.RawMessage
	if needsGo {
		previous := current
		current, err = s.awaitComplete(ctx)
		if err != nil {
			return nil, err
		}
		if current.epoch != previous.epoch {
			return nil, staleResult()
		}
		if current.goProjection == nil {
			return nil, refactorFailure(fmt.Errorf("this operation requires the Go language service"))
		}
		extra := map[string]any{}
		if method == "textDocument/references" {
			extra["context"] = params.Context
		}
		if method == "textDocument/rename" {
			extra["newName"] = goName
		}
		answer, queryErr := s.goQuery(ctx, current, key, offset, method, extra)
		if queryErr != nil {
			return nil, refactorFailure(queryErr)
		}
		if answer != nil {
			goResult, _ = answer.(json.RawMessage)
		}
	}
	switch method {
	case "textDocument/references":
		locations := []wireLocation{}
		if refs != nil {
			for _, location := range refs.Locations {
				if mapped, ok := s.location(current, location); ok {
					locations = append(locations, mapped)
				}
			}
		}
		if presentJSON(goResult) {
			var native []wireLocation
			if err := json.Unmarshal(goResult, &native); err != nil {
				return nil, err
			}
			for _, location := range native {
				if mapped, ok := s.mapGoLocation(current, location); ok && !strings.HasPrefix(mapped.URI, "goxide-generated:") {
					locations = append(locations, mapped)
				}
			}
		}
		locations = uniqueLocations(locations)
		s.mu.Lock()
		valid := s.epoch == current.epoch && !s.shutdown
		s.mu.Unlock()
		if !valid {
			return nil, staleResult()
		}
		return locations, nil
	case "textDocument/prepareRename":
		preparedResult := func(value any) (any, error) {
			s.mu.Lock()
			valid := s.epoch == current.epoch && !s.shutdown
			s.mu.Unlock()
			if !valid {
				return nil, staleResult()
			}
			return value, nil
		}
		if !current.editable[key] {
			return nil, refactorFailure(fmt.Errorf("rename requires an authored workspace source"))
		}
		if refs != nil {
			for _, location := range refs.Locations {
				if location.File == key && location.Span.Start <= uint32(offset) && uint32(offset) < location.Span.End {
					if mapped, ok := s.location(current, location); ok {
						return preparedResult(map[string]any{"range": mapped.Range, "placeholder": index.Text()[location.Span.Start:location.Span.End]})
					}
				}
			}
		}
		if !presentJSON(goResult) {
			return nil, nil
		}
		var prepared struct {
			Range       *source.Range `json:"range"`
			Placeholder string        `json:"placeholder"`
		}
		if err := json.Unmarshal(goResult, &prepared); err != nil {
			return nil, err
		}
		if prepared.Range == nil {
			var range_ completionRange
			if err := json.Unmarshal(goResult, &range_); err != nil {
				return nil, err
			}
			prepared.Range = &range_.Range
		}
		uri := current.goProjection.uris[key]
		mapped, err := s.goSourceEdit(current, goEdit{URI: uri, Edit: wireTextEdit{Range: *prepared.Range}})
		if err != nil {
			return nil, refactorFailure(err)
		}
		location, ok := s.location(current, analysis.Location{File: mapped.File, Span: mapped.Span})
		if !ok {
			return nil, nil
		}
		return preparedResult(map[string]any{"range": location.Range, "placeholder": prepared.Placeholder})
	case "textDocument/rename":
		if needsGo && !presentJSON(goResult) {
			return nil, refactorFailure(fmt.Errorf("the Go language service did not produce a complete rename"))
		}
		if presentJSON(goResult) {
			backend, err := backendWorkspaceEdits(goResult)
			if err != nil {
				return nil, refactorFailure(err)
			}
			for _, edit := range backend {
				mapped, err := s.renameSourceEdits(current, edit, planned)
				if err != nil {
					return nil, refactorFailure(err)
				}
				planned = append(planned, mapped...)
			}
		}
		planned, err = uniqueSourceEdits(planned)
		if err != nil {
			return nil, refactorFailure(err)
		}
		if err := current.queries.ValidateDeclarationRenames(planned); err != nil {
			return nil, refactorFailure(err)
		}
		if len(planned) != 0 {
			previous := current
			current, err = s.awaitComplete(ctx)
			if err != nil {
				return nil, err
			}
			if current.epoch != previous.epoch {
				return nil, staleResult()
			}
			if current.result == nil || current.result.Err != nil || current.generateError != nil {
				return nil, refactorFailure(fmt.Errorf("rename requires valid current source and generated Go"))
			}
			if err := s.validateCandidate(ctx, current, planned, true, func(_ context.Context, candidate *analysis.Result) error {
				return current.queries.ValidateBindings(candidate, planned)
			}); err != nil {
				return nil, refactorFailure(err)
			}
		}
		edits, err := s.guardSourceEdits(current, planned)
		if err != nil {
			return nil, refactorFailure(err)
		}
		return edits, nil
	}
	return nil, nil
}

func (s *Server) renameSourceEdits(current *publishedAnalysis, change goEdit, planned []analysis.SourceEdit) ([]analysis.SourceEdit, error) {
	mapped, err := s.goSourceEdit(current, change)
	p := current.goProjection
	key := p.keys[change.URI]
	if filepath.Ext(key) == ".go" {
		return []analysis.SourceEdit{mapped}, err
	}
	if err != nil && p.indexes[change.URI] != nil {
		start, end, offsetErr := strictOffsets(p.indexes[change.URI], change.Edit.Range, source.UTF16)
		span := source.ByteSpan{Start: start, End: end}
		mapping, ok := p.maps[key].Generated(span)
		if offsetErr == nil && ok && mapping.Kind == source.Derived && mapping.Generated == span && len(mapping.Expansions) == 0 {
			mapped = analysis.SourceEdit{File: mapping.Source.File, Span: syntaxSpan(mapping.Source.Span.Start, mapping.Source.Span.End), NewText: change.Edit.NewText}
		} else {
			return nil, err
		}
	}
	var independent []analysis.SourceEdit
	sourcePlanned := false
	for _, original := range planned {
		if original.File == mapped.File && original.Span == mapped.Span {
			mapped = original
			err = nil // The source operation planned this exact token independently.
			sourcePlanned = true
			break
		}
	}
	if !sourcePlanned {
		// Every Go-originated source edit also obeys source rename rules.
		// Exact spelling alone does not prove semantics such as main/init
		// entry points are preserved; derived public names need this too.
		independent, err = current.queries.Rename(mapped.File, mapped.Span.Start, mapped.NewText)
		if err != nil {
			return nil, err
		}
		found := false
		for _, edit := range independent {
			if edit.File == mapped.File && edit.Span == mapped.Span {
				mapped, found = edit, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("Go rename has no independent source edit for its derived token")
		}
	}
	if !analysis.ValidRename(mapped.NewText) {
		return nil, fmt.Errorf("Go rename is not a valid source identifier")
	}
	expected, err := current.queries.GoNameAfterRename(mapped.File, mapped.Span.Start, mapped.NewText)
	if err != nil {
		return nil, err
	}
	if expected != change.Edit.NewText {
		return nil, fmt.Errorf("source declaration would generate Go name %s instead of %s", expected, change.Edit.NewText)
	}
	return append(independent, mapped), nil
}

func refactorFailure(err error) error {
	var rpc *RPCError
	if errors.As(err, &rpc) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return &RPCError{Code: -32803, Message: err.Error()}
}

func hasNativeSource(current *publishedAnalysis) bool {
	for key := range current.result.Sources {
		if filepath.Ext(key) == ".go" && current.editable[key] {
			return true
		}
	}
	return false
}

func uniqueLocations(locations []wireLocation) []wireLocation {
	seen := map[wireLocation]bool{}
	result := []wireLocation{}
	for _, location := range locations {
		if !seen[location] {
			seen[location] = true
			result = append(result, location)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].URI != result[j].URI {
			return result[i].URI < result[j].URI
		}
		if result[i].Range.Start != result[j].Range.Start {
			return positionBefore(result[i].Range.Start, result[j].Range.Start)
		}
		return positionBefore(result[i].Range.End, result[j].Range.End)
	})
	return result
}

func uniqueSourceEdits(edits []analysis.SourceEdit) ([]analysis.SourceEdit, error) {
	seen := map[source.Location]string{}
	result := []analysis.SourceEdit{}
	for _, edit := range edits {
		key := source.Location{File: edit.File, Span: source.ByteSpan{Start: int(edit.Span.Start), End: int(edit.Span.End)}}
		if previous, exists := seen[key]; exists {
			if previous != edit.NewText {
				return nil, fmt.Errorf("rename has conflicting replacements in %s", edit.File)
			}
			continue
		}
		seen[key] = edit.NewText
		result = append(result, edit)
	}
	return result, nil
}

func backendWorkspaceEdits(raw json.RawMessage) ([]goEdit, error) {
	var workspace struct {
		Changes         map[string][]wireTextEdit `json:"changes"`
		DocumentChanges []json.RawMessage         `json:"documentChanges"`
	}
	if err := json.Unmarshal(raw, &workspace); err != nil {
		return nil, err
	}
	var edits []goEdit
	for uri, changes := range workspace.Changes {
		for _, change := range changes {
			edits = append(edits, goEdit{URI: uri, Edit: change})
		}
	}
	for _, rawChange := range workspace.DocumentChanges {
		var change struct {
			Kind         string `json:"kind"`
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Edits []wireTextEdit `json:"edits"`
		}
		if err := json.Unmarshal(rawChange, &change); err != nil {
			return nil, err
		}
		if change.Kind != "" || change.TextDocument.URI == "" {
			return nil, fmt.Errorf("rename requires an unsupported file operation")
		}
		for _, edit := range change.Edits {
			edits = append(edits, goEdit{URI: change.TextDocument.URI, Edit: edit})
		}
	}
	return edits, nil
}

func (s *Server) goSourceEdit(current *publishedAnalysis, change goEdit) (analysis.SourceEdit, error) {
	if current == nil || current.goProjection == nil || current.result == nil {
		return analysis.SourceEdit{}, fmt.Errorf("backend edit has no current source projection")
	}
	p := current.goProjection
	key, index := p.keys[change.URI], p.indexes[change.URI]
	if key == "" || index == nil {
		return analysis.SourceEdit{}, fmt.Errorf("rename targets an unselected Go document")
	}
	start, end, err := strictOffsets(index, change.Edit.Range, source.UTF16)
	if err != nil {
		return analysis.SourceEdit{}, err
	}
	location := source.Location{File: key, Span: source.ByteSpan{Start: start, End: end}}
	if filepath.Ext(key) != ".go" {
		location, err = exactGoSource(p, key, location.Span)
		if err != nil {
			return analysis.SourceEdit{}, err
		}
	}
	original := current.indexes[location.File]
	if original == nil || current.names[location.File] == "" || current.result.Sources[location.File] == nil || location.Span.Start < 0 || location.Span.End < location.Span.Start || location.Span.End > original.Len() || original.Text()[location.Span.Start:location.Span.End] != index.Text()[start:end] {
		return analysis.SourceEdit{}, fmt.Errorf("rename mapping does not preserve the source token")
	}
	return analysis.SourceEdit{File: location.File, Span: syntaxSpan(location.Span.Start, location.Span.End), NewText: change.Edit.NewText}, nil
}
