package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/goxide-lang/goxide/internal/analysis"
	"github.com/goxide-lang/goxide/internal/compile"
	"github.com/goxide-lang/goxide/internal/config"
	"github.com/goxide-lang/goxide/internal/lower"
	"github.com/goxide-lang/goxide/internal/project"
)

type developmentView struct {
	Root, Entry, SourceRoot                                  string
	Config                                                   config.Config
	Tags                                                     []string
	Names                                                    map[string]string
	Single, SingleGo, Virtual, ModFile, Overlay, CommandRoot string
	close                                                    func() error
}

func (v *developmentView) Close() error { return v.close() }

// Development owns one private executable projection for the whole invocation.
// Compiler publication and the editor's live overlays keep their own lifetimes.
func prepareDevelopment(ctx context.Context, req developmentRequest, stderr io.Writer) (_ *developmentView, err error) {
	input, err := filepath.Abs(req.Input)
	if err != nil {
		return nil, err
	}
	input, err = filepath.EvalSymlinks(input)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(input)
	if err != nil {
		return nil, err
	}
	directory := input
	if !info.IsDir() {
		directory = filepath.Dir(input)
	}
	manifestRoot := ""
	if info.IsDir() || filepath.Base(input) == project.ManifestName {
		manifestRoot, err = generationProject(directory)
		if err != nil {
			return nil, err
		}
	}
	host, err := newWASIHost(ctx)
	if err != nil {
		return nil, err
	}
	defer host.Close()
	session, err := host.NewSession(ctx)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	options := req.Compiler
	options.RunProcMacro = session.Run
	var prepared *compile.Project
	var native *compile.NativeProject
	view := &developmentView{SourceRoot: directory, Tags: options.Tags}
	if manifestRoot != "" {
		manifest, err := project.Read(manifestRoot)
		if err != nil {
			return nil, err
		}
		if options.Bin == "" && options.Example == "" && !options.Lib {
			options.Lib = manifest.Lib != nil && len(manifest.Bins) == 0 && len(manifest.Examples) == 0
		}
		if options.Lib && manifest.Lib != nil && manifest.Lib.ProcMacro {
			return nil, fmt.Errorf("proc-macro targets execute as WASI; select an ordinary consumer or example")
		}
		prepared, err = compile.Prepare(ctx, manifestRoot, options)
		if err != nil {
			return nil, err
		}
		view.Root, view.Entry, view.SourceRoot, view.close = prepared.GoRoot(), prepared.Entry(), manifestRoot, prepared.Close
	} else {
		if filepath.Base(input) == project.ManifestName {
			input = directory
		}
		native, err = compile.PrepareNative(ctx, input, compile.NativeOptions{Options: options, Automatic: true, Private: true})
		if err != nil {
			return nil, err
		}
		view.Root, view.SourceRoot, view.close = native.GoRoot(), native.Root(), native.Close
		view.Single, _, _ = native.SingleFile()
		if view.Single != "" {
			view.ModFile = native.GoModFile()
		}
		relative, err := filepath.Rel(native.Root(), directory)
		if err != nil {
			native.Close()
			return nil, err
		}
		view.Entry = "."
		if relative != "." {
			view.Entry = "./" + filepath.ToSlash(relative)
		}
	}
	defer func() {
		if err != nil {
			view.Close()
		}
	}()
	for {
		var snapshot *compile.Snapshot
		if native != nil {
			snapshot, err = native.Snapshot(ctx, nil)
		} else {
			snapshot, err = prepared.Snapshot(ctx, nil)
		}
		if err != nil {
			return nil, err
		}
		snapshot.BindProcMacros(session.Run)
		result, analyzeErr := analysis.Analyze(snapshot.Context, snapshot.Input)
		if analyzeErr != nil {
			snapshot.Close()
			if native != nil {
				activated, activateErr := native.Activate(ctx, analyzeErr, session.Run)
				if activateErr != nil {
					return nil, activateErr
				}
				if activated {
					view.Root = native.GoRoot()
					continue
				}
			}
			if result != nil {
				analyzeErr = analysis.FormatSourceDiagnostic(analyzeErr, result.Files, snapshot.SourceNames, result.Expanded)
			}
			return nil, analyzeErr
		}
		view.Config = snapshot.Input.Modules[0].Config
		view.Names = snapshot.SourceNames
		if view.Config.Environment.Target != view.Config.Environment.Host {
			snapshot.Close()
			return nil, fmt.Errorf("editor execution requires the host target %s; selected %s", view.Config.Environment.Host, view.Config.Environment.Target)
		}
		output, generateErr := lower.Generate(snapshot.Context, result, lower.GenerationOptions{LineDirectives: true})
		if generateErr == nil {
			for name, data := range output {
				output[name] = developmentLines(name, data, snapshot.SourceNames)
			}
			generateErr = lower.Materialize(snapshot.Context, result, output, lower.MaterializeOptions{RemoveSources: true})
		}
		if generateErr == nil {
			// Native Go participates in the same source-addressed stack traces.
			for key, original := range snapshot.SourceNames {
				if filepath.Ext(key) != ".go" {
					continue
				}
				generated := result.DiskNames[key]
				if generated == "" || generated == original {
					continue
				}
				data, readErr := os.ReadFile(generated)
				if readErr != nil {
					generateErr = readErr
					break
				}
				data = append([]byte("//line "+filepath.ToSlash(original)+":1\n"), data...)
				if writeErr := os.WriteFile(generated, data, 0600); writeErr != nil {
					generateErr = writeErr
					break
				}
			}
		}
		if generateErr == nil && view.Single != "" {
			for key, name := range snapshot.SourceNames {
				if name != view.Single {
					continue
				}
				view.SingleGo = result.DiskOutputs[key]
				if view.SingleGo == "" {
					view.SingleGo = result.DiskNames[key]
				}
				break
			}
			generateErr = view.singleFileProjection(native)
		}
		snapshot.Close()
		if generateErr != nil {
			return nil, generateErr
		}
		return view, nil
	}
}

