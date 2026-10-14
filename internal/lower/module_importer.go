package lower

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type moduleGoImporter struct {
	root, tool string
	exports    map[string]string
	owners     map[string]string
	standard   map[string]bool
	gc         types.Importer
}

func sourceModuleImporter(root, tool string) *moduleGoImporter {
	if tool == "" {
		tool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	i := &moduleGoImporter{root: root, tool: tool, exports: map[string]string{}, owners: map[string]string{}, standard: map[string]bool{}}
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
	if exports {
		args = append(args, "-export", "-deps")
	} else {
		args = append(args, "-find")
	}
	args = append(args, "-f", "{{.ImportPath}}\t{{.Export}}\t{{if .Module}}{{.Module.Path}}{{end}}\t{{.Standard}}", path)
	command := exec.Command(i.tool, args...)
	command.Dir = i.root
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=")
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
