package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goxide-lang/goxide/internal/config"
)

// scripts/build.go stamps the compiler's published require pins.
var stdSuggestedVersion, serdeSuggestedVersion string

type missingGenerationDependencies struct {
	Root    string
	Modules []string
	Config  config.Config
	Cause   error
}

func (e *missingGenerationDependencies) Error() string {
	message := "generation needs dependencies absent from go.mod: " + strings.Join(e.Modules, ", ")
	if e.Cause != nil {
		message += "\n" + e.Cause.Error()
	}
	return message
}

func (e *missingGenerationDependencies) Unwrap() error { return e.Cause }

func executeCompile(req CompileRequest, out, stderr io.Writer) error {
	if req.PromptInput != nil {
		req.PromptInput = bufio.NewReader(req.PromptInput)
	}
	attempted := map[string]bool{}
	for {
		err := executeCompileOnce(req, out, stderr)
		var missing *missingGenerationDependencies
		if req.Mode != "transpile" || !errors.As(err, &missing) {
			return err
		}
		for _, module := range missing.Modules {
			key := missing.Root + "\x00" + module
			if attempted[key] {
				return fmt.Errorf("dependency remains unavailable after adding it: %w", err)
			}
			attempted[key] = true
		}
		if err := addGenerationDependencies(req, missing, stderr); err != nil {
			return err
		}
	}
}

func suggestedGenerationVersion(module string) string {
	switch module {
	case "github.com/goxide-lang/std":
		return stdSuggestedVersion
	case "github.com/goxide-lang/serde":
		return serdeSuggestedVersion
	}
	return ""
}

func addGenerationDependencies(req CompileRequest, missing *missingGenerationDependencies, stderr io.Writer) error {
	modules := slices.Clone(missing.Modules)
	slices.Sort(modules)
	modules = slices.Compact(modules)
	var arguments []string
	versionsAvailable := true
	for _, module := range modules {
		version := suggestedGenerationVersion(module)
		if version == "" {
			versionsAvailable = false
			version = "VERSION"
		}
		arguments = append(arguments, module+"@"+version)
	}
	commandText := "go get " + strings.Join(arguments, " ")
	diagnostic := func() error {
		message := missing.Error() + "\nadd it with: " + commandText
		if !versionsAvailable {
			message += "\nthis compiler has no recommended dependency versions; choose a published version or rebuild with scripts/build.go"
		}
		return errors.New(message)
	}
	if !req.Interactive || req.PromptInput == nil || !versionsAvailable {
		return diagnostic()
	}
	var original [2]generationModuleFile
	for i, name := range []string{"go.mod", "go.sum"} {
		file, err := readGenerationModuleFile(filepath.Join(missing.Root, name))
		if err != nil {
			return err
		}
		original[i] = file
	}
	if _, err := fmt.Fprintf(stderr, "%s\nRun `%s` in %s and update go.mod/go.sum? [y/N] ", missing.Error(), commandText, missing.Root); err != nil {
		return err
	}
	reader, ok := req.PromptInput.(*bufio.Reader)
	if !ok {
		reader = bufio.NewReader(req.PromptInput)
	}
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return err
	}
	if err == io.EOF || !strings.EqualFold(strings.TrimSpace(answer), "y") && !strings.EqualFold(strings.TrimSpace(answer), "yes") {
		return diagnostic()
	}
	directory, err := os.MkdirTemp("", "goxide-gen-dependencies-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	modFile := filepath.Join(directory, "consumer.mod")
	for i, name := range []string{"consumer.mod", "consumer.sum"} {
		if original[i].exists {
			if err := os.WriteFile(filepath.Join(directory, name), original[i].data, 0600); err != nil {
				return err
			}
		}
	}
	command := exec.Command(missing.Config.Environment.GoTool, append([]string{"get", "-modfile=" + modFile}, arguments...)...)
	command.Dir, command.Env = missing.Root, missing.Config.Environment.ExecEnv()
	command.Stdout, command.Stderr = stderr, stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("add generation dependencies (go.mod/go.sum unchanged): %w", err)
	}
	var updated [2]generationModuleFile
	for i, name := range []string{"consumer.mod", "consumer.sum"} {
		updated[i], err = readGenerationModuleFile(filepath.Join(directory, name))
		if err != nil {
			return err
		}
	}
	return publishGenerationModuleFiles(original, updated)
}

type generationModuleFile struct {
	path   string
	data   []byte
	mode   os.FileMode
	exists bool
}

func readGenerationModuleFile(path string) (generationModuleFile, error) {
	file := generationModuleFile{path: path, mode: 0644}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return file, nil
	}
	if err != nil {
		return file, err
	}
	if !info.Mode().IsRegular() {
		return file, fmt.Errorf("module configuration is not a regular file: %s", path)
	}
	file.exists, file.mode = true, info.Mode().Perm()
	file.data, err = os.ReadFile(path)
	return file, err
}

func (file generationModuleFile) unchanged() error {
	current, err := readGenerationModuleFile(file.path)
	if err != nil {
		return err
	}
	if current.exists != file.exists || current.mode != file.mode || !bytes.Equal(current.data, file.data) {
		return fmt.Errorf("module configuration changed while adding dependencies; preserving current file: %s", file.path)
	}
	return nil
}

func publishGenerationModuleFiles(original, updated [2]generationModuleFile) error {
	var staged [2]string
	var rollback string
	defer func() {
		for _, name := range staged {
			if name != "" {
				os.Remove(name)
			}
		}
		if rollback != "" {
			os.Remove(rollback)
		}
	}()
	for i := range original {
		if err := original[i].unchanged(); err != nil {
			return err
		}
		if !updated[i].exists {
			continue
		}
		file, err := os.CreateTemp(filepath.Dir(original[i].path), ".goxide-dependency-*")
		if err != nil {
			return err
		}
		staged[i] = file.Name()
		_, writeErr := file.Write(updated[i].data)
		modeErr := file.Chmod(original[i].mode)
		closeErr := file.Close()
		if err := errors.Join(writeErr, modeErr, closeErr); err != nil {
			return err
		}
	}
	if original[1].exists && staged[1] != "" {
		file, err := os.CreateTemp(filepath.Dir(original[1].path), ".goxide-dependency-*")
		if err != nil {
			return err
		}
		rollback = file.Name()
		_, writeErr := file.Write(original[1].data)
		modeErr := file.Chmod(original[1].mode)
		closeErr := file.Close()
		if err := errors.Join(writeErr, modeErr, closeErr); err != nil {
			return err
		}
	}
	// Publish checksums before the manifest that requires them. If the second
	// rename fails, restore only the checksum bytes written by this transaction.
	for _, i := range []int{1, 0} {
		err := original[i].unchanged()
		if err == nil && staged[i] != "" {
			err = os.Rename(staged[i], original[i].path)
		}
		if err != nil {
			if i == 0 && staged[1] != "" {
				published := updated[1]
				published.path, published.mode = original[1].path, original[1].mode
				if rollbackErr := published.unchanged(); rollbackErr != nil {
					return errors.Join(err, rollbackErr)
				}
				if original[1].exists {
					return errors.Join(err, os.Rename(rollback, original[1].path))
				}
				return errors.Join(err, os.Remove(original[1].path))
			}
			return err
		}
	}
	return nil
}
