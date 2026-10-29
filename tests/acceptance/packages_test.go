package acceptance_test

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

// Compile the example through the CLI, then use a real, separate Go module.
// Race instrumentation must cover generated code, not just this test driver.
func TestPackagesApplication(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	cli := acceptanceCLI(t)

	t.Run("independent-module-race-and-build", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "source")
		if err := filepath.WalkDir(filepath.Join(root, "examples/packages"), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(filepath.Join(root, "examples/packages"), path)
			if err != nil {
				return err
			}
			dest := filepath.Join(source, rel)
			if entry.IsDir() {
				return os.MkdirAll(dest, 0700)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			return testdeps.WriteFile(dest, data, 0600)
		}); err != nil {
			t.Fatal(err)
		}
		packagesWrite(t, source, "go.mod", packagesModule()+stdFixtureGoRequirements(t))
		generated := filepath.Join(t.TempDir(), "generated")
		packagesCommand(t, root, cli, true, "transpile", "-o", generated, source)
		packagesWrite(t, generated, "application_test.go", packagesAssertions)
		packagesCommand(t, generated, tool, true, "test", "-race", "-count=1", "-v", "./...")
		binary := filepath.Join(t.TempDir(), "application")
		packagesCommand(t, generated, tool, true, "build", "-buildvcs=false", "-o", binary, ".")
		output := packagesCommand(t, generated, binary, true)
		if !strings.Contains(output, `"msg":"configuration accepted"`) {
			t.Fatalf("unexpected binary output: %s", output)
		}
		output = packagesCommand(t, generated, binary, false, "{")
		if !strings.Contains(output, `"msg":"configuration rejected"`) {
			t.Fatalf("unexpected rejection: %s", output)
		}
	})

	t.Run("diagnostics", func(t *testing.T) {
		cases := []struct {
			name, source, filename string
			line                   int
			files                  map[string]string
		}{
			{"cross-file-type-error", "package main\nfunc main() { var n int = Shared; _ = n }\n", "bad.gox", 2, map[string]string{"shared.gox": "package main\nvar Shared = \"wrong\"\n"}},
			{"uses-are-file-scoped", "package main\nfunc main() { printing::Println(1) }\n", "bad.gox", 2, map[string]string{"other.gox": "package main\nuse fmt as printing;\nfunc other() { printing::Println(2) }\n"}},
			{"private-function", "package main\nuse model as cfg;\nfunc main() { cfg::hidden() }\n", "bad.gox", 3, map[string]string{"model/model.gox": "package model\nfunc hidden() {}\n"}},
			{"private-enum", "package main\nuse model as cfg;\nfunc main() { var value cfg::secret; _ = value }\n", "bad.gox", 3, map[string]string{"model/model.gox": "package model\nenum secret { Idle, }\n"}},
			{"nominal-enums", "package main\nuse model as cfg;\npub enum Mode { Idle, }\nfunc main() { var local Mode; cfg::Accept(local) }\n", "bad.gox", 4, map[string]string{"model/model.gox": "package model\npub enum Mode { Idle, }\nfunc Accept(value Mode) {}\n"}},
			{"cross-package-macro", "package main\nuse model as cfg;\nfunc main() { cfg::identity!(1) }\n", "bad.gox", 3, map[string]string{"model/model.gox": "package model\nmacro_rules! identity { ($value:expr) => { $value }; }\n"}},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				source := t.TempDir()
				packagesWrite(t, source, "go.mod", packagesModule()+stdFixtureGoRequirements(t))
				writeAcceptanceFixtureRoots(t, source, "packages.example/app", map[string]string{"model": "packages.example/app/model"})
				packagesWrite(t, source, test.filename, test.source)
				for name, data := range test.files {
					packagesWrite(t, source, name, data)
				}
				output := packagesCommand(t, root, cli, false, "build", "--go", tool, "--runtime", root, "-o", filepath.Join(t.TempDir(), "bad"), source)
				if test.name == "cross-file-type-error" || test.name == "nominal-enums" {
					if !strings.Contains(output, "cannot use") {
						t.Fatal("missing package type error", output)
					}
				}
				location := regexp.MustCompile(regexp.QuoteMeta(test.filename) + ":" + fmt.Sprint(test.line) + `(?::[0-9]+)?[: ]`)
				if !location.MatchString(output) {
					t.Fatalf("diagnostic did not map to original source %s:%d:\n%s", test.filename, test.line, output)
				}
			})
		}
	})
}

func packagesModule() string {
	return "module packages.example/app\n\ngo 1.27\n"
}

