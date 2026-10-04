// Package scripttrust stores local user grants outside sources and build artifacts.
// It is an authorization mechanism, not an OS security boundary against same-UID code.
package scripttrust

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Store struct {
	Version   int             `json:"version"`
	Projects  map[string]bool `json:"projects"`
	Subjects  map[string]bool `json:"subjects"`
	directory string
	release   func()
}

// Location resolves the user-owned policy directory without creating it.
func Location() (string, error) {
	directory := os.Getenv("HGO_TRUST_DIR")
	if directory == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		directory = filepath.Join(config, "goxide", "build-trust")
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	return directory, nil
}

// CheckLocation prevents grants from being stored in source or disposable outputs.
func CheckLocation(forbidden ...string) error {
	directory, err := Location()
	if err != nil {
		return err
	}
	for _, root := range forbidden {
		if root == "" {
			continue
		}
		root, err = filepath.Abs(root)
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(root, directory)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("B-TRUST-PATH: trust must be outside sources and build artifacts: %s", directory)
		}
	}
	for at := directory; ; at = filepath.Dir(at) {
		if _, e := os.Lstat(filepath.Join(at, ".hgo-layout.json")); e == nil {
			return fmt.Errorf("B-TRUST-PATH: trust cannot be in a managed layout")
		} else if !os.IsNotExist(e) {
			return e
		}
		if filepath.Dir(at) == at {
			break
		}
	}
	return nil
}

// Open takes an exclusive policy lease. Mutations fail busy while a cooperating
// build holds the lease, so a successful revocation cannot race artifact binding.
// A crashed process may leave lease/ behind; never steal possibly live leases.
func Open(forbidden ...string) (*Store, error) {
	if err := CheckLocation(forbidden...); err != nil {
		return nil, err
	}
	directory, err := Location()
	if err != nil {
		return nil, err
	}
	// Reject links in existing components before creating anything below them.
	for p := directory; ; p = filepath.Dir(p) {
		info, e := os.Lstat(p)
		if e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			return nil, fmt.Errorf("B-TRUST-PATH: trust directory must contain only real directories: %s", p)
		}
		if e != nil && !os.IsNotExist(e) {
			return nil, e
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	lease := filepath.Join(directory, "lease")
	if err = os.Mkdir(lease, 0700); err != nil {
		return nil, fmt.Errorf("B-TRUST-BUSY: another build/policy operation or stale lease; do not remove while active: %w", err)
	}
	s := &Store{Version: 1, Projects: map[string]bool{}, Subjects: map[string]bool{}, directory: directory, release: func() { _ = os.Remove(lease) }}
	file := filepath.Join(directory, "grants.json")
	info, err := os.Lstat(file)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		s.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		s.Close()
		return nil, fmt.Errorf("B-TRUST-PATH: grants must be a regular file")
	}
	f, err := os.Open(file)
	if err != nil {
		s.Close()
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(io.LimitReader(f, 1<<20))
	dec.DisallowUnknownFields()
	if err = dec.Decode(s); err == nil {
		var extra any
		if e := dec.Decode(&extra); e != io.EOF {
			err = fmt.Errorf("trailing trust data")
		}
	}
	if err != nil || s.Version != 1 || s.Projects == nil || s.Subjects == nil {
		s.Close()
		return nil, fmt.Errorf("B-TRUST-INVALID: invalid local policy: %v", err)
	}
	return s, nil
}
func (s *Store) Close() {
	if s.release != nil {
		s.release()
		s.release = nil
	}
}
func (s *Store) Save() error {
	if s.release == nil {
		return fmt.Errorf("trust policy lease closed")
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(s.directory, ".grants-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(append(data, '\n')); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(s.directory, "grants.json"))
}
func (s *Store) Authorize(project string, required []string) error {
	if !s.Projects[project] {
		return fmt.Errorf("B-TRUST-ROOT: project is not trusted; inspect 'hgo scripts --action plan', then explicitly use --action trust-project")
	}
	for _, id := range required {
		if !s.Subjects[id] {
			return fmt.Errorf("B-TRUST-SUBJECT: unapproved or changed host execution closure %s; inspect plan then --action allow --subject %s", id, id)
		}
	}
	return nil
}
func ValidSubject(id string) bool {
	if len(id) != 64 {
		return false
	}
	return strings.Trim(id, "0123456789abcdef") == ""
}
