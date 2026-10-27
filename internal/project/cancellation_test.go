package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/config"
)

func TestMain(m *testing.M) {
	if os.Getenv("GOXIDE_TEST_PROJECT_TOOL") == "1" {
		if err := runPlanningTool(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type planningToolReport struct {
	Args []string
	GOOS string
}

func runPlanningTool() error {
	prefix := os.Getenv("GOXIDE_TEST_BLOCK_COMMAND")
	platform := os.Getenv("GOXIDE_TEST_BLOCK_GOOS")
	if (prefix == "all" || strings.HasPrefix(strings.Join(os.Args[1:], " "), prefix)) && (platform == "" || platform == os.Getenv("GOOS")) {
		connection, err := net.Dial("tcp", os.Getenv("GOXIDE_TEST_TOOL_READY"))
		if err != nil {
			return err
		}
		defer connection.Close()
		if err := json.NewEncoder(connection).Encode(planningToolReport{Args: os.Args[1:], GOOS: os.Getenv("GOOS")}); err != nil {
			return err
		}
		// The parent only closes its side during cleanup. A successful test must
		// cancel and reap this blocked process before that cleanup can run.
		_, _ = io.Copy(io.Discard, connection)
		return errors.New("blocking tool was released without process cancellation")
	}
	command := exec.Command(os.Getenv("GOXIDE_TEST_REAL_GO"), os.Args[1:]...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	return command.Run()
}

type blockingPlanningTool struct {
	path        string
	environment []string
	listener    *net.TCPListener
	connection  net.Conn
}

func newBlockingPlanningTool(t *testing.T, command, platform string) *blockingPlanningTool {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	path, err := os.Executable()
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	tool := &blockingPlanningTool{
		path: path, listener: listener,
		environment: append(os.Environ(),
			"GOXIDE_TEST_PROJECT_TOOL=1",
			"GOXIDE_TEST_BLOCK_COMMAND="+command,
			"GOXIDE_TEST_BLOCK_GOOS="+platform,
			"GOXIDE_TEST_TOOL_READY="+listener.Addr().String(),
			"GOXIDE_TEST_REAL_GO="+filepath.Join(runtime.GOROOT(), "bin", "go"),
			"GORACE=atexit_sleep_ms=0",
		),
	}
	t.Cleanup(func() {
		listener.Close()
		if tool.connection != nil {
			tool.connection.Close()
		}
	})
	return tool
}

func (tool *blockingPlanningTool) wait(t *testing.T) planningToolReport {
	t.Helper()
	if err := tool.listener.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	connection, err := tool.listener.Accept()
	if err != nil {
		t.Fatalf("tool never entered the requested blocking command: %v", err)
	}
	tool.connection = connection
	if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var report planningToolReport
	if err := json.NewDecoder(connection).Decode(&report); err != nil {
		t.Fatal(err)
	}
	return report
}

func canceledPlanningResult(t *testing.T, result <-chan error) {
	t.Helper()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("planning cancellation was lost: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled planning did not stop its subprocess")
	}
}

func TestLoadBuildPlansContextCancelsCommandsAndReleasesLocks(t *testing.T) {
	for _, test := range []struct {
		name, command, platform string
		macro                   bool
	}{
		{name: "read module", command: "mod edit"},
		{name: "select modules", command: "list"},
		{name: "download modules", command: "mod download"},
		{name: "macro role", command: "mod edit", platform: "wasip1", macro: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := sourceTempDir(t)
			manifest := roleManifest("example.org/cancel", false)
			if test.macro {
				manifest += "[dependencies.macros]\npath='macros'\n"
				wsWrite(t, root, "macros/"+ManifestName, roleManifest("example.org/macros", true))
			}
			wsWrite(t, root, ManifestName, manifest)
			tool := newBlockingPlanningTool(t, test.command, test.platform)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				err := func() error {
					release, err := LockProject(root)
					if err != nil {
						return err
					}
					defer release()
					_, err = LoadBuildPlansContext(ctx, root, tool.path, nil, false, true, false, "", "linux/amd64", false, false, tool.environment)
					return err
				}()
				result <- err
			}()
			report := tool.wait(t)
			if test.platform != "" && report.GOOS != test.platform {
				t.Fatalf("did not reach nested macro role: %+v", report)
			}
			for _, path := range []string{filepath.Join(root, "goxide.lock.guard"), filepath.Join(root, "target", ".layout-lock")} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("planning did not hold the expected lease: %s: %v", path, err)
				}
			}
			cancel()
			canceledPlanningResult(t, result)
			assertPlanningLocksReleased(t, root)
			// Reacquisition and a real subsequent prepare prove that cancellation
			// did not leave either lease or the temporary planning view behind.
			release, err := LockProject(root)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			if _, err := LoadBuildPlansContext(t.Context(), root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, true, false, "", "linux/amd64", false, false); err != nil {
				t.Fatalf("prepare after cancellation: %v", err)
			}
		})
	}
}

