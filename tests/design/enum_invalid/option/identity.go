package invalididentity

import e "example.org/hgodesigncheck/enumdemo"

var _ e.Option[int] = e.Some[string]{Value: "wrong"}
