package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	sourceformat "github.com/goxide-lang/goxide/internal/format"
)

func (s *Server) formatting(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Options struct {
			TabSize      int  `json:"tabSize"`
			InsertSpaces bool `json:"insertSpaces"`
		} `json:"options"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, invalidParams(err)
	}
	filename, err := URIToPath(params.TextDocument.URI)
	if err != nil {
		return nil, invalidParams(err)
	}
	if params.Options.TabSize < 1 || params.Options.TabSize > 32 {
		return nil, invalidParams(fmt.Errorf("formatting tab size must be between 1 and 32"))
	}
	if filepath.Ext(filename) != ".gox" {
		return nil, &RPCError{Code: -32803, Message: "formatting requires an authored Goxide document"}
	}
	s.mu.Lock()
	if sequence := ReceiveSequence(ctx); sequence != 0 && sequence < s.sequence {
		s.mu.Unlock()
		return nil, staleResult()
	}
	document, open := s.documents.Get(params.TextDocument.URI)
	encoding := s.encoding
	s.mu.Unlock()
	if !open {
		return nil, &RPCError{Code: -32803, Message: "formatting requires an open document"}
	}
	formatted, err := sourceformat.Source(ctx, filename, document.Text, sourceformat.Options{TabSize: params.Options.TabSize, InsertSpaces: params.Options.InsertSpaces})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, &RPCError{Code: -32803, Message: err.Error()}
	}
	span, err := document.Index.Range(0, len(document.Text), encoding)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	current, open := s.documents.Get(document.URI)
	sequence := ReceiveSequence(ctx)
	valid := open && current.Version == document.Version && current.Text == document.Text && !s.shutdown && (sequence == 0 || sequence >= s.sequence)
	s.mu.Unlock()
	if !valid {
		return nil, staleResult()
	}
	edits := []wireTextEdit{}
	if formatted != document.Text {
		edits = append(edits, wireTextEdit{Range: span, NewText: formatted})
	}
	return edits, nil
}
