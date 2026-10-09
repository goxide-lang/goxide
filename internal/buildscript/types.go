// Package buildscript executes explicitly trusted, unconfined host generators.
// Declared-input caching is an optimization, not an OS sandbox or hermeticity guarantee.
package buildscript

import (
	"github.com/hxsf/goxide/internal/buildconfig"
	"github.com/hxsf/goxide/internal/paths"
)

// Config is the shared manifest contract; this alias preserves runner callers.
type Config = buildconfig.Config

type Options struct {
	ProjectRoot, PackageRoot, Module, SourceID, GoTool, CompilerID, ArtifactRoot, Boundary string
	Root                                                                                   bool
	Config                                                                                 Config
	ToolchainRoots                                                                         paths.Roots
	Features, Tags                                                                         []string
	Race                                                                                   bool
}
type Request struct {
	Version    int               `json:"version"`
	SourceDir  string            `json:"source_dir"`
	OutDir     string            `json:"out_dir"`
	HostOS     string            `json:"host_os"`
	HostArch   string            `json:"host_arch"`
	TargetOS   string            `json:"target_os"`
	TargetArch string            `json:"target_arch"`
	Features   []string          `json:"features"`
	Tools      map[string]string `json:"tools"`
	Env        map[string]string `json:"env"`
}
type Subject struct{ ID, Description string }
type Artifact struct {
	Files    map[string][]byte
	Digest   string
	CacheHit bool
}
type Plan struct {
	ID, Subject, RootIdentity string
	Subjects                  []Subject
	options                   Options
	script, inputs            map[string][]byte
	helpers                   map[string]map[string][]byte
	tools                     map[string][]byte
	env                       map[string]string
	targetOS, targetArch      string
	inputDirs                 []string
}

// Close is currently a no-op: Prepare freezes bytes in memory and Execute owns its scratch directory.
func (p *Plan) Close() error { return nil }
