module trial.example/app

go 1.27

require trial.example/model v0.0.0

replace trial.example/model => ../model

require github.com/goxide-lang/serde v0.0.0-20261023080815-5422034b0fcc

require github.com/goxide-lang/std v0.0.2-0.20261023080713-8db223547de1
