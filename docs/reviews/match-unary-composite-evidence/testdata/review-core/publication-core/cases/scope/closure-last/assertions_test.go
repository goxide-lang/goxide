package probe
import "testing"
func TestProbe(t *testing.T){x:=Run();if x.F()!=255||x.W!=uint64(1)<<63{t.Fatal(x)}}
