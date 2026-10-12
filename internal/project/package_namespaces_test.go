package project

import (
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestPackageNamespacesSchema(t *testing.T) {
	m, err := Parse("self.toml", []byte(mappingHeader+`
[package.namespaces]
models="example.org/app/models"
"generated::schema"="example.org/app/gen/schema"
`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"models": "example.org/app/models", "generated::schema": "example.org/app/gen/schema"}
	if !reflect.DeepEqual(m.PackageNamespaces, want) || len(m.Dependencies) != 0 {
		t.Fatalf("self mappings became dependencies or lost exact paths: %+v", m)
	}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse("roundtrip.toml", data)
	if err != nil || !reflect.DeepEqual(m, again) || Digest(m) != Digest(again) {
		t.Fatalf("package namespace roundtrip: %s, %+v, %v", data, again, err)
	}
	_, requests, err := Resolve(m, nil, false)
	if err != nil || len(requests) != 0 {
		t.Fatalf("package namespaces created dependency requests: %+v, %v", requests, err)
	}
	again.PackageNamespaces["models"] = "example.org/app/other"
	if Digest(m) == Digest(again) {
		t.Fatal("package namespaces missing from manifest identity")
	}
}

func TestPackageNamespacesDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"hyphen", "[package.namespaces]\n'bad-name'='example.org/app/models'", "invalid namespace path"},
		{"blank", "[package.namespaces]\n_='example.org/app/models'", "invalid namespace path"},
		{"keyword", "[package.namespaces]\n'models::use'='example.org/app/models'", "invalid namespace path"},
		{"dot", "[package.namespaces]\n'models.item'='example.org/app/models'", "invalid namespace path"},
		{"empty segment", "[package.namespaces]\n'models::::item'='example.org/app/models'", "invalid namespace path"},
		{"empty package", "[package.namespaces]\nmodels=''", "invalid exact package path"},
		{"relative package", "[package.namespaces]\nmodels='./models'", "invalid exact package path"},
		{"bad case", "[package.Namespaces]\nmodels='example.org/app/models'", "case-sensitive"},
		{"dependency collision", "[package.namespaces]\nmodels='example.org/app/models'\n[dependencies.models]\ngo = 'example.org/models'\nversion = 'v1.0.0' ", "conflicts with dependency alias"},
		{"nested optional collision", "[package.namespaces]\n'models::item'='example.org/app/models/item'\n[dependencies.models]\ngo = 'example.org/models'\nversion = 'v1.0.0' \noptional=true", "conflicts with dependency alias"},
		{"toolchain collision", "[package.namespaces]\nfmt='example.org/app/fmt'\n[dependencies.fmt]\ngo = 'fmt' ", "conflicts with dependency alias"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse("self.toml", []byte(mappingHeader+tc.body))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestPackageNamespacesWorkspaceIsolation(t *testing.T) {
	root := t.TempDir()
	wsWrite(t, root, ManifestName, `[package]
format=2
module="example.org/root"
go="1.27"
[package.namespaces]
root_only="example.org/root/internalpkg"
[workspace]
format=1
members=["app", "other"]
`)
	wsWrite(t, root, "app/"+ManifestName, mappingHeader+"[package.namespaces]\nmodels='example.org/app/models'\n")
	wsWrite(t, root, "other/"+ManifestName, strings.Replace(mappingHeader, "example.org/app", "example.org/other", 1))
	w, err := DiscoverWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Manifests[root].PackageNamespaces) != 1 || !reflect.DeepEqual(w.Manifests[filepath.Join(root, "app")].PackageNamespaces, map[string]string{"models": "example.org/app/models"}) || len(w.Manifests[filepath.Join(root, "other")].PackageNamespaces) != 0 {
		t.Fatalf("workspace inherited another package's namespaces: %+v", w.Manifests)
	}
	wsWrite(t, root, ManifestName, "[workspace]\nformat=1\nmembers=['app']\n[workspace.package.namespaces]\nmodels='example.org/root/models'\n")
	if _, err := DiscoverWorkspace(root); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("workspace-level namespace inheritance accepted: %v", err)
	}
}

func TestPackageNamespacesLockedPlan(t *testing.T) {
	root := t.TempDir()
	manifest := mappingHeader + "[package.namespaces]\nmodels='example.org/app/models'\n"
	wsWrite(t, root, ManifestName, manifest)
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	g, err := Load(root, tool, nil, false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 1 || len(g.Plan) != 0 || len(g.Root.Requests) != 0 || len(g.Root.Sources) != 0 || len(g.Root.Children) != 0 {
		t.Fatalf("self namespace changed module graph: %+v", g)
	}
	if err := g.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(root, tool, nil, false, true, true); err != nil {
		t.Fatalf("identical self namespace rejected by lock: %v", err)
	}
	for _, changed := range []string{
		strings.Replace(manifest, "app/models", "app/other", 1),
		strings.Replace(manifest, "models=", "'models::nested'=", 1),
	} {
		wsWrite(t, root, ManifestName, changed)
		if _, err := Load(root, tool, nil, false, true, true); err == nil || !strings.Contains(err.Error(), "--locked") {
			t.Fatalf("changed self namespace reused lock: %v", err)
		}
	}
}
