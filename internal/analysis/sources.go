package analysis

import (
	"bytes"
	"context"
	"fmt"
	goast "go/ast"
	"go/build"
	goparser "go/parser"
	gotoken "go/token"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/expand"
	"github.com/goxide-lang/goxide/internal/paths"
	"github.com/goxide-lang/goxide/internal/resolve"
	"github.com/goxide-lang/goxide/internal/rootconfig"
	syntax "github.com/goxide-lang/goxide/internal/syntax/ast"
	"github.com/goxide-lang/goxide/internal/syntax/parser"
	"github.com/goxide-lang/goxide/internal/syntax/printer"
)

// Source retains the fixed bytes and, when requested, the unconfigured syntax
// of one input. All fields belong to the published analysis and are read-only.
// GoSyntax always contains the original Go body, not its declaration projection.
type Source struct {
	Content     []byte
	Syntax      *syntax.File
	Diagnostics []parser.Diagnostic
	GoSyntax    *goast.File
	GoTokens    *gotoken.FileSet
	parsed      bool
	err         error
}

type configuredSource struct {
	target bool
	clean  *syntax.File
	file   *syntax.File
	used   bool
	err    error
	text   []byte
}

type sourceSet struct {
	files             map[string]*Source
	names             map[string]string
	keys              map[string]string
	modules           map[string][]string
	roots             map[string]paths.Roots
	configured        map[string]configuredSource
	previous          map[string]*Source
	retryable, replay bool
	preserve          bool
	snapshotPackages  bool
	overlays          map[string][]byte
}

func captureSources(input Input) (*sourceSet, error) {
	s := &sourceSet{files: map[string]*Source{}, names: map[string]string{}, keys: map[string]string{}, modules: map[string][]string{}, roots: map[string]paths.Roots{}, configured: map[string]configuredSource{}, preserve: input.Options.PreserveSyntax, overlays: input.Overlays}
	if input.Options.Previous != nil {
		s.previous = input.Options.Previous.Sources
	}
	for _, module := range input.Modules {
		if len(module.Crates) != 0 {
			s.retryable = true
		}
		if module.OnSourcePackages != nil {
			s.snapshotPackages = true
		}
	}
	if input.Files != nil {
		for _, module := range input.Modules {
			s.roots[module.Path] = module.Roots
		}
		selected := map[string][]byte{}
		for key, data := range input.Files {
			selected[key] = data
		}
		for key, data := range input.Overlays {
			if data == nil {
				delete(selected, key)
			} else {
				selected[key] = data
			}
		}
		var keys []string
		for key := range selected {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			var owner *Module
			for i := range input.Modules {
				module := &input.Modules[i]
				if strings.HasPrefix(key, module.Path+"/") && (owner == nil || len(module.Path) > len(owner.Path)) {
					owner = module
				}
			}
			if owner == nil {
				return s, fmt.Errorf("source %s is outside the selected modules", key)
			}
			relative := strings.TrimPrefix(key, owner.Path+"/")
			if path.Clean(key) != key || !filepath.IsLocal(filepath.FromSlash(relative)) {
				return s, fmt.Errorf("invalid source path %s", key)
			}
			name := input.DiskNames[key]
			if name == "" {
				name = filepath.Join(owner.Dir, filepath.FromSlash(strings.TrimPrefix(key, owner.Path+"/")))
			}
			s.add(key, selected[key])
			s.names[key], s.keys[name] = name, key
			s.modules[owner.Path] = append(s.modules[owner.Path], name)
		}
		return s, nil
	}
	for moduleIndex, module := range input.Modules {
		roots := module.Roots
		if roots == nil {
			var err error
			roots, err = rootconfig.Load(module.Dir)
			if err != nil {
				return s, err
			}
		}
		s.roots[module.Path] = roots
		seen := map[string]bool{}
		managed := moduleIndex == 0 || module.RootPackage != ""
		for _, ext := range []string{".gox", ".go"} {
			names, err := moduleFiles(module.Dir, ext, managed)
			if err != nil {
				return s, err
			}
			if ext == ".gox" && len(names) != 0 {
				managed = true
			}
			for _, name := range names {
				seen[name] = true
			}
		}
		if module.NativePackage != nil {
			for key := range module.NativePackage.DisplayNames {
				relative, ok := strings.CutPrefix(key, module.Path+"/")
				if !ok || !filepath.IsLocal(filepath.FromSlash(relative)) {
					return s, fmt.Errorf("native source %s is outside module %s", key, module.Path)
				}
				seen[filepath.Join(module.Dir, filepath.FromSlash(relative))] = true
			}
		}
		var names []string
		for name := range seen {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			relative, err := filepath.Rel(module.Dir, name)
			if err != nil {
				return s, err
			}
			key := path.Join(module.Path, filepath.ToSlash(relative))
			data, ok := input.Overlays[key]
			if !ok {
				data, err = os.ReadFile(name)
				if err != nil {
					return s, err
				}
			}
			s.add(key, data)
			s.names[key], s.keys[name] = name, key
			s.modules[module.Path] = append(s.modules[module.Path], name)
		}
	}
	return s, nil
}

