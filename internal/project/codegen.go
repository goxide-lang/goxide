package project

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// CodegenConfig changes generated APIs without changing dependency identities.
type CodegenConfig struct {
	GoSugar *bool `toml:"go_sugar,omitempty"`
}

func ReadCodegen(root string) (CodegenConfig, error) {
	settings, _, err := readCodegen(root)
	return settings, err
}

func readCodegen(root string) (CodegenConfig, bool, error) {
	filename := filepath.Join(root, ManifestName)
	info, err := os.Lstat(filename)
	if os.IsNotExist(err) {
		return CodegenConfig{}, false, nil
	}
	if err != nil {
		return CodegenConfig{}, false, err
	}
	if !info.Mode().IsRegular() {
		return CodegenConfig{}, false, fmt.Errorf("%s: manifest must be a regular file", filename)
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return CodegenConfig{}, false, err
	}
	var settings struct {
		Codegen CodegenConfig `toml:"codegen"`
	}
	if err := toml.Unmarshal(data, &settings); err != nil {
		return CodegenConfig{}, false, manifestDecodeError(filename, err)
	}
	if err := validateNames(filename, data); err != nil {
		return CodegenConfig{}, false, err
	}
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil {
		return CodegenConfig{}, false, manifestDecodeError(filename, err)
	}
	_, codegen := document["codegen"]
	if len(document) == 1 && codegen {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
			return CodegenConfig{}, false, fmt.Errorf("%s: codegen-only configuration requires go.mod in the same directory", filename)
		}
		return settings.Codegen, false, nil
	}
	return settings.Codegen, true, nil
}

// ModuleCodegen stops at the nearest author or native Go module boundary.
func ModuleCodegen(directory string) (CodegenConfig, error) {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return CodegenConfig{}, err
	}
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return CodegenConfig{}, err
	}
	for {
		settings, author, err := readCodegen(directory)
		if err != nil || author {
			return settings, err
		}
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return settings, nil
		} else if !os.IsNotExist(err) {
			return CodegenConfig{}, err
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return CodegenConfig{}, nil
		}
		directory = parent
	}
}
