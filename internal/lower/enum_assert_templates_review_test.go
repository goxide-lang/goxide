package lower_test

import (
	"os"
	"strings"
	"testing"

	"github.com/goxide-lang/goxide/internal/lower"
)

func TestEnumAssertionConditionalAliasAndAnonymousPayload(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"library/check.gox": `package library
pub trait Mark{}
impl Mark for int{}
pub enum Box[T any]{Value(T)}
pub type Alias[T any]=Box[T]
pub func checkAlias[T Mark](value Alias[T])bool{__goxide_enum_assert(value);return true}
pub func checkStruct[T Mark](value Box[struct{Item T}])bool{__goxide_enum_assert(value);return true}
`,
		"main.gox": `package main
use fmt;use associatedfixture::library;
func main(){fmt::Println(library::checkAlias(library::Box[int]::Value(7)),library::checkStruct[int](library::Box[struct{Item int}]::Value(struct{Item int}{Item:9})))}
`,
	}, "true true\n")
}

func TestEnumAssertionExpandingRecursionKeepsGoDiagnostic(t *testing.T) {
	for _, constraint := range []string{"any", "Mark"} {
		t.Run(constraint, func(t *testing.T) {
			root := t.TempDir()
			writeDiagnosticModuleFile(t, root, "go.mod", []byte("module fixture\ngo 1.27\n"))
			writeDiagnosticModuleFile(t, root, "main.gox", []byte(`package main
trait Mark{}
impl[T any] Mark for T{}
enum Box[T any]{Value(T)}
func Growing[T `+constraint+`](value Box[T]){__goxide_enum_assert(value);var next Box[[]T];Growing(next)}
func main(){}
`))
			module, err := lower.PrepareModule([]string{root}, "fixture", testConfig(t))
			if module != "" {
				t.Cleanup(func() { os.RemoveAll(module) })
			}
			if err == nil || !strings.Contains(err.Error(), "instantiation cycle") {
				t.Fatalf("growing generic recursion lost its Go diagnostic: %v", err)
			}
		})
	}
}

func TestEnumAssertionConditionalRotationRetainsEveryCheck(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"library/check.gox": `package library
pub trait Mark{}
impl Mark for int{}
impl Mark for string{}
pub enum Box[T any]{Value(T)}
pub func rotate[A Mark,B Mark,C Mark](first Box[A],second Box[B],third Box[C],remaining int)bool{
 __goxide_enum_assert(first)
 if remaining>0{return rotate[B,C,A](second,third,first,remaining-1)}
 return true
}
`,
		"model/value.gox": `package model
use associatedfixture::library;
enum hidden{good}
impl library::Mark for hidden{}
pub func box(valid bool)library::Box[hidden]{var value hidden;if valid{value=hidden::good};return library::Box[hidden]::Value(value)}
`,
		"main.gox": `package main
use fmt;use associatedfixture::library;use associatedfixture::model;
func rejected(call func())(yes bool){defer func(){yes=recover()!=nil}();call();return false}
func main(){
 first:=library::Box[int]::Value(7);second:=library::Box[string]::Value("two")
 good:=library::rotate(first,second,model::box(true),5)
 same:=library::rotate(first,first,first,5)
 badSecond:=rejected(func(){library::rotate(first,model::box(false),second,1)})
 badThird:=rejected(func(){library::rotate(first,second,model::box(false),2)})
 fmt::Println(good,same,badSecond,badThird)
}
`,
	}, "true true true true\n")
}

func TestEnumAssertionConditionalResetRecursionConverges(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{"main.gox": `package main
use fmt;
trait Mark{}
enum Leaf{Good}
impl Mark for Leaf{}
impl Mark for string{}
impl Mark for int{}
impl[T any] Mark for []T{}
enum Box[T any]{Value(T)}
func Reset[A Mark,B Mark](first Box[A],second Box[B],remaining int)bool{
 __goxide_enum_assert(first)
 if remaining>0{return Reset[[]B,int](Box[[]B]::Value([]B{}),Box[int]::Value(1),remaining-1)}
 return true
}
func main(){fmt::Println(Reset[Leaf,string](Box[Leaf]::Value(Leaf::Good),Box[string]::Value("two"),5))}
`}, "true\n")
}

func TestEnumAssertionConditionalSharedArgumentsRetainSlots(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"library/check.gox": `package library
pub trait Mark{}
impl Mark for int{}
pub enum Box[T any]{Empty,Value(T)}
pub func check[A Mark,B Mark](first Box[A],second Box[B])bool{
 __goxide_enum_assert(first)
 __goxide_enum_assert(second)
 return true
}
`,
		"model/value.gox": `package model
use associatedfixture::library;
enum hidden{good}
impl library::Mark for hidden{}
pub func box()library::Box[hidden]{return library::Box[hidden]::Value(hidden::good)}
`,
		"main.gox": `package main
use fmt;use associatedfixture::library;use associatedfixture::model;
func main(){
 value:=library::Box[int]::Value(1)
 hidden:=model::box()
 private,public:=model::box(),library::Box[int]::Value(2)
 public=library::Box[int]::Empty
 fmt::Println(library::check(value,value),library::check(hidden,hidden),library::check(private,public))
}
`,
	}, "true true true\n")
}
