package lower_test

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

func TestCorePanicLocationsWithinSourceLines(t *testing.T) {
	goldens, err := filepath.Glob("testdata/core/*/main.golden.go")
	if err != nil || len(goldens) == 0 {
		t.Fatalf("find core goldens: %v (%d files)", err, len(goldens))
	}
	locationPattern := regexp.MustCompile(`\bat ([^"\s]+):([0-9]+)\b`)
	checked := 0
	for _, golden := range goldens {
		t.Run(filepath.Base(filepath.Dir(golden)), func(t *testing.T) {
			generated, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for _, location := range locationPattern.FindAllStringSubmatch(string(generated), -1) {
				if seen[location[0]] {
					continue
				}
				seen[location[0]] = true
				checked++
				sourceName := filepath.Join(filepath.Dir(golden), filepath.Base(location[1]))
				source, err := os.ReadFile(sourceName)
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Count(source, []byte{'\n'})
				if len(source) > 0 && source[len(source)-1] != '\n' {
					lines++
				}
				line, err := strconv.Atoi(location[2])
				if err != nil || line < 1 || line > lines {
					t.Errorf("%s: %s:%s is outside %s lines 1..%d", golden, location[1], location[2], sourceName, lines)
				}
			}
		})
	}
	if checked == 0 {
		t.Fatal("no panic source locations found in core goldens")
	}
}
