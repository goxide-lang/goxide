//go:build unix

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/compile"
)

func TestDevelopmentDebuggerCleanup(t *testing.T) {
	for _, mode := range []string{"startup cancel", "startup eof", "cancel", "eof", "launch failure", "disconnect", "prepare eof", "prepare backend close", "parent exits"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			t.Cleanup(func() {
				for _, name := range []string{"server.pid", "target.pid", "late.pid", "build.pid"} {
					data, err := os.ReadFile(filepath.Join(root, name))
					if err != nil {
						continue
					}
					pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
					if err != nil {
						continue
					}
					command, _ := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
					if strings.Contains(string(command), root) {
						_ = syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			})
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", "'\\''") + "'" }
			wrapper := filepath.Join(root, "dlv")
			if err := os.WriteFile(wrapper, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestDevelopmentDebuggerProcess$' -- serve "+quote(root)+" "+quote(mode)+"\n"), 0755); err != nil {
				t.Fatal(err)
			}
			unrelated := exec.Command(executable, "-test.run=^TestDevelopmentDebuggerProcess$", "--", "child", filepath.Join(root, "unrelated.pid"))
			if err := unrelated.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = unrelated.Process.Kill(); _ = unrelated.Wait() })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			input, send := io.Pipe()
			defer input.Close()
			defer send.Close()
			finished := make(chan error, 1)
			request := developmentRequest{Delve: wrapper, Input: filepath.Join(root, "missing.gox")}
			if strings.HasPrefix(mode, "prepare") {
				request.Input = filepath.Join(root, "application")
				if err := os.Mkdir(request.Input, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(request.Input, "go.mod"), []byte("module example.org/debugcancel\n\ngo 1.27\n"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(request.Input, "main.gox"), []byte("package main\nfunc main(){}\n"), 0600); err != nil {
					t.Fatal(err)
				}
				goTool := filepath.Join(root, "go")
				if err := os.WriteFile(goTool, []byte("#!/bin/sh\nexec "+quote(executable)+" -test.run='^TestDevelopmentDebuggerProcess$' -- build "+quote(filepath.Join(root, "build.pid"))+"\n"), 0755); err != nil {
					t.Fatal(err)
				}
				request.Compiler = compile.Options{GoTool: goTool, Offline: true}
			}
			go func() {
				finished <- debugDevelopment(ctx, request, input, io.Discard, io.Discard)
			}()
			returned := false
			defer func() {
				cancel()
				if !returned {
					select {
					case <-finished:
					case <-time.After(5 * time.Second):
					}
				}
			}()
			waitFile := func(name string) []byte {
				t.Helper()
				deadline := time.Now().Add(5 * time.Second)
				for time.Now().Before(deadline) {
					if data, err := os.ReadFile(filepath.Join(root, name)); err == nil {
						return data
					}
					time.Sleep(10 * time.Millisecond)
				}
				t.Fatalf("debug helper did not create %s", name)
				return nil
			}
			waitFile("target.pid")
			if !strings.HasPrefix(mode, "startup") {
				waitFile("connected")
			}
			start := time.Now()
			switch mode {
			case "startup cancel", "cancel":
				cancel()
			case "startup eof", "eof", "parent exits":
				_ = send.Close()
			case "launch failure", "disconnect", "prepare eof", "prepare backend close":
				command := "disconnect"
				if mode == "launch failure" || strings.HasPrefix(mode, "prepare") {
					command = "launch"
				}
				data, _ := json.Marshal(map[string]any{"seq": 1, "type": "request", "command": command, "arguments": map[string]any{}})
				if _, err := fmt.Fprintf(send, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(mode, "prepare") {
					waitFile("build.pid")
					start = time.Now()
					if mode == "prepare eof" {
						_ = send.Close()
					}
				}
			}
			select {
			case err := <-finished:
				returned = true
				if err != nil && !strings.Contains(mode, "cancel") {
					t.Error(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("debugger cleanup exceeded its bound")
			}
			t.Logf("cleanup: %s", time.Since(start))
			pids := []string{"server.pid", "target.pid"}
			if strings.HasPrefix(mode, "prepare") {
				pids = append(pids, "build.pid")
			}
			if !strings.HasPrefix(mode, "startup") && mode != "prepare backend close" {
				waitFile("disconnect")
				if mode != "disconnect" {
					pids = append(pids, "late.pid")
				} else {
					waitFile("graceful")
				}
			}
			for _, name := range pids {
				pid, err := strconv.Atoi(strings.TrimSpace(string(waitFile(name))))
				if err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(time.Second)
				for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
					time.Sleep(10 * time.Millisecond)
				}
				if syscall.Kill(pid, 0) == nil {
					t.Errorf("owned %s process %d survived cleanup", name, pid)
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
			if err := unrelated.Process.Signal(syscall.Signal(0)); err != nil {
				t.Fatalf("cleanup killed a process outside the debugger tree: %v", err)
			}
		})
	}
}

func TestDevelopmentDebuggerProcess(t *testing.T) {
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) < 2 {
		return
	}
	writePID := func(name string) { _ = os.WriteFile(name, []byte(strconv.Itoa(os.Getpid())), 0600) }
	if args[0] == "child" || args[0] == "build" {
		writePID(args[1])
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	if args[0] == "branch" {
		root := args[1]
		writePID(filepath.Join(root, "target.pid"))
		for {
			if _, err := os.Stat(filepath.Join(root, "spawn")); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		grandchild := exec.Command(os.Args[0], "-test.run=^TestDevelopmentDebuggerProcess$", "--", "child", filepath.Join(root, "late.pid"))
		grandchild.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := grandchild.Start(); err != nil {
			panic(err)
		}
		for {
			if _, err := os.Stat(filepath.Join(root, "late.pid")); err == nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		_ = os.WriteFile(filepath.Join(root, "spawned"), nil, 0600)
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	root, mode := args[1], args[2]
	writePID(filepath.Join(root, "server.pid"))
	child := func(name string) *exec.Cmd {
		command := exec.Command(os.Args[0], "-test.run=^TestDevelopmentDebuggerProcess$", "--", "child", filepath.Join(root, name))
		if mode == "parent exits" && name == "target.pid" {
			command = exec.Command(os.Args[0], "-test.run=^TestDevelopmentDebuggerProcess$", "--", "branch", root)
		}
		command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := command.Start(); err != nil {
			panic(err)
		}
		return command
	}
	target := child("target.pid")
	if strings.HasPrefix(mode, "startup") {
		time.Sleep(time.Hour)
		os.Exit(0)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	fmt.Println("DAP server listening at:", listener.Addr())
	connection, err := listener.Accept()
	if err != nil {
		panic(err)
	}
	_ = os.WriteFile(filepath.Join(root, "connected"), nil, 0600)
	if mode == "prepare backend close" {
		go func() {
			for {
				if _, err := os.Stat(filepath.Join(root, "build.pid")); err == nil {
					_ = connection.Close()
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}
	reader := bufio.NewReader(connection)
	for {
		data, err := developmentReadDAP(reader)
		if err != nil {
			if mode == "prepare backend close" {
				time.Sleep(time.Hour)
			}
			panic(err)
		}
		var request struct{ Command string }
		_ = json.Unmarshal(data, &request)
		if request.Command != "disconnect" {
			continue
		}
		_ = os.WriteFile(filepath.Join(root, "disconnect"), data, 0600)
		if mode == "parent exits" {
			_ = os.WriteFile(filepath.Join(root, "spawn"), nil, 0600)
			for {
				if _, err := os.Stat(filepath.Join(root, "spawned")); err == nil {
					break
				}
				time.Sleep(time.Millisecond)
			}
			_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		}
		if mode == "disconnect" {
			_ = target.Process.Kill()
			_ = target.Wait()
			_ = os.WriteFile(filepath.Join(root, "graceful"), nil, 0600)
			os.Exit(0)
		}
		_ = child("late.pid")
		time.Sleep(time.Hour)
	}
}
