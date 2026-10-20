package lower

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

type moduleGoImporter struct {
	root, tool  string
	context     context.Context
	env         []string
	tags        []string
	race        bool
	exports     map[string]string
	owners      map[string]string
	standard    map[string]bool
	gc          types.Importer
	indices     map[string][]string
	indexErrors map[string]error
}

func (i *moduleGoImporter) PackageIndex(source, module string) ([]string, error) {
	key := source + "\x00" + module
	if i.indices == nil {
		i.indices, i.indexErrors = map[string][]string{}, map[string]error{}
	}
	if result, ok := i.indices[key]; ok {
		return result, i.indexErrors[key]
	}
	pattern := module + "/..."
	if source == "toolchain" {
		pattern = "std"
	}
	args := []string{"list", "-mod=readonly", "-e", "-find", "-json"}
	if len(i.tags) != 0 {
		args = append(args, "-tags="+strings.Join(i.tags, ","))
	}
	if i.race {
		args = append(args, "-race")
	}
	args = append(args, pattern)
	command := exec.CommandContext(i.context, i.tool, args...)
	command.Dir, command.Env = i.root, i.env
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		err = fmt.Errorf("index Go packages %s: %w\n%s", pattern, err, stderr.String())
		i.indices[key], i.indexErrors[key] = nil, err
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	var result []string
	for decoder.More() {
		var pkg struct {
			ImportPath, Name string
			Standard         bool
			Module           *struct{ Path string }
		}
		if err := decoder.Decode(&pkg); err != nil {
			i.indices[key], i.indexErrors[key] = nil, err
			return nil, err
		}
		owner := ""
		if pkg.Module != nil {
			owner = pkg.Module.Path
		}
		if pkg.Name != "" && ((source == "toolchain" && pkg.Standard) || (source != "toolchain" && !pkg.Standard && owner == module)) {
			result = append(result, pkg.ImportPath)
			i.owners[pkg.ImportPath], i.standard[pkg.ImportPath] = owner, pkg.Standard
		}
	}
	if source == "toolchain" {
		result = append(result, "unsafe")
		i.owners["unsafe"], i.standard["unsafe"] = "", true
	}
	sort.Strings(result)
	i.indices[key] = result
	return result, nil
}

func sourceModuleImporter(module Module) *moduleGoImporter {
	ctx := module.Context
	if ctx == nil {
		ctx = context.Background()
	}
	environment := module.Config.Environment
	i := &moduleGoImporter{root: module.Dir, tool: environment.GoTool, context: ctx, env: environment.ExecEnv(), tags: append([]string(nil), module.Tags...), race: module.Race, exports: map[string]string{}, owners: map[string]string{}, standard: map[string]bool{}}
	i.gc = importer.ForCompiler(token.NewFileSet(), "gc", i.lookup)
	return i
}

func (i *moduleGoImporter) Import(path string) (*types.Package, error) { return i.gc.Import(path) }

func (i *moduleGoImporter) PackageOrigin(path string) (string, bool, error) {
	if _, ok := i.owners[path]; !ok {
		if err := i.load(path, false); err != nil {
			return "", false, err
		}
	}
	owner := i.owners[path]
	return owner, i.standard[path], nil
}

func (i *moduleGoImporter) lookup(path string) (io.ReadCloser, error) {
	if filename := i.exports[path]; filename != "" {
		return os.Open(filename)
	}
	if err := i.load(path, true); err != nil {
		return nil, err
	}
	filename := i.exports[path]
	if filename == "" {
		return nil, fmt.Errorf("Go package %s has no export data", path)
	}
	return os.Open(filename)
}

func (i *moduleGoImporter) load(path string, exports bool) error {
	args := []string{"list", "-mod=readonly"}
	if len(i.tags) != 0 {
		args = append(args, "-tags="+strings.Join(i.tags, ","))
	}
	if i.race {
		args = append(args, "-race")
	}
	if exports {
		args = append(args, "-export", "-deps")
	} else {
		args = append(args, "-find")
	}
	args = append(args, "-f", "{{.ImportPath}}\t{{.Export}}\t{{if .Module}}{{.Module.Path}}{{end}}\t{{.Standard}}", path)
	command := exec.CommandContext(i.context, i.tool, args...)
	command.Dir = i.root
	command.Env = i.env
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("load Go package %s: %w\n%s", path, err, output)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) == 4 {
			i.exports[fields[0]], i.owners[fields[0]] = fields[1], fields[2]
			i.standard[fields[0]] = fields[3] == "true"
		}
	}
	return nil
}
