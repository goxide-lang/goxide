package acceptance_test

import (
	"os"
	"testing"
)

func TestConditionalLogApplicationRace(t *testing.T) {
	source, err := os.ReadFile("../../examples/prototype/conditional_log.gox")
	if err != nil {
		t.Fatal(err)
	}
	macroRaceModule(t, string(source), `package main
import("bytes";"encoding/json";"reflect";"testing";"log/slog";"errors";"context")
func TestConditionalFieldsAndErrorChain(t *testing.T){
 old:=slog.Default();defer slog.SetDefault(old)
 for _,level:=range []slog.Level{slog.LevelError,slog.LevelInfo}{
  var out bytes.Buffer;slog.SetDefault(slog.New(slog.NewJSONHandler(&out,&slog.HandlerOptions{Level:level})))
  trace:=[]string{};cause:=errors.New("down");failure:=FromRequestFailure(cause)
  emitRequest(func()bool{trace=append(trace,"enabled");return slog.Default().Enabled(context.Background(),slog.LevelInfo)},func()string{trace=append(trace,"message");return "request"},func()int{trace=append(trace,"request_id");return 41},failure)
  want:=[]string{"enabled"};if level==slog.LevelInfo{want=append(want,"message","request_id")}
  if !reflect.DeepEqual(trace,want){t.Fatalf("order/count=%v",trace)}
  if level==slog.LevelError{if out.Len()!=0{t.Fatal(out.String())};continue}
  var event map[string]any;if err:=json.Unmarshal(out.Bytes(),&event);err!=nil{t.Fatal(err)}
  if event["request_id"]!=float64(41)||event["msg"]!="request"||event["error"]!="request failed"{t.Fatal(event)}
  var typed RequestFailure;if !errors.Is(failure,cause)||!errors.As(failure,&typed)||typed.Cause!=cause{t.Fatal("lost error chain")}
 }
}
`)
}
