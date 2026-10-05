package project

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SuppliedSource is migration evidence, not a resolver or build policy.
type SuppliedSource struct {
	Source          string   `json:"source"`
	Module          string   `json:"module,omitempty"`
	Version         string   `json:"version,omitempty"`
	Status          string   `json:"status"`
	Sum             string   `json:"sum,omitempty"`
	GoModSum        string   `json:"go_mod_sum,omitempty"`
	Compared        int      `json:"compared_files"`
	Differences     []string `json:"differences,omitempty"`
	ModeDifferences []string `json:"mode_differences,omitempty"`
	Accepted        bool     `json:"differences_accepted,omitempty"`
}

func SuppliedRemote(m VendorModule) (string, string) {
	if m.Replace != "" {
		return m.Replace, m.ReplaceVersion
	}
	return m.Path, m.Version
}

// VerifySuppliedRemote compares the supplied subset with a checksum-verified zip.
// It never trusts a mutable expanded module cache, guesses a Git URL or uses the
// original sum file as a selection policy. Every Go subprocess obeys offline.
func VerifySuppliedRemote(tool, root string, a *SuppliedModules, module string, offline bool) (SuppliedSource, error) {
	s := SuppliedSource{Source: "remote", Status: "unknown"}
	m, ok := a.Selected[module]
	if !ok {
		return s, fmt.Errorf("unknown supplied module %s", module)
	}
	s.Module, s.Version = SuppliedRemote(m)
	if s.Version == "" {
		return s, fmt.Errorf("remote source for %s is unconfirmed (local replacement); provide local source", module)
	}
	dir, e := os.MkdirTemp("", "goxide-source-check-")
	if e != nil {
		return s, e
	}
	defer os.RemoveAll(dir)
	if e = os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module goxide.invalid/sourcecheck\ngo 1.27\n"), 0600); e != nil {
		return s, e
	}
	data, e := goCommand(tool, dir, offline, "mod", "download", "-json", s.Module+"@"+s.Version)
	if e != nil {
		return s, fmt.Errorf("remote source %s@%s (offline=%t): %w", s.Module, s.Version, offline, e)
	}
	var d struct{ Path, Version, Zip, Sum, GoModSum, Error string }
	if e = json.Unmarshal(data, &d); e != nil {
		return s, e
	}
	if d.Error != "" || d.Path != s.Module || d.Version != s.Version || d.Zip == "" || d.Sum == "" || d.GoModSum == "" {
		return s, fmt.Errorf("unverified remote identity for %s: %s", module, d.Error)
	}
	z, e := zip.OpenReader(d.Zip)
	if e != nil {
		return s, e
	}
	defer z.Close()
	files := map[string]*zip.File{}
	names := []string{}
	prefix := s.Module + "@" + s.Version + "/"
	for _, f := range z.File {
		if files[f.Name] != nil || !strings.HasPrefix(f.Name, prefix) {
			return s, fmt.Errorf("invalid remote zip member %s", f.Name)
		}
		files[f.Name] = f
		names = append(names, f.Name)
	}
	sort.Strings(names)
	hash := sha256.New()
	for _, name := range names {
		f, e := files[name].Open()
		if e != nil {
			return s, e
		}
		h := sha256.New()
		_, e = io.Copy(h, f)
		f.Close()
		if e != nil {
			return s, e
		}
		fmt.Fprintf(hash, "%x  %s\n", h.Sum(nil), name)
	}
	if "h1:"+base64.StdEncoding.EncodeToString(hash.Sum(nil)) != d.Sum {
		return s, fmt.Errorf("remote zip checksum mismatch for %s", module)
	}
	s.Sum, s.GoModSum = d.Sum, d.GoModSum
	for _, rel := range Keys(a.Outputs) {
		for _, output := range a.Outputs[rel] {
			localPrefix := a.Paths[module] + "/"
			if !strings.HasPrefix(output, localPrefix) {
				continue
			}
			original := filepath.Join(root, "vendor", filepath.FromSlash(rel))
			b, e := os.ReadFile(original)
			if e != nil {
				return s, e
			}
			info, e := os.Stat(original)
			if e != nil {
				return s, e
			}
			if Digest([]any{b, uint32(info.Mode().Perm())}) != a.Snapshot.Files[rel] {
				return s, fmt.Errorf("supplied source changed during verification: %s", rel)
			}
			// Ancestor license copies have no proven module-relative upstream name.
			if !strings.HasPrefix(rel, module+"/") {
				s.Differences = append(s.Differences, rel+": unknown ancestor license mapping")
				continue
			}
			name := strings.TrimPrefix(rel, module+"/")
			f := files[prefix+name]
			s.Compared++
			if f == nil {
				s.Differences = append(s.Differences, rel+": missing remotely")
				continue
			}
			reader, e := f.Open()
			if e != nil {
				return s, e
			}
			remote, e := io.ReadAll(reader)
			reader.Close()
			if e != nil {
				return s, e
			}
			if !bytes.Equal(b, remote) {
				s.Differences = append(s.Differences, rel+": bytes differ")
			}
			if info.Mode().Perm() != f.Mode().Perm() {
				s.ModeDifferences = append(s.ModeDifferences, fmt.Sprintf("%s: supplied %04o, zip %04o", rel, info.Mode().Perm(), f.Mode().Perm()))
			}
		}
	}
	if s.Compared == 0 && len(s.Differences) == 0 {
		s.Differences = append(s.Differences, "no supplied files available for comparison: unknown")
	}
	s.Status = "verified subset"
	if len(s.Differences) > 0 {
		s.Status = "modified/unknown"
	}
	return s, nil
}

func WriteSuppliedAudit(destination string, a *SuppliedModules) error {
	data, e := json.MarshalIndent(a, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(destination, "goxide-import.json"), append(data, '\n'), 0644)
}
