package invalididentity

import e "example.org/goxidedesigncheck/enumdemo"

var _ e.Result[int, string] = e.Ok[int, bool]{Value: 1}
