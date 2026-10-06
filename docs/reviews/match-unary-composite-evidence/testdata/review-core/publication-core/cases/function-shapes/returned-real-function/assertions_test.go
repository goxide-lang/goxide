package probe
import "testing"
func TestShape(t *testing.T){f:=Run();if f==nil||f()!=-7{t.Fatal("function owner") }}
