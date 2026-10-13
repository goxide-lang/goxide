package lower

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
	"strings"
)

type moduleGoImporter struct {
	root    string
	exports map[string]string
	owners  map[string]string
	gc      types.Importer
}

func sourceModuleImporter(root string) types.Importer {
	i := &moduleGoImporter{root: root, exports: map[string]string{}, owners: map[string]string{}}
	i.gc = importer.ForCompiler(token.NewFileSet(), "gc", i.lookup)
	return i
}

func (i *moduleGoImporter) Import(path string) (*types.Package, error) { return i.gc.Import(path) }

func (i *moduleGoImporter) PackageOrigin(path string) (string, bool, error) {
	if _, ok := i.owners[path]; !ok {
		if err := i.load(path); err != nil {
			return "", false, err
		}
	}
	owner := i.owners[path]
	return owner, owner == "", nil
}

func (i *moduleGoImporter) lookup(path string) (io.ReadCloser, error) {
	if filename := i.exports[path]; filename != "" {
		return os.Open(filename)
	}
	if err := i.load(path); err != nil {
		return nil, err
	}
	filename := i.exports[path]
	if filename == "" {
		return nil, fmt.Errorf("Go package %s has no export data", path)
	}
	return os.Open(filename)
}

func (i *moduleGoImporter) load(path string) error {
	command := exec.Command("go", "list", "-mod=readonly", "-export", "-deps", "-f", "{{.ImportPath}}\t{{.Export}}\t{{if .Module}}{{.Module.Path}}{{end}}", path)
	command.Dir = i.root
	command.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("load Go package %s: %w\n%s", path, err, output)
	}
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) == 3 {
			i.exports[fields[0]], i.owners[fields[0]] = fields[1], fields[2]
		}
	}
	return nil
}
