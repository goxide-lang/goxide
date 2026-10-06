package probe
import "testing"
func TestShape(t *testing.T){x:=Run();if len(x)!=1{t.Fatal(x)};for f,n:=range x{if f==nil||(*f)()!=7||n!=1{t.Fatal(x)}}}
