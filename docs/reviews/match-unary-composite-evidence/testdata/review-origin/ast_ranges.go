package main
import("encoding/json";"go/ast";"go/parser";"go/token";"os")
func main(){fs:=token.NewFileSet();file,err:=parser.ParseFile(fs,os.Args[len(os.Args)-1],nil,0);if err!=nil{panic(err)};var out []map[string]any
ast.Inspect(file,func(n ast.Node)bool{f,ok:=n.(*ast.FuncLit);if !ok{return true};found:=false;ast.Inspect(f.Body,func(n ast.Node)bool{if id,ok:=n.(*ast.Ident);ok&&id.Name=="innerValue"{found=true};return true});if found{out=append(out,map[string]any{"start":fs.Position(f.Pos()).Offset,"end":fs.Position(f.End()).Offset})};return true});json.NewEncoder(os.Stdout).Encode(out)}
