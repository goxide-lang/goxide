package lower_test

import "testing"

func TestTraitTemplateExplicitPrivateAliasArguments(t *testing.T) {
	runPrivateAssociatedFixture(t, map[string]string{
		"protocol/lib.gox": `package protocol
pub trait Initial { func initial() Self }
impl[T any] Initial for Option[T] { func initial() Self { return Option[T]::None } }
pub func Fresh[T Initial]() T { return T::initial() }
pub func Catch[T Initial](caught *bool) { *caught=recover()!=nil; _=T::initial() }
`,
		"model/lib.gox": `package model
struct hidden { N int }
pub type Secret=Option[hidden]
pub func Empty(value Secret) bool { return match value { Option[hidden]::None=>true, Option[hidden]::Some(_)=>false } }
`,
		"main.gox": `package main
use fmt;
use associatedfixture::protocol;
use associatedfixture::model;
func main() {
    direct := protocol::Fresh[model::Secret]()
    saved := protocol::Fresh[model::Secret]
    paren := (protocol::Fresh[model::Secret])()
    directCaught,savedCaught := false,false
    func(){defer protocol::Catch[model::Secret](&directCaught);panic("direct")}()
    catch := protocol::Catch[model::Secret]
    func(){defer catch(&savedCaught);panic("saved")}()
    fmt::Println(model::Empty(direct), model::Empty(saved()), model::Empty(paren),directCaught,savedCaught)
}
`}, "true true true true true\n")
}
