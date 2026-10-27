package compile

import (
	"os"
	"sync"
	"testing"

	"github.com/goxide-lang/goxide/internal/project"
)

func TestCompilerIdentityMatchesExecutableConcurrently(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	compiler, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	want := project.Digest(compiler)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 32 {
		workers.Go(func() {
			<-start
			if got, err := compilerIdentity(); err != nil || got != want {
				t.Errorf("compiler identity = %q, %v; want %q", got, err, want)
			}
		})
	}
	close(start)
	workers.Wait()
}

func BenchmarkCompilerIdentity(b *testing.B) {
	b.Run("uncached", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			executable, err := os.Executable()
			if err != nil {
				b.Fatal(err)
			}
			compiler, err := os.ReadFile(executable)
			if err != nil {
				b.Fatal(err)
			}
			project.Digest(compiler)
		}
	})
	b.Run("cached", func(b *testing.B) {
		if _, err := compilerIdentity(); err != nil {
			b.Fatal(err)
		}
		b.ReportAllocs()
		for b.Loop() {
			if _, err := compilerIdentity(); err != nil {
				b.Fatal(err)
			}
		}
	})
}
