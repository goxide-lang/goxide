package main

import (
	"fmt"

	"github.com/goxide-lang/std/typeinfo"
	"traitfixture/model"
)

func main() {
	value := model.Make[model.Choice]()
	user := model.Make[model.User]()
	fmt.Printf("%T %d %s %d %d %s\n", value, model.Read[model.Choice](value), model.Read[model.User](user), model.ReadWhere[model.Choice](value), model.ReadInt[model.Choice](value), model.ThroughGoxide())
	fmt.Printf("%T %T %d\n", model.ZeroItem[model.Choice](), model.ZeroItem[model.User](), typeinfo.Of[model.Choice]().Make().AsSource().Get())
	oldView := value.AsSource()
	borrowView := model.ForChoice(&value).AsReplace()
	model.Set[model.Choice](&value, 91)
	borrowView.Set(99)
	fmt.Println(oldView.Get(), value.AsSource().Get(), user.AsSource().Get())
	fresh := model.Renew[model.Choice]()
	shifted := model.Shift[model.Choice](&fresh, 5)
	fmt.Println(fresh.AsSource().Get(), shifted.AsSource().Get())
}
