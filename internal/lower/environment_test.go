package lower_test

import (
	"os"
	"testing"

	"github.com/goxide-lang/goxide/internal/config"
)

func testConfig(t *testing.T, features ...config.Features) config.Config {
	t.Helper()
	environment, err := config.NewEnvironment(config.HostTarget(), os.Environ())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Environment: environment, Reads: new(config.Reads)}
	if len(features) != 0 {
		cfg.Features = features[0]
	}
	return cfg
}
