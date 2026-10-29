package lsp

import (
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/goxide-lang/goxide/internal/source"
)

type Document struct {
	URI     string
	Version int
	Text    string
	Index   *source.Index
}

type ContentChange struct {
	Range *source.Range `json:"range,omitempty"`
	Text  string        `json:"text"`
}

type DocumentSnapshot struct {
	Epoch     uint64
	Documents map[string]Document
}

// DocumentStore publishes value copies and immutable source indexes. Versions
// may advance without advancing the analysis epoch when the bytes are unchanged.
type DocumentStore struct {
	mu        sync.RWMutex
	documents map[string]Document
	epoch     uint64
	encoding  source.Encoding
}

func NewDocumentStore(encoding source.Encoding) *DocumentStore {
	if encoding == "" {
		encoding = source.UTF16
	}
	return &DocumentStore{documents: make(map[string]Document), encoding: encoding}
}

func (s *DocumentStore) Open(uri string, version int, text string) (uint64, error) {
	if _, err := URIToPath(uri); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.documents[uri]; exists {
		return s.epoch, fmt.Errorf("document is already open: %s", uri)
	}
	if s.documents == nil {
		s.documents = make(map[string]Document)
	}
	s.documents[uri] = Document{URI: uri, Version: version, Text: text, Index: source.New(text)}
	s.epoch++
	return s.epoch, nil
}

// Change validates the entire notification before committing it. Each range
// applies to the result of the preceding change in this same notification.
func (s *DocumentStore) Change(uri string, version int, changes []ContentChange) (epoch uint64, changed bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	document, exists := s.documents[uri]
	if !exists {
		return s.epoch, false, fmt.Errorf("document is not open: %s", uri)
	}
	if version <= document.Version {
		return s.epoch, false, fmt.Errorf("document version %d does not advance %d: %s", version, document.Version, uri)
	}
	text, index := document.Text, document.Index
	for i, change := range changes {
		next := change.Text
		if change.Range != nil {
			start, end, err := index.Offsets(*change.Range, s.encoding)
			if err != nil {
				return s.epoch, false, fmt.Errorf("document change %d: %w", i+1, err)
			}
			next = text[:start] + change.Text + text[end:]
		}
		if next != text {
			text, index = next, source.New(next)
		}
	}
	changed = text != document.Text
	if changed {
		s.epoch++
	} else {
		index = document.Index
	}
	document.Version, document.Text, document.Index = version, text, index
	s.documents[uri] = document
	return s.epoch, changed, nil
}

func (s *DocumentStore) Close(uri string) (epoch uint64, changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.documents[uri]; !exists {
		return s.epoch, false
	}
	delete(s.documents, uri)
	s.epoch++
	return s.epoch, true
}

func (s *DocumentStore) Get(uri string) (Document, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	document, exists := s.documents[uri]
	return document, exists
}

func (s *DocumentStore) Snapshot() DocumentSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := DocumentSnapshot{Epoch: s.epoch, Documents: make(map[string]Document, len(s.documents))}
	for uri, document := range s.documents {
		snapshot.Documents[uri] = document
	}
	return snapshot
}

// URIToPath decodes a standard absolute file URI without consulting the disk,
// resolving symlinks, or interpreting another operating system's path syntax.
func URIToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(u.Scheme, "file") || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Port() != "" || !strings.HasPrefix(u.Path, "/") {
		return "", fmt.Errorf("expected an absolute file URI: %q", uri)
	}
	path := u.Path
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		if runtime.GOOS != "windows" {
			return "", fmt.Errorf("non-local file URI is not a local filesystem path: %q", uri)
		}
		path = "//" + u.Host + path
	} else if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) || strings.ContainsRune(path, '\x00') {
		return "", fmt.Errorf("invalid absolute file URI path: %q", uri)
	}
	return path, nil
}

func PathToURI(path string) (string, error) {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, '\x00') {
		return "", fmt.Errorf("expected an absolute filesystem path: %q", path)
	}
	path = filepath.ToSlash(path)
	u := url.URL{Scheme: "file", Path: path}
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(path, "//") {
			u.Host, u.Path, _ = strings.Cut(path[2:], "/")
			u.Path = "/" + u.Path
		} else {
			u.Path = "/" + path
		}
	}
	return u.String(), nil
}
