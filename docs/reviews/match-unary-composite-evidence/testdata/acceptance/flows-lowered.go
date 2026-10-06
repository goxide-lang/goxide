package main
import "errors"
import cfg "packages.example/app/model"
var Trace []string
var stopped=errors.New("checked failure")
func mark(label string,n int)int{Trace=append(Trace,label);return n}
func record(label string){Trace=append(Trace,label)}
func choice(c cfg.Mode)cfg.Mode{record("target");return c}
func guard(n int)bool{record("guard");return n>0}
func checked(ok bool,n int)Result[int,error]{record("checked");if !ok{return Result[int,error]::Err(stopped)};return Result[int,error]::Ok(n)}
type Bundle struct{First int;Values []int;Last int}
func collect(c cfg.Mode,ok bool)Result[Bundle,error]{defer record("defer");b:= Bundle{First:mark("before",1),Values:[]int{-((func() __goxide_match_expr_pending { var __goxide_match_expr_atom_1 = __goxide_match_expr_operand_context(__goxide_match_expr_literal(),"numeric","../flows.gox:12:124");
match  choice(c) {
cfg.Mode::Off => { __goxide_match_expr_atom_1 = (mark("off",0));
 },
cfg.Mode::Workers(n) if guard(n) => { __goxide_match_expr_atom_1 = (checked(ok,n)?);
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_1 = (0);
 },
}
return __goxide_match_expr_atom_1 }())),mark("element-after",2)},Last:mark("field-after",3)};return Result[Bundle,error]::Ok(b)}
func Collect(c cfg.Mode,ok bool)(Bundle,error){match collect(c,ok){Result[Bundle,error]::Ok(v)=>{return v,nil},Result[Bundle,error]::Err(e)=>{return Bundle{},e}};panic("unreachable")}
func mapOrder(c cfg.Mode)map[int]int{return map[int]int{func() __goxide_match_expr_pending { var __goxide_match_expr_atom_2 = __goxide_match_expr_operand_context(__goxide_match_expr_literal(),"direct","../flows.gox:14:57");
match  choice(c) {
cfg.Mode::Off => { __goxide_match_expr_atom_2 = (mark("key1",1));
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_2 = (mark("key1",1));
 },
}
return __goxide_match_expr_atom_2 }():func() __goxide_match_expr_pending { var __goxide_match_expr_atom_3 = __goxide_match_expr_operand_context(__goxide_match_expr_literal(),"direct","../flows.gox:14:141");
match  c {
cfg.Mode::Off => { __goxide_match_expr_atom_3 = (mark("value1",2));
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_3 = (mark("value1",2));
 },
}
return __goxide_match_expr_atom_3 }(),mark("key2",3):mark("value2",4)}}
func receiveOrder(c cfg.Mode,ch <-chan int)[]int{return []int{<-ch,func() __goxide_match_expr_pending { var __goxide_match_expr_atom_4 = __goxide_match_expr_operand_context(__goxide_match_expr_literal(),"direct","../flows.gox:15:68");
match  c {
cfg.Mode::Off => { __goxide_match_expr_atom_4 = (receiveMiddle(ch));
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_4 = (0);
 },
}
return __goxide_match_expr_atom_4 }(),<-ch}}
func receiveMiddle(ch <-chan int)int{if len(ch)!=1{panic("receive moved across match")};return mark("middle",2)}
func Short(c cfg.Mode)bool{v:= (false && !((func() __goxide_match_expr_pending { var __goxide_match_expr_atom_5 = __goxide_match_expr_infer("../flows.gox:17:40");
match  choice(c) {
cfg.Mode::Off => { __goxide_match_expr_atom_5 = (false);
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_5 = (true);
 },
}
return __goxide_match_expr_atom_5 }())));return v}
func DirectRecover()(out bool){defer func(){v:= []bool{!((func() __goxide_match_expr_pending { var __goxide_match_expr_atom_6 = __goxide_match_expr_operand_context(__goxide_match_expr_literal(),"boolean","../flows.gox:18:57");
match  cfg.Mode::Off {
cfg.Mode::Off => { __goxide_match_expr_atom_6 = (recover()==nil);
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_6 = (true);
 },
}
return __goxide_match_expr_atom_6 }()))};out=v[0]}();panic("direct")}
func deferred(c cfg.Mode,ok bool)Result[int,error]{defer record("cleanup");v:= []int{mark("first",1),func() __goxide_match_expr_pending { var __goxide_match_expr_atom_7 = __goxide_match_expr_operand_context(__goxide_match_expr_literal(),"direct","../flows.gox:19:101");
match  c {
cfg.Mode::Off => { __goxide_match_expr_atom_7 = (checked(ok,2)?);
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_7 = (0);
 },
}
return __goxide_match_expr_atom_7 }(),mark("last",3)};defer consume(v);return Result[int,error]::Ok(1)}
func consume([]int){record("registered")}
func Deferred(c cfg.Mode,ok bool)bool{{
var __goxide_match_expr_8 = __goxide_match_expr_operand_context(__goxide_match_expr_return(),"direct","../flows.gox:21:46");
match  deferred(c,ok) {
Result[int,error]::Ok(_) => { __goxide_match_expr_8 = (true);
 },
Result[int,error]::Err(_) => { __goxide_match_expr_8 = (false);
 },
}
return __goxide_match_expr_8
}}
type WithClosure struct{First int;Run func()Result[int,error];Last int}
func Nearest(c cfg.Mode)int{x:=WithClosure{First:match c{cfg.Mode::Off=>mark("outer-first",1),cfg.Mode::Workers(_)=>0},Run:func()Result[int,error]{defer record("inner-defer");v:= []int{func() __goxide_match_expr_pending { var __goxide_match_expr_atom_9 = __goxide_match_expr_operand_context(__goxide_match_expr_literal(),"direct","../flows.gox:23:185");
match  c {
cfg.Mode::Off => { __goxide_match_expr_atom_9 = (checked(false,1)?);
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_9 = (0);
 },
}
return __goxide_match_expr_atom_9 }(),mark("never",9)};return Result[int,error]::Ok(v[0])},Last:match c{cfg.Mode::Off=>mark("outer-last",2),cfg.Mode::Workers(_)=>0}};_=x.Run();return mark("outer",8)}
func Rebind(c cfg.Mode)(int,int){a:=[]int{1};old:=a;replace:=func()int{a=[]int{9};return mark("arm",3)};a[mark("index",0)]= -((func() __goxide_match_expr_pending { var __goxide_match_expr_atom_10 = __goxide_match_expr_operand_context(a[mark("index",0)],"numeric","../flows.gox:24:126");
match  choice(c) {
cfg.Mode::Off => { __goxide_match_expr_atom_10 = (replace());
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_10 = (0);
 },
}
return __goxide_match_expr_atom_10 }()));return old[0],a[0]}
func Bounds(c cfg.Mode){a:=[]int{};a[mark("index",2)]= -((func() __goxide_match_expr_pending { var __goxide_match_expr_atom_11 = __goxide_match_expr_operand_context(a[mark("index",2)],"numeric","../flows.gox:25:57");
match  choice(c) {
cfg.Mode::Off => { __goxide_match_expr_atom_11 = (mark("arm",3));
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_11 = (0);
 },
}
return __goxide_match_expr_atom_11 }()))}
func indexFailure(c cfg.Mode)Result[int,error]{a:=[]int{};a[mark("index",2)]= -((func() __goxide_match_expr_pending { var __goxide_match_expr_atom_12 = __goxide_match_expr_operand_context(a[mark("index",2)],"numeric","../flows.gox:26:80");
match  choice(c) {
cfg.Mode::Off => { __goxide_match_expr_atom_12 = (checked(false,1)?);
 },
cfg.Mode::Workers(_) => { __goxide_match_expr_atom_12 = (0);
 },
}
return __goxide_match_expr_atom_12 }()));return Result[int,error]::Ok(1)}
func IndexFailure(c cfg.Mode)bool{{
var __goxide_match_expr_13 = __goxide_match_expr_operand_context(__goxide_match_expr_return(),"direct","../flows.gox:27:42");
match  indexFailure(c) {
Result[int,error]::Ok(_) => { __goxide_match_expr_13 = (true);
 },
Result[int,error]::Err(_) => { __goxide_match_expr_13 = (false);
 },
}
return __goxide_match_expr_13
}}
