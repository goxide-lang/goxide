package probe
import("testing";"reflect")
func want(t *testing.T,x []string){t.Helper();if !reflect.DeepEqual(trace,x){t.Fatalf("trace=%v want=%v",trace,x)};trace=nil}
func TestOrderedMapNested(t *testing.T){trace=nil;m:=Ordered();if len(m)!=2||m[0].N!=10||m[1].N!=20||!reflect.DeepEqual(m[1].Next,[]int{30}){t.Fatal(m)};want(t,[]string{"k0","v0","s1","g0","g1","k1","s2","v1","last"})}
func TestQuestionStopsKeyAndValue(t *testing.T){for _,tt:=range []struct{where int;events []string}{{1,[]string{"k0","v0","s1","key","defer"}},{2,[]string{"k0","v0","s1","key","s2","value","defer"}},{0,[]string{"k0","v0","s1","key","s2","value","k2","v2","done","defer"}}}{trace=nil;if !BuildOK(tt.where){t.Fatal("value/cause",tt.where)};want(t,tt.events)}}
func TestNearestUserClosure(t *testing.T){trace=nil;if Closure()!=7{t.Fatal("wrong function exited")};want(t,[]string{"before","checked","inner-defer","outer-after","outer-defer"})}
func TestNilIdentity(t *testing.T){a,b:=TypedNils(NewEA()),TypedNils(NewEB());if len(a)!=1||a[0]!=nil||len(b)!=1||b[0]==nil{t.Fatal(a,b)};v,ok:=b[0].(*int);if !ok||v!=nil{t.Fatal(b)}}
func TestInvalidEnumTiming(t *testing.T){trace=nil;panicked:=false;func(){defer func(){panicked=recover()!=nil}();Invalid(nil)}();if !panicked{t.Fatal("invalid enum accepted")};want(t,[]string{"before","defer"})}
func TestNamedSlotAndDirectRecover(t *testing.T){trace=nil;seen=0;r:=Named();if r.N!=8||seen!=7{t.Fatal(r,seen)};want(t,[]string{"explode","defer"})}
func TestLoopTargets(t *testing.T){if v:=Loop();v!=22{t.Fatal(v)}}
