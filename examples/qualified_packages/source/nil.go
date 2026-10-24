package source

import "github.com/goxide-lang/std/sum"

func InvalidNested() sum.Result[Mode, error] { return sum.NewOk[Mode, error](nil) }
