package lsp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/source"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
)

func (s *Server) codingActions(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Range   source.Range `json:"range"`
		Context struct {
			Only []string `json:"only"`
		} `json:"context"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	filename, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, invalidParams(err)
	}
	actions := []any{}
	if filepath.Ext(filename) != ".gox" {
		return actions, nil
	}
	current, err := s.await(ctx)
	if err != nil {
		return nil, err
	}
	key := current.keys[filepath.Clean(filename)]
	index := current.indexes[key]
	if key == "" || index == nil || !current.editable[key] {
		return actions, nil
	}
	if _, open := current.documents.Documents[params.TextDocument.URI]; !open {
		return nil, &RPCError{Code: -32803, Message: "code actions require an open document"}
	}
	start, end, err := strictOffsets(index, params.Range, s.encoding)
	if err != nil {
		return nil, invalidParams(err)
	}
	sourceActions, err := current.queries.CodeActions(key, syntax.Span{Start: uint32(start), End: uint32(end)})
	if err != nil {
		return nil, err
	}
	for _, action := range sourceActions {
		requested := len(params.Context.Only) == 0
		for _, kind := range params.Context.Only {
			requested = requested || action.Kind == kind || strings.HasPrefix(action.Kind, kind+".")
		}
		if !requested {
			continue
		}
		edits, err := s.guardSourceEdits(current, action.Edits)
		if err != nil {
			if isStaleEdit(err) {
				return nil, err
			}
			continue
		}
		if err := s.validateCandidate(ctx, current, action.Edits, false, func(_ context.Context, candidate *analysis.Result) error {
			return current.queries.ValidateCodeAction(candidate, action)
		}); err != nil {
			if isStaleEdit(err) || ctx.Err() != nil {
				return nil, err
			}
			continue
		}
		actions = append(actions, map[string]any{"title": action.Title, "kind": action.Kind, "edit": edits})
	}
	s.mu.Lock()
	valid := s.epoch == current.epoch && !s.shutdown
	s.mu.Unlock()
	if !valid {
		return nil, staleResult()
	}
	return actions, nil
}