// A named-file Go overlay preserves embed/cgo lookup in the author's package
// without admitting sibling source files or writing generated files there.
func (v *developmentView) singleFileProjection(native *compile.NativeProject) error {
	if v.SingleGo == "" {
		return fmt.Errorf("selected file has no executable Go projection")
	}
	extension := ".go"
	if strings.HasSuffix(v.Single, "_test.gox") || strings.HasSuffix(v.Single, "_test.go") {
		extension = "_test.go"
	}
	v.Virtual = filepath.Join(filepath.Dir(v.Single), "goxide_debug_"+filepath.Base(filepath.Dir(v.ModFile))+extension)
	if _, err := os.Lstat(v.Virtual); err == nil {
		return fmt.Errorf("private file projection would mask existing source %s", v.Virtual)
	} else if !os.IsNotExist(err) {
		return err
	}
	v.Overlay = filepath.Join(filepath.Dir(v.ModFile), "development-overlay.json")
	data, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{v.Virtual: v.SingleGo}})
	if err != nil {
		return err
	}
	if err := os.WriteFile(v.Overlay, data, 0600); err != nil {
		return err
	}
	v.CommandRoot = filepath.Dir(v.Single)
	if _, module, _ := native.SingleFile(); module == "" {
		v.CommandRoot = v.Root
	}
	v.Entry = v.Virtual
	return nil
}

// Lowering keeps ordinary generated Go relocatable by emitting basenames.
// Executable private views point those directives at the author's real files.
func developmentLines(key string, data []byte, names map[string]string) []byte {
	var output bytes.Buffer
	for _, line := range bytes.SplitAfter(data, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("//line ")) {
			text := string(line[len("//line "):])
			colon := strings.LastIndexByte(strings.TrimSpace(text), ':')
			if colon >= 0 {
				name := text[:colon]
				original := names[name]
				if original == "" {
					original = names[path.Join(path.Dir(key), name)]
				}
				if original != "" {
					line = []byte("//line " + filepath.ToSlash(original) + text[colon:])
				}
			}
		}
		output.Write(line)
	}
	return output.Bytes()
}