func packagesWrite(t *testing.T, root, name, data string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := testdeps.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func packagesCommand(t *testing.T, dir, executable string, success bool, args ...string) string {
	t.Helper()
	cmd := exec.Command(executable, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	started := time.Now()
	output, err := cmd.CombinedOutput()
	elapsed := time.Since(started)
	if (err == nil) != success {
		t.Fatalf("%s %v: elapsed=%.3fs expected success=%t; got %v\n%s", executable, args, elapsed.Seconds(), success, err, output)
	}
	t.Logf("%s %v: elapsed=%.3fs\n%s", filepath.Base(executable), args, elapsed.Seconds(), output)
	return string(output)
}

const packagesAssertions = `package main
import (
 "bytes"
 "encoding/json"
 "errors"
 "io"
 "log/slog"
 "reflect"
 "testing"
 cfg "packages.example/app/model"
 "github.com/goxide-lang/serde"
 "github.com/goxide-lang/serde/json"
)
func invokePackage(t *testing.T, input string) (map[string]any, error) {
 t.Helper()
 var output bytes.Buffer
 err := Run(slog.New(slog.NewJSONHandler(&output,nil)), " 42 ", []byte(input))
 decoder := json.NewDecoder(&output)
 var event map[string]any
 if e := decoder.Decode(&event); e != nil { t.Fatal(e) }
 if e := decoder.Decode(new(any)); e != io.EOF { t.Fatalf("expected exactly one log: %v", e) }
 if event["request_id"] != "request-42" { t.Fatalf("native Go package lost: %#v", event) }
 return event, err
}
func TestApplicationSuccess(t *testing.T) {
 for _, c := range []struct{input,mode string;workers float64}{
  {"{\"Name\":\"mail\",\"Mode\":{\"Workers\":4},\"Labels\":[\"queue\"]}","workers",4},
  {"{\"Name\":\"mail\",\"Mode\":\"DryRun\",\"Labels\":[]}","dry_run",0},
 } {
  event, err := invokePackage(t,c.input)
  if err != nil || event["level"]!="INFO" || event["msg"]!="configuration accepted" || event["name"]!="mail" || event["mode"]!=c.mode || event["workers"]!=c.workers { t.Fatalf("success: %#v %v",event,err) }
 }
}
func TestApplicationFailures(t *testing.T) {
 for _, c := range []struct{input string;sentinel error}{
  {"{",nil},
  {"{\"Name\":\"mail\",\"Mode\":{\"Workers\":false},\"Labels\":[]}",nil},
  {"{\"Name\":\"mail\",\"Mode\":{\"Workers\":0},\"Labels\":[]}",cfg.ErrInvalidWorkers},
  {"{\"Name\":\"\",\"Mode\":\"DryRun\",\"Labels\":[]}",cfg.ErrMissingName},
 } {
  event,err := invokePackage(t,c.input)
  var typed cfg.ConfigError
  if !errors.As(err,&typed) || errors.Unwrap(err)==nil { t.Fatalf("exported ErrorFrom chain lost: %v",err) }
  if c.sentinel != nil && !errors.Is(err,c.sentinel) { t.Fatalf("sentinel lost: %v",err) }
  if c.sentinel == nil { var detail *serde.Error; if !errors.As(err,&detail) {t.Fatalf("serde cause lost: %v",err)} }
  if event["level"]!="ERROR" || event["msg"]!="configuration rejected" || event["error"]!=err.Error() || event["cause"]!=errors.Unwrap(err).Error() {t.Fatalf("failure: %#v",event)}
 }
}
func TestExportedProtocols(t *testing.T) {
 constructed:=&cfg.Workers{Value: 7}
 if name,n:=cfg.Describe(constructed); name!="workers" || n!=7 {t.Fatal("exported constructor lost payload")}
 if name,n:=cfg.Describe(&cfg.DryRun{}); name!="dry_run" || n!=0 {t.Fatal("exported unit constructor")}
 original,err:=cfg.Load([]byte("{\"Name\":\"mail\",\"Mode\":{\"Workers\":4},\"Labels\":[\"queue\"]}"))
 if err!=nil {t.Fatal(err)}
 clone:=(cfg.ConfigOps{}).Copy(&original)
 clone.Labels[0]="changed"
 if original.Labels[0]!="queue" {t.Fatal("cross-package Clone shares slice")}
 mode:=(cfg.RunModeOps{}).Copy(&original.Mode)
 data,err:=serdejson.Encode(func(s serde.Serializer)error{return (cfg.RunModeOps{}).Encode(s,&mode)})
 if err!=nil || string(data)!="{\"Workers\":4}" {t.Fatalf("enum serialize: %s %v",data,err)}
 decoded,err:=serdejson.Unmarshal(data,(cfg.RunModeOps{}).Decode)
 if err!=nil || !reflect.DeepEqual(decoded,mode) {t.Fatalf("enum deserialize: %#v %v",decoded,err)}
 sentinel:=errors.New("upstream")
 wrapped:=(cfg.ConfigErrorOps{}).Wrap(sentinel)
 if !errors.Is(wrapped,sentinel) {t.Fatal("exported From lost cause")}
 _,err=cfg.Load([]byte("{\"Name\":\"mail\",\"Mode\":{\"Workers\":false},\"Labels\":[]}"))
 var detail *serde.Error
 if !errors.As(err,&detail) || !reflect.DeepEqual(detail.Path,[]serde.PathSegment{{Kind:"Field",Name:"Mode"},{Kind:"Variant",Name:"Workers"}}) {t.Fatalf("structured serde path: %v",err)}
}
`
