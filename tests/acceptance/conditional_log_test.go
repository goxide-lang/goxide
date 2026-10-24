package acceptance_test

import (
	"os"
	"strings"
	"testing"

	testdeps "github.com/goxide-lang/goxide/tests/testutil"
)

func TestConditionalLogApplicationRace(t *testing.T) {
	source, err := os.ReadFile("../../examples/prototype/conditional_log.gox")
	if err != nil {
		t.Fatal(err)
	}
	formalBusinessExample(t, source, `package main
import("bytes";"encoding/json";"reflect";"testing";"log/slog";"errors";"context")
func TestConditionalFieldsAndErrorChain(t *testing.T){
 old:=slog.Default();defer slog.SetDefault(old)
 for _,level:=range []slog.Level{slog.LevelError,slog.LevelInfo}{
  var out bytes.Buffer;slog.SetDefault(slog.New(slog.NewJSONHandler(&out,&slog.HandlerOptions{Level:level})))
  trace:=[]string{};cause:=errors.New("down");failure:=RequestFailureOps{}.Wrap(cause)
  emitRequest(func()bool{trace=append(trace,"enabled");return slog.Default().Enabled(context.Background(),slog.LevelInfo)},func()string{trace=append(trace,"message");return "request"},func()int{trace=append(trace,"request_id");return 41},failure)
  want:=[]string{"enabled"};if level==slog.LevelInfo{want=append(want,"message","request_id")}
  if !reflect.DeepEqual(trace,want){t.Fatalf("order/count=%v",trace)}
  if level==slog.LevelError{if out.Len()!=0{t.Fatal(out.String())};continue}
  var event map[string]any;if err:=json.Unmarshal(out.Bytes(),&event);err!=nil{t.Fatal(err)}
  if event["request_id"]!=float64(41)||event["msg"]!="request"||event["error"]!="request failed"{t.Fatal(event)}
  var typed RequestFailure;if !errors.Is(failure,cause)||!errors.As(failure,&typed)||typed.Cause!=cause{t.Fatal("lost error chain")}
 }
}
`, "cause preserved: true")
}

func formalBusinessExample(t *testing.T, source []byte, assertions, wantOutput string) {
	t.Helper()
	dependencies := stdFixtureImports + "[dependencies.context]\ngo=\"context\"\n[dependencies.slog]\ngo=\"log/slog\"\n[dependencies.os]\ngo=\"os\"\n[dependencies.strconv]\ngo=\"strconv\"\n"
	result, err := testdeps.FormalBinarySource(t, "main.gox", source, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	again, err := testdeps.FormalBinarySource(t, "main.gox", source, dependencies)
	if err != nil || string(result.Go) != string(again.Go) || result.MacroCalls == 0 || again.MacroCalls == 0 {
		t.Fatalf("formal procedural generation is not reproducible: %v", err)
	}
	packagesWrite(t, result.Project.Root, "business_test.go", assertions)
	testdeps.RunFormalGo(t, result.Project, "test", "-mod=readonly", "-race", "-count=1", "./...")
	testdeps.RunFormalGo(t, result.Project, "vet", "-mod=readonly", "./...")
	if output := testdeps.RunFormalGo(t, result.Project, "run", "-mod=readonly", "."); !strings.Contains(output, wantOutput) {
		t.Fatalf("example output %q does not contain %q", output, wantOutput)
	}
}