// LoadInput fixes the module inventory, roots and source bytes for an analysis.
// Callers can create later snapshots by replacing Overlays without rereading
// unchanged input files. An overlay with nil bytes removes the selected file.
func LoadInput(ctx context.Context, input Input) (Input, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return Input{}, err
		}
	}
	sources, err := captureSources(input)
	if err != nil {
		return Input{}, err
	}
	if input.Modules == nil {
		return input, nil
	}
	input.Files = map[string][]byte{}
	input.DiskNames = map[string]string{}
	for key, source := range sources.files {
		input.Files[key] = source.Content
		input.DiskNames[key] = sources.names[key]
	}
	input.Modules = append([]Module(nil), input.Modules...)
	for i := range input.Modules {
		input.Modules[i].Roots = sources.roots[input.Modules[i].Path]
	}
	input.Overlays = nil
	return input, nil
}

func (s *sourceSet) add(key string, data []byte) *Source {
	if source := s.files[key]; source != nil {
		return source
	}
	if overlay, ok := s.overlays[key]; ok {
		data = overlay
	}
	if previous := s.previous[key]; previous != nil && previous.parsed && (previous.Syntax != nil || previous.GoSyntax != nil) && bytes.Equal(previous.Content, data) {
		s.files[key] = previous
		return previous
	}
	if s.preserve {
		data = bytes.Clone(data)
	}
	source := &Source{Content: data}
	s.files[key] = source
	return source
}

func (s *sourceSet) parse(key string) *Source {
	source := s.files[key]
	if source.parsed {
		return source
	}
	source.parsed = true
	if path.Ext(key) == ".go" {
		source.GoTokens = gotoken.NewFileSet()
		source.GoSyntax, source.err = goparser.ParseFile(source.GoTokens, key, source.Content, goparser.SkipObjectResolution)
	} else {
		source.Syntax, source.Diagnostics = parser.ParseFile(key, source.Content)
		for _, diagnostic := range source.Diagnostics {
			if diagnostic.Severity == parser.Error {
				source.err = &Diagnostic{File: key, Source: source.Syntax, Span: diagnostic.Span, Message: diagnostic.Message, Origin: resolve.Origin{File: key, Span: diagnostic.Span}}
				break
			}
		}
	}
	return source
}

func (s *sourceSet) working(key string, configuration config.Config) (*syntax.File, bool, error) {
	if configured, ok := s.configured[key]; ok {
		if configured.target {
			configuration.Reads.RecordTarget(configured.file)
		}
		return configured.file, configured.used, configured.err
	}
	source := s.parse(key)
	file := source.Syntax
	if source.err != nil {
		return file, false, source.err
	}
	if s.preserve || s.previous[key] == source {
		file = syntax.CloneFile(file)
	} else {
		source.Syntax = nil
	}
	used, err := expand.Configure(file, configuration)
	// Only graphs with item expansion need clean trees for a discovery retry.
	// Ordinary CLI inputs transfer their syntax directly into the working tree.
	if s.retryable && !s.preserve && !s.replay && hasItemExpansion(file) {
		s.replay = true
		for key, configured := range s.configured {
			configured.clean = syntax.CloneFile(configured.file)
			s.configured[key] = configured
		}
	}
	configured := configuredSource{file: file, used: used, err: err}
	for _, targetFile := range configuration.Reads.TargetFiles() {
		if targetFile == file {
			configured.target = true
			break
		}
	}
	if s.replay && !s.preserve {
		configured.clean = syntax.CloneFile(file)
	}
	if s.snapshotPackages && err == nil {
		configured.text = []byte(printer.Source(file))
	}
	s.configured[key] = configured
	return file, used, err
}

func (s *sourceSet) reset() {
	if s.preserve {
		s.configured = map[string]configuredSource{}
		return
	}
	for key, configured := range s.configured {
		configured.file = syntax.CloneFile(configured.clean)
		s.configured[key] = configured
	}
}
func hasItemExpansion(file *syntax.File) bool {
	found := false
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.MacroItem, *syntax.Attr:
			found = true
		}
		return !found
	})
	return found
}
func (s *sourceSet) moduleFiles(module Module, extension string) []string {
	var names []string
	for _, name := range s.modules[module.Path] {
		if filepath.Ext(name) == extension {
			names = append(names, name)
		}
	}
	return names
}
func (s *sourceSet) read(name string) []byte { return s.files[s.keys[name]].Content }
func (s *sourceSet) matches(name string, context build.Context) (bool, error) {
	data := s.read(name)
	context.OpenFile = func(string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }
	base := filepath.Base(name)
	if strings.HasSuffix(base, ".gox") {
		base = strings.TrimSuffix(base, ".gox") + ".go"
	}
	return context.MatchFile(filepath.Dir(name), base)
}
