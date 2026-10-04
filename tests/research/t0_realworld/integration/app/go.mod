module trial.example/app

go 1.27

require (
    github.com/hxsf/goxide v0.0.0
    trial.example/model v0.0.0
)
replace github.com/hxsf/goxide => ../../..
replace trial.example/model => ../model
