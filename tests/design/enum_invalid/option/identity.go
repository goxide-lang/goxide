package invalididentity

import e "example.org/goxidedesigncheck/enumdemo"

var _ e.Option[int] = e.Some[string]{Value: "wrong"}
