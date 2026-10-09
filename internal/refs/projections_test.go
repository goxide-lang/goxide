package refs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const projectionDeclarations = `
type E struct { tag int; value int }
type Payload = int
func Project(e *E)*int { if e==nil||e.tag!=1 { panic("variant") };return &e.value }
`

func compileProjectionFixture(source string, registry []Projection, observers ...EnumObserver) ([]byte, error) {
	prepared, err := Prepare("projection.gox", []byte(source))
	if err != nil {
		return nil, err
	}
	files, bindings, err := BindProjectionsWithPath("example/projection", map[string][]byte{"projection.gox": prepared.Source}, nil, registry)
	if err != nil {
		return nil, err
	}
	finished, err := FinishWithEvidence("example/projection", "projection.gox", files["projection.gox"], nil, PermissionEvidence{EnumTypes: bindings.ReferenceElements, Observers: observers})
	if err != nil {
		return nil, err
	}
	return bindings.Restore("projection.gox", finished.Source)
}

func TestEnumProjectionImportedPrivateGenericPayload(t *testing.T) {
	const producer = `package model
type private struct{N int}
type PublicPayload = private
type Box[T any]struct{value T}
type Concrete = Box[private]
func New()Concrete{return Concrete{}}
func Project[T any](b *Box[T])*T{return &b.value}
`
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "model.go", producer, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{}).Check("example/model", fs, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	imp := metadataImporter{"example/model": pkg}
	source := `package consumer
import route "example/model"
func Take(e &mut route.Concrete)&mut route.PublicPayload {
 return __goxide_enum_project_write(route.Project[route.PublicPayload],e)
}
func Run()int{e:=route.New();p:=Take(&mut e);p.N=4;return p.N}
`
	prepared, err := Prepare("consumer.gox", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	registry := []Projection{{PackagePath: "example/model", Function: "Project", EnumTypeID: "example/model.Box[example/model.private]", PayloadTypeID: "example/model.private"}}
	bound, bindings, err := BindProjectionsWithPath("example/consumer", map[string][]byte{"consumer.gox": prepared.Source}, imp, registry)
	if err != nil {
		t.Fatal(err)
	}
	lowered, err := FinishWithEnumTypes("example/consumer", "consumer.gox", bound["consumer.gox"], imp, bindings.ReferenceElements)
	if err != nil {
		t.Fatal(err)
	}
	out, err := bindings.Restore("consumer.gox", lowered.Source)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "route.private") || strings.Contains(string(out), "__goxide_permission_") {
		t.Fatalf("private/analysis spelling leaked:\n%s", out)
	}
	finalFS := token.NewFileSet()
	final, err := parser.ParseFile(finalFS, "consumer.go", out, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = (&types.Config{Importer: imp}).Check("example/consumer", finalFS, []*ast.File{final}, nil); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestEnumErrorPayloadKeepsReferencePermissions(t *testing.T) {
	decls := `package p;type E struct{slot error};func Project(e *E)*error{return &e.slot};`
	registry := []Projection{{PackagePath: "example/projection", Function: "Project", EnumTypeID: "example/projection.E", PayloadTypeID: "error"}}
	for _, body := range []string{
		`func f(){e:=E{};w:=__goxide_enum_project_write(Project,&mut e);*w=nil;r:=__goxide_enum_project_read(Project,&e);copy:=r;_=copy}`,
		`func f(e &E)&error{return __goxide_enum_project_read(Project,e)}`,
	} {
		if _, err := compileProjectionFixture(decls+body, registry); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{
		`func f(e &E){r:=__goxide_enum_project_read(Project,e);*r=nil}`,
		`func f(e &E)error{r:=__goxide_enum_project_read(Project,e);return *r}`,
		`func f(e &E)string{r:=__goxide_enum_project_read(Project,e);return (*r).Error()}`,
	} {
		if _, err := compileProjectionFixture(decls+body, registry); err == nil {
			t.Fatalf("readonly error capability erased: %s", body)
		}
	}
}

func projectionEvidence() []Projection {
	return []Projection{{PackagePath: "example/projection", Function: "Project", EnumTypeID: "example/projection.E", PayloadTypeID: "int", PayloadAlias: "Payload"}}
}

func TestEnumProjectionReferenceIdentity(t *testing.T) {
	source := "package main\n" + projectionDeclarations + `
func take(e &mut E)&mut int { return __goxide_enum_project_write(Project,e) }
func main(){
 e:=E{tag:1,value:1}; before:=e; owner:=&mut e
 p:=take(owner); r:=__goxide_enum_project_read(Project,&(*owner)); q:=p
 reader:=func()int{return *r}
 *q=3
 if e.value!=3||before.value!=1||reader()!=3{panic("identity")}
 other:=7; replacement:=&mut other; p=replacement;*p=8
 if e.value!=3||other!=8{panic("rebind")}
}
`
	out, err := compileProjectionFixture(source, projectionEvidence())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "__goxide_permission_") || strings.Contains(string(out), "__goxide_enum_project_") || strings.Contains(string(out), prefix) {
		t.Fatalf("analysis stub leaked:\n%s", out)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example/projection\ngo 1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), out, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "-race", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	if log, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s\n%s", err, log, out)
	}
}

func TestEnumProjectionRejectsCapabilityAndOwnerForgery(t *testing.T) {
	for name, body := range map[string]string{
		"readonly-write":  `func f(e &E){p:=__goxide_enum_project_read(Project,e);*p=2}`,
		"owner-upgrade":   `func f(e &E){p:=__goxide_enum_project_write(Project,e);*p=2}`,
		"raw-adoption":    `func f(e *E){p:=__goxide_enum_project_read(Project,e);_=p}`,
		"raw-erasure":     `func f(e &E)*int{return __goxide_enum_project_read(Project,e)}`,
		"function-shadow": `func f(){Project:=func(e *E)*int{return &e.value};e:=E{tag:1};p:=__goxide_enum_project_read(Project,&e);_=p}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := compileProjectionFixture("package p\n"+projectionDeclarations+body, projectionEvidence())
			if err == nil {
				t.Fatal("accepted invalid projection")
			}
			if !strings.Contains(err.Error(), "projection.gox") {
				t.Fatalf("lost origin: %v", err)
			}
		})
	}
	for _, which := range []string{"missing", "owner", "payload", "alias"} {
		t.Run(which, func(t *testing.T) {
			registry := projectionEvidence()
			switch which {
			case "missing":
				registry = nil
			case "owner":
				registry[0].PackagePath = "wrong/owner"
			case "payload":
				registry[0].PayloadTypeID = "string"
			case "alias":
				registry[0].PayloadAlias = "string"
			}
			_, err := compileProjectionFixture("package p\n"+projectionDeclarations+`func f(){e:=E{tag:1};p:=__goxide_enum_project_read(Project,&e);_=p}`, registry)
			if err == nil {
				t.Fatal("accepted forged projection evidence")
			}
		})
	}
}

func TestReferenceMetadataRegisteredEnumElement(t *testing.T) {
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "enum.go", `package p;type E[T any]struct{value T; err error};func View(e *E[int])*E[int]{return e}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{}).Check("example/p", fs, []*ast.File{f}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sig := pkg.Scope().Lookup("View").Type().(*types.Signature)
	contract := signatureSlots(sig)
	contract.Params[0].Permission = Read
	contract.Results[0].Permission = Read
	if validateSignature(sig, contract) == nil {
		t.Fatal("unregistered generic element accepted")
	}
	if err := validateSignature(sig, contract, map[string]bool{"example/p.E[int]": true}); err != nil {
		t.Fatal(err)
	}
	if validateSignature(sig, contract, map[string]bool{"wrong/p.E[int]": true}) == nil {
		t.Fatal("wrong owner authorized element")
	}
	contract.Results[0].Permission = "exclusive"
	if validateSignature(sig, contract, map[string]bool{"example/p.E[int]": true}) == nil {
		t.Fatal("registration bypassed permissions")
	}
}
