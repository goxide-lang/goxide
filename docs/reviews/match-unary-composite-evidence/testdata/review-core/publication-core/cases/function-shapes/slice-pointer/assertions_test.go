package probe
import "testing"
func TestShape(t *testing.T){x:=Run();if len(x)!=1||x[0]==nil||(*x[0])()!=7{t.Fatal(x)}}
