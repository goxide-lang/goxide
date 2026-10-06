package probe
import "testing"
func TestShape(t *testing.T){x:=Run();if len(x)!=1||x["x"]==nil||(*x["x"])()!=7{t.Fatal(x)}}
