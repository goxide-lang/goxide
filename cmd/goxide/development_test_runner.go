package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	goast "go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"io"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type developmentTest struct {
	Package string `json:"package"`
	Name    string `json:"name"`
	File    string `json:"file"`
	Line    int    `json:"line"`
}

type developmentPackage struct {
	ImportPath, Dir           string
	TestGoFiles, XTestGoFiles []string
}

func (v *developmentView) command(ctx context.Context, args ...string) *exec.Cmd {
	if v.Single != "" {
		args = append([]string{args[0], "-modfile=" + v.ModFile, "-overlay=" + v.Overlay}, args[1:]...)
	}
	command := exec.CommandContext(ctx, v.Config.Environment.GoTool, args...)
	command.Dir, command.Env = v.Root, localEnv(v.Config.Environment.ExecEnv())
	if v.Single != "" {
		command.Dir, command.Env = v.CommandRoot, singleFileEnvironment(v.Config.Environment.ExecEnv())
	}
	command.WaitDelay = time.Second
	return command
}

func (v *developmentView) packages(ctx context.Context) ([]developmentPackage, error) {
	args := append([]string{"list", "-json"}, developmentFlags(v.Tags)...)
	selection := "./..."
	if v.Single != "" {
		selection = v.Entry
	}
	args = append(args, selection)
	command := v.command(ctx, args...)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	data, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("test package discovery: %w: %s", err, stderr.String())
	}
	var packages []developmentPackage
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var item developmentPackage
		if err := decoder.Decode(&item); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		packages = append(packages, item)
	}
	return packages, nil
}

func (v *developmentView) Tests(ctx context.Context) ([]developmentTest, error) {
	packages, err := v.packages(ctx)
	if err != nil {
		return nil, err
	}
	return v.tests(packages)
}

func (v *developmentView) tests(packages []developmentPackage) ([]developmentTest, error) {
	tests := []developmentTest{}
	for _, item := range packages {
		for _, name := range append(item.TestGoFiles, item.XTestGoFiles...) {
			set := token.NewFileSet()
			filename := filepath.Join(item.Dir, name)
			if filename == v.Virtual {
				filename = v.SingleGo
			}
			file, err := parser.ParseFile(set, filename, nil, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				return nil, err
			}
			examples := make(map[string]bool)
			for _, example := range doc.Examples(file) {
				if example.Output != "" || example.EmptyOutput {
					examples["Example"+example.Name] = true
				}
			}
			for _, declaration := range file.Decls {
				function, ok := declaration.(*goast.FuncDecl)
				if !ok || function.Recv != nil || !developmentTestName(function.Name.Name) {
					continue
				}
				if strings.HasPrefix(function.Name.Name, "Example") && !examples[function.Name.Name] {
					continue
				}
				if function.Name.Name == "TestMain" && !developmentTestParameter(function, "T") {
					continue
				}
				position := set.Position(function.Name.Pos())
				tests = append(tests, developmentTest{item.ImportPath, function.Name.Name, position.Filename, position.Line})
			}
		}
	}
	sort.Slice(tests, func(i, j int) bool {
		if tests[i].Package != tests[j].Package {
			return tests[i].Package < tests[j].Package
		}
		return tests[i].Name < tests[j].Name
	})
	return tests, nil
}

func developmentTestName(name string) bool {
	for _, prefix := range []string{"Test", "Fuzz", "Example"} {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		rest := strings.TrimPrefix(name, prefix)
		if rest == "" {
			return true
		}
		r, _ := utf8.DecodeRuneInString(rest)
		return !unicode.IsLower(r)
	}
	return false
}

// Go treats TestMain(*testing.T) as an ordinary test, and TestMain(*testing.M)
// as the package harness. Match its syntactic distinction without type checking.
func developmentTestParameter(function *goast.FuncDecl, name string) bool {
	parameters := function.Type.Params.List
	if len(parameters) != 1 || len(parameters[0].Names) > 1 || function.Type.Results.NumFields() != 0 {
		return false
	}
	pointer, ok := parameters[0].Type.(*goast.StarExpr)
	if !ok {
		return false
	}
	switch item := pointer.X.(type) {
	case *goast.Ident:
		return item.Name == name
	case *goast.SelectorExpr:
		return item.Sel.Name == name
	}
	return false
}

type developmentLocation struct {
	File string `json:"file"`
	Line int    `json:"line"`
}

var testOutputLocation = regexp.MustCompile(`^\s*(.+\.(?:gox|go)):(\d+)(?::\d+)?:\s`)

func (v *developmentView) Test(ctx context.Context, selected, pattern string, out, stderr io.Writer) error {
	packages, err := v.packages(ctx)
	if err != nil {
		return err
	}
	tests, err := v.tests(packages)
	if err != nil {
		return err
	}
	if selected != "" {
		found := false
		for _, item := range packages {
			found = found || item.ImportPath == selected
		}
		if !found {
			return fmt.Errorf("test package %q is not part of the selected project", selected)
		}
	}
	if selected == "" {
		selected = "./..."
	}
	if v.Single != "" {
		selected = v.Entry
	}
	args := append([]string{"test", "-json", "-count=1"}, developmentFlags(v.Tags)...)
	if pattern != "" {
		args = append(args, "-run", pattern)
	}
	args = append(args, selected)
	command := v.command(ctx, args...)
	command.Stderr = stderr
	stream, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return err
	}
	encoder := json.NewEncoder(out)
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	var outputErr error
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			outputErr = err
			break
		}
		text, _ := event["Output"].(string)
		if location := testOutputLocation.FindStringSubmatch(text); location != nil {
			filename := location[1]
			if !filepath.IsAbs(filename) {
				packagePath, _ := event["Package"].(string)
				for _, test := range tests {
					if test.Package == packagePath && filepath.Base(test.File) == filename {
						filename = test.File
						break
					}
				}
				if !filepath.IsAbs(filename) {
					for key, original := range v.Names {
						if path.Dir(key) == packagePath && filepath.Base(original) == filename {
							filename = original
							break
						}
					}
				}
			}
			if filepath.IsAbs(filename) {
				line, _ := strconv.Atoi(location[2])
				event["Location"] = developmentLocation{filename, line}
			}
		}
		if err := encoder.Encode(event); err != nil {
			outputErr = err
			break
		}
	}
	if outputErr == nil {
		outputErr = scanner.Err()
	}
	if outputErr != nil {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if outputErr != nil {
		return outputErr
	}
	return waitErr
}
