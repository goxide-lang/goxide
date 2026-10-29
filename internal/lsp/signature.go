package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/source"
)

func (s *Server) signatureHelp(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position source.Position `json:"position"`
		Context  json.RawMessage `json:"context,omitempty"`
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
	if filepath.Ext(key) == ".go" {
		current, err = s.awaitComplete(ctx)
		if err != nil {
			return nil, err
		}
		key = current.keys[filepath.Clean(filename)]
		index = current.indexes[key]
		if index == nil {
			return nil, nil
		}
		offset, err := index.Offset(params.Position, s.encoding)
		if err != nil {
			return nil, invalidParams(err)
		}
		answer, err := s.goQuery(ctx, current, key, offset, "textDocument/signatureHelp", map[string]any{"context": params.Context})
		s.mu.Lock()
		valid := s.epoch == current.epoch && !s.shutdown
		s.mu.Unlock()
		if !valid {
			return nil, staleResult()
		}
		return answer, err
	}
	offset, err := index.Offset(params.Position, s.encoding)
	if err != nil {
		return nil, invalidParams(err)
	}
	help, err := current.queries.SignatureHelp(key, uint32(offset))
	var unavailable *analysis.QueryUnavailable
	if errors.As(err, &unavailable) {
		return nil, &RPCError{Code: CodeContentModified, Message: unavailable.Reason}
	}
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	valid := s.epoch == current.epoch && !s.shutdown
	s.mu.Unlock()
	if !valid {
		return nil, staleResult()
	}
	if help == nil {
		return nil, nil
	}
	parameters := []map[string]string{}
	for _, label := range help.Parameters {
		parameters = append(parameters, map[string]string{"label": label})
	}
	return map[string]any{"signatures": []any{map[string]any{"label": help.Label, "parameters": parameters, "activeParameter": help.ActiveParameter}}, "activeSignature": 0, "activeParameter": help.ActiveParameter}, nil
}
