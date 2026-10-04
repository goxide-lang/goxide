module trial.example/app

go 1.27

require (
    github.com/hxsf/goxide v0.0.0
    trial.example/model v0.0.0
)
replace github.com/hxsf/goxide => ../../..
replace trial.example/model => ../model

require github.com/goxide-lang/serde v0.0.0-20261004120911-a1b791e96d66
require github.com/goxide-lang/std v0.0.0-20261004120854-3f2204770fd3
