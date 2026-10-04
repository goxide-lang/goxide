package acceptance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Install the actual CLI, leave its repository, and exercise a newly authored
// business example through the public command boundary and native Go tools.
func TestT0DeliveryInstalledCLI(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin", "go")
	bin := t.TempDir()
	deliveryCommand(t, root, tool, true, []string{"GOBIN=" + bin}, "install", "./cmd/goxide")
	cli := filepath.Join(bin, "goxide")
	outside := t.TempDir()
	input := filepath.Join(root, "examples/prototype/delivery_job.gox")
	original, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	generated := t.TempDir()
	first := filepath.Join(generated, "main.go")
	firstMap := first + ".map.json"
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", first, "--source-map", firstMap, input)
	second := filepath.Join(t.TempDir(), "main.go")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", second, "--source-map", second+".map.json", input)
	for _, suffix := range []string{"", ".map.json"} {
		a, err := os.ReadFile(first + suffix)
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(second + suffix)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("repeated CLI generation differs: %s", suffix)
		}
	}
	packagesWrite(t, generated, "go.mod", "module delivery.example/app\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => "+strconvQuote(root)+"\n")
	packagesWrite(t, generated, "delivery_test.go", deliveryAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", ".")
	application := filepath.Join(t.TempDir(), "job")
	deliveryCommand(t, generated, tool, true, nil, "build", "-trimpath", "-buildvcs=false", "-o", application, ".")
	output := deliveryCommand(t, outside, application, true, nil)
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var event map[string]any
	if len(lines) != 2 || json.Unmarshal([]byte(lines[0]), &event) != nil || event["msg"] != "job accepted" || event["name"] != "mail" || event["units"] != float64(3) || lines[1] != "scheduled mail: 3 units" {
		t.Fatalf("unexpected independent application output: %s", output)
	}
	cliOutput := deliveryCommand(t, outside, cli, true, nil, "run", "--go", tool, "--runtime", root, input)
	if !strings.Contains(cliOutput, `"msg":"job accepted"`) || !strings.Contains(cliOutput, "scheduled mail: 3 units") {
		t.Fatalf("installed CLI run with explicit runtime failed: %s", cliOutput)
	}
	for _, invalid := range []string{"{", `{"Name":"mail","Mode":{"Batch":9},"Labels":[]}`} {
		output := deliveryCommand(t, outside, application, false, nil, invalid)
		if !strings.Contains(output, "job rejected") || strings.Contains(output, "job accepted") {
			t.Fatalf("failure logged as success: %s", output)
		}
	}
	// Exercise input-alias protection through the installed executable.
	alias := filepath.Join(t.TempDir(), "alias.go")
	if err := os.Symlink(input, alias); err != nil {
		t.Fatal(err)
	}
	output = deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", alias, input)
	if !strings.Contains(output, "overwrite input") {
		t.Fatalf("wrong alias rejection: %s", output)
	}
	after, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(original, after) {
		t.Fatal("CLI changed example input")
	}
	t.Run("multi-file-business", func(t *testing.T) {
		source := t.TempDir()
		readLimit := "func readLimit(limit &int) int { return *limit }"
		if strings.Count(string(original), readLimit) != 1 {
			t.Fatal("example no longer has the expected readonly helper")
		}
		packagesWrite(t, source, "go.mod", "module delivery.example/package\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => "+strconvQuote(root)+"\n")
		packagesWrite(t, source, "main.gox", strings.Replace(string(original), readLimit, "", 1))
		packagesWrite(t, source, "limits.gox", "package main\n"+readLimit+"\n")
		dest := filepath.Join(t.TempDir(), "generated")
		deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", dest, source)
		repeated := filepath.Join(t.TempDir(), "generated")
		deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", repeated, source)
		if err := filepath.WalkDir(dest, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !(strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".map.json")) {
				return nil
			}
			rel, err := filepath.Rel(dest, path)
			if err != nil {
				return err
			}
			first, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			second, err := os.ReadFile(filepath.Join(repeated, rel))
			if err != nil {
				return err
			}
			if !bytes.Equal(first, second) {
				t.Errorf("directory generation is not reproducible: %s", rel)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		packagesWrite(t, dest, "delivery_test.go", deliveryAssertions)
		deliveryCommand(t, dest, tool, true, nil, "test", "-race", "-count=1", "-v", ".")
	})
	t.Run("readonly-write-rejected", func(t *testing.T) {
		source := t.TempDir()
		packagesWrite(t, source, "go.mod", "module delivery.example/invalid\ngo 1.27\nrequire github.com/hxsf/goxide v0.0.0\nreplace github.com/hxsf/goxide => "+strconvQuote(root)+"\n")
		packagesWrite(t, source, "bad.gox", string(original)+"\nfunc illegalWrite(limit &int) { *limit = 9 }\n")
		dest := filepath.Join(t.TempDir(), "bad.go")
		output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", dest, filepath.Join(source, "bad.gox"))
		if !strings.Contains(output, "cannot write through a read") || !strings.Contains(output, "bad.gox:") {
			t.Fatalf("missing permission failure: %s", output)
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Fatalf("failed single-file output retained: %v", err)
		}
	})

	t.Run("cross-package-diagnostic-and-cleanup", func(t *testing.T) {
		source := t.TempDir()
		packagesWrite(t, source, "go.mod", "module delivery.example/broken\ngo 1.27\n")
		packagesWrite(t, source, "kit/definition.gox", "package kit\nfunc Emit(value string){}\npub macro_rules! broken {()=>{Emit(17)};}\n")
		packagesWrite(t, source, "main.gox", "package main\nimport k \"delivery.example/broken/kit\"\nfunc main(){k.broken!()}\n")
		dest := filepath.Join(t.TempDir(), "generated")
		output := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", dest, source)
		for _, want := range []string{"definition.gox:3:", "main.gox:3:"} {
			if !strings.Contains(output, want) {
				t.Fatalf("missing %s in real CLI error: %s", want, output)
			}
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Fatalf("failed module output retained: %v", err)
		}
	})
}

func strconvQuote(s string) string { b, _ := json.Marshal(filepath.ToSlash(s)); return string(b) }

func deliveryCommand(t *testing.T, dir, tool string, success bool, extra []string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.Dir = dir
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch name {
		case "GOTOOLCHAIN", "GOWORK", "GOPROXY", "GOSUMDB", "GOFLAGS", "GOBIN":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "GOTOOLCHAIN=local", "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=")
	cmd.Env = append(cmd.Env, extra...)
	output, err := cmd.CombinedOutput()
	if (err == nil) != success {
		t.Fatalf("%s %v: expected success=%t; got %v\n%s", tool, args, success, err, output)
	}
	t.Logf("%s %v:\n%s", filepath.Base(tool), args, output)
	return string(output)
}

const deliveryAssertions = `package main
import("bytes";"errors";"log/slog";"reflect";"strings";"testing";"github.com/goxide-lang/serde/json")
func TestJobSuccessAndClone(t *testing.T){
 var output bytes.Buffer;old:=slog.Default();defer slog.SetDefault(old);slog.SetDefault(slog.New(slog.NewJSONHandler(&output,nil)))
 raw:=[]byte("{\"Name\":\"archive\",\"Mode\":{\"Batch\":2},\"Labels\":[\"cold\"]}")
 receipt,err:=RunJob(raw,3,false);if err!=nil||receipt.Name!="archive"||receipt.Units!=2||output.Len()!=0{t.Fatalf("disabled flow: %#v %v %s",receipt,err,output.String())}
 receipt,err=RunJob(raw,3,true);if err!=nil||receipt.Units!=2||strings.Count(output.String(),"job accepted")!=1{t.Fatal(receipt,err,output.String())}
 original,err:=serdejson.Unmarshal(raw,DeserializeJob);if err!=nil{t.Fatal(err)};copied:=CloneJob(&original);copied.Labels[0]="changed";if original.Labels[0]!="cold"{t.Fatal("clone shared labels")}
 encoded,err:=serdejson.Marshal(&original,SerializeJob);if err!=nil{t.Fatal(err)};again,err:=serdejson.Unmarshal(encoded,DeserializeJob);if err!=nil||!reflect.DeepEqual(original,again){t.Fatal("roundtrip",err)}
 receipt,err=RunJob([]byte("{\"Name\":\"single\",\"Mode\":\"Immediate\",\"Labels\":[]}"),3,false);if err!=nil||receipt.Units!=1{t.Fatal(receipt,err)}
}
func TestJobFailureFromAndNoSuccessEffects(t *testing.T){
 var output bytes.Buffer;old:=slog.Default();defer slog.SetDefault(old);slog.SetDefault(slog.New(slog.NewJSONHandler(&output,nil)))
 for _,tc:=range []struct{raw string;cause error}{
  {"{\"Name\":\"\",\"Mode\":\"Immediate\",\"Labels\":[]}",missingName},
  {"{\"Name\":\"bulk\",\"Mode\":{\"Batch\":4},\"Labels\":[]}",exceededLimit},
  {"{",nil},
 }{_,err:=RunJob([]byte(tc.raw),3,true);var failure JobFailure;if err==nil||!errors.As(err,&failure)||errors.Unwrap(err)==nil{t.Fatalf("lost From/source: %v",err)};if tc.cause!=nil&&!errors.Is(err,tc.cause){t.Fatalf("wrong source: %v",err)}}
 if output.Len()!=0{t.Fatalf("failed jobs emitted success: %s",output.String())}
}
`