func assertPlanningLocksReleased(t *testing.T, root string) {
	t.Helper()
	for _, path := range []string{filepath.Join(root, "goxide.lock.guard"), filepath.Join(root, "target", ".layout-lock")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("lease remained after cancellation: %s: %v", path, err)
		}
	}
	work, err := filepath.Glob(filepath.Join(root, "target", ".role-plan-*"))
	if err != nil || len(work) != 0 {
		t.Fatalf("temporary planning views remained: %v, %v", work, err)
	}
}

func TestPreparationContextAPIsCancelGo(t *testing.T) {
	for _, test := range []struct {
		name, command string
		run           func(context.Context, string, string, []string) error
	}{
		{name: "read", command: "mod edit", run: func(ctx context.Context, tool, root string, env []string) error {
			_, err := ReadModContext(ctx, tool, root, env)
			return err
		}},
		{name: "import", command: "mod edit", run: func(ctx context.Context, tool, root string, env []string) error {
			_, err := ImportModContext(ctx, tool, root, env)
			return err
		}},
		{name: "environment", command: "env", run: func(ctx context.Context, tool, root string, env []string) error {
			_, err := config.PrepareContext(ctx, tool, root, "linux/amd64", false, env)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := sourceTempDir(t)
			wsWrite(t, root, "go.mod", "module example.org/cancel\n\ngo 1.27\n")
			tool := newBlockingPlanningTool(t, test.command, "")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			result := make(chan error, 1)
			go func() { result <- test.run(ctx, tool.path, root, tool.environment) }()
			tool.wait(t)
			cancel()
			canceledPlanningResult(t, result)
		})
	}
}

func TestLoadBuildPlansContextCancelsGitAndReleasesLocks(t *testing.T) {
	root, dependency := sourceTempDir(t), sourceTempDir(t)
	wsWrite(t, dependency, ManifestName, roleManifest("example.org/dependency", false))
	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Fixture", "-c", "user.email=fixture@example.org", "commit", "--no-gpg-sign", "-qm", "fixture"}} {
		if _, err := git(dependency, args...); err != nil {
			t.Fatal(err)
		}
	}
	revision, err := git(dependency, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	wsWrite(t, root, ManifestName, roleManifest("example.org/cancel", false)+fmt.Sprintf("[dependencies.dep]\ngit=%q\nrev=%q\n", dependency, revision))
	tool := newBlockingPlanningTool(t, "all", "")
	bin := sourceTempDir(t)
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	data, err := os.ReadFile(tool.path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, name), data, 0755); err != nil {
		t.Fatal(err)
	}
	previousPath := os.Getenv("PATH")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+previousPath)
	for _, entry := range tool.environment {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "GOXIDE_TEST_") || key == "GORACE" {
			t.Setenv(key, value)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		err := func() error {
			release, err := LockProject(root)
			if err != nil {
				return err
			}
			defer release()
			_, err = LoadBuildPlansContext(ctx, root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, false, false, "", "linux/amd64", false, false)
			return err
		}()
		result <- err
	}()
	if report := tool.wait(t); !slices.Contains(report.Args, "clone") {
		t.Fatalf("did not enter git acquisition: %+v", report)
	}
	cancel()
	canceledPlanningResult(t, result)
	assertPlanningLocksReleased(t, root)
	t.Setenv("PATH", previousPath)
	if _, err := LoadBuildPlansContext(t.Context(), root, filepath.Join(runtime.GOROOT(), "bin", "go"), nil, false, false, false, "", "linux/amd64", false, false); err != nil {
		t.Fatalf("git preparation after cancellation: %v", err)
	}
}
