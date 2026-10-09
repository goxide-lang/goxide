module trial.example/app

go 1.27

require trial.example/model v0.0.0

replace trial.example/model => ../model

require github.com/goxide-lang/serde v0.0.0-20261009021520-f342a31e20d0

require github.com/goxide-lang/std v0.0.0-20261009053434-9ed27c85088a
