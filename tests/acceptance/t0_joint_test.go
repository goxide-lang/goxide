package acceptance_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This is a joint language/frontend acceptance, not a Go mechanism probe.
func TestT0JointBusiness(t *testing.T) {
	_, here, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "../.."))
	tool := filepath.Join(runtime.GOROOT(), "bin/go")
	bin, outside := t.TempDir(), t.TempDir()
	deliveryCommand(t, root, tool, true, nil, "run", "./scripts/build.go", "-o", filepath.Join(bin, "goxide"))
	cli := filepath.Join(bin, "goxide")
	sourceFor := func() string {
		dir := t.TempDir()
		err := filepath.WalkDir(filepath.Join(root, "examples/t0_joint"), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(filepath.Join(root, "examples/t0_joint"), path)
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			packagesWrite(t, dir, rel, string(b))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		packagesWrite(t, dir, "go.mod", "module joint.example/app\ngo 1.27\n")
		return dir
	}
	source, generated := sourceFor(), filepath.Join(t.TempDir(), "generated")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", generated, source)
	repeated := filepath.Join(t.TempDir(), "generated")
	deliveryCommand(t, outside, cli, true, nil, "transpile", "-o", repeated, source)
	if err := filepath.WalkDir(generated, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(generated, path)
		a, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		b, e := os.ReadFile(filepath.Join(repeated, rel))
		if e != nil {
			return e
		}
		if string(a) != string(b) {
			t.Fatalf("nondeterministic %s", rel)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	packagesWrite(t, generated, "joint_test.go", jointAssertions)
	deliveryCommand(t, generated, tool, true, nil, "test", "-race", "-count=1", "-v", "./...")
	deliveryCommand(t, generated, tool, true, nil, "vet", "./...")
	application := filepath.Join(t.TempDir(), "orders")
	deliveryCommand(t, generated, tool, true, nil, "build", "-trimpath", "-buildvcs=false", "-o", application, ".")
	if out := deliveryCommand(t, outside, application, true, nil); strings.TrimSpace(out) != "accepted 42 units=3 retry=-1 port=8080 observed=6" {
		t.Fatal(out)
	}
	for _, input := range []string{"{", `{"Name":"bad"}`, `{"Name":"42","Mode":{"Batch":9}}`} {
		out := deliveryCommand(t, outside, application, false, nil, input)
		if !strings.Contains(out, "order rejected") || strings.Contains(out, "accepted") {
			t.Fatal(out)
		}
	}
	for name, body := range map[string]string{
		"readonly-write":   `func Bad(){b:=store::Budget{History:[]int{1}};r:=match cfg::Mode::Immediate{cfg::Mode::Immediate=>store::View(&b),cfg::Mode::Batch(_)=>store::View(&b)};r.History[0]=2}`,
		"readonly-erasure": `func Bad(){b:=store::Budget{};r:=match cfg::Mode::Immediate{cfg::Mode::Immediate=>store::View(&b),cfg::Mode::Batch(_)=>store::View(&b)};var a any=r;_=a}`,
		"source-type":      `func Bad(){r:=match cfg::Mode::Immediate{cfg::Mode::Immediate=>1,cfg::Mode::Batch(_)=>"bad"};_=r}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := sourceFor()
			imports := "use config as cfg;\n"
			if name != "source-type" {
				imports += "use store;\n"
			}
			source := "package main\n" + imports + body + "\n"
			packagesWrite(t, dir, "bad.gox", source)
			out := deliveryCommand(t, outside, cli, false, nil, "transpile", "-o", filepath.Join(t.TempDir(), "generated"), dir)
			if !strings.Contains(out, "bad.gox") {
				t.Fatal(out)
			}
			if name == "source-type" {
				if !strings.Contains(out, "bad.gox:3:") || !strings.Contains(out, "match arm has type") {
					t.Fatal(out)
				}
			} else {
				marker, advance := "r.History[0]", 0
				want := "cannot write through a read-only reference"
				if name == "readonly-erasure" {
					marker, advance = "any=r", len("any=")
					want = "cannot erase or increase read-only reference permissions"
				}
				start := strings.Index(source, marker) + advance
				line := strings.Count(source[:start], "\n") + 1
				column := start - strings.LastIndex(source[:start], "\n")
				location := fmt.Sprintf("bad.gox:%d:%d:", line, column)
				if !strings.Contains(out, location) || !strings.Contains(out, want) {
					t.Fatal(out)
				}
			}
		})
	}
	t.Run("native-go-permission-boundary", func(t *testing.T) {
		dir := t.TempDir()
		packagesWrite(t, dir, "go.mod", "module consumer.example/app\ngo 1.27\nrequire joint.example/app v0.0.0\nreplace joint.example/app => "+strconvQuote(generated)+"\n")
		packagesWrite(t, dir, "app_test.go", `package app
import("testing";"joint.example/app/store")
// The ordinary Go pointer API has Go permissions; Goxide checks remain at source call sites.
func TestNativePointer(t *testing.T){b:=store.Budget{};r:=store.View(&b);r.Limit=9;if b.Limit!=9{t.Fatal("native pointer identity")}}
`)
		deliveryCommand(t, dir, tool, true, nil, "test", "-race", "-count=1", "./...")
	})
}

const jointAssertions = `package main
import("testing";"errors";"strconv";cfg "joint.example/app/config";"joint.example/app/logkit";"github.com/goxide-lang/serde")
func reset(){cfg.DefaultCalls=0;SideEffects=0;logkit.Calls=0;logkit.Last=0}
func TestJointSuccess(t *testing.T){
 reset();r,e:=Run("{\"Name\":\"42\",\"Mode\":{\"Batch\":3}}",5,true)
 if e!=nil||r!=(Receipt{42,3,-1,8080,6})||cfg.DefaultCalls!=1||SideEffects!=1||logkit.Calls!=1||logkit.Last!=45{t.Fatal(r,e,cfg.DefaultCalls,SideEffects,logkit.Calls,logkit.Last)}
 reset();r,e=Run("{\"Name\":\"7\",\"Retry\":null}",5,false)
 if e!=nil||r.Units!=1||r.Retry!=-1||cfg.DefaultCalls!=1||SideEffects!=0||logkit.Calls!=0{t.Fatal(r,e)}
}
func TestJointSerde(t *testing.T){
 reset();q,e:=cfg.Decode([]byte("{\"Name\":\"8\",\"Retry\":2,\"Settings\":{\"Port\":90},\"Secret\":99}"))
 if e!=nil||cfg.DefaultCalls!=0||q.Settings.Port!=90||q.Secret!=0||q.Flags.Enabled||cfg.RetryCount(q)!=2{t.Fatal(q,e)}
 b,e:=cfg.Encode(q);if e!=nil{t.Fatal(e)};back,e:=cfg.Decode(b);if e!=nil||cfg.RetryCount(back)!=2||back.Settings.Port!=90||cfg.DefaultCalls!=0{t.Fatal(string(b),back,e)}
 if _,e:=cfg.Decode([]byte("{\"Name\":\"1\",\"Mode\":{\"Unknown\":0}}"));e==nil{t.Fatal("open enum accepted")}
 if _,e:=cfg.Decode([]byte("{\"Name\":\"1\",\"Unknown\":0}"));e==nil{t.Fatal("unknown field accepted")}
}
func TestJointFailures(t *testing.T){
 reset();_,e:=Run("{\"Name\":\"42\",\"Mode\":{\"Batch\":9}}",5,true);if !errors.Is(e,OverCapacity){t.Fatal(e)}
 _,e=Run("{\"Name\":\"bad\"}",5,true);var number *strconv.NumError;if !errors.As(e,&number){t.Fatal(e)}
 _,e=Run("{\"Name\":\"1\",\"Mode\":{\"Unknown\":0}}",5,true);var decode *serde.Error;if !errors.As(e,&decode){t.Fatal(e)}
 if SideEffects!=0||logkit.Calls!=0{t.Fatal("failed operation logged success")}
}
`
