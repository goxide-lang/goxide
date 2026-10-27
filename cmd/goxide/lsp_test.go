package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/goxide-lang/goxide/internal/source"
)

// This peer runs the actual stamped CLI, so stdout framing also detects logging
// from Cobra, startup, project preparation, and process shutdown.
type lspCLIProcess struct {
	command *exec.Cmd
	input   *os.File
	output  *os.File
	reader  *bufio.Reader
	stderr  bytes.Buffer
	done    chan struct{}
	err     error
	next    int
}

type lspCLIMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

func startLSPCLI(t *testing.T, binary, input string) *lspCLIProcess {
	t.Helper()
	inputRead, inputWrite, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outputRead, outputWrite, err := os.Pipe()
	if err != nil {
		inputRead.Close()
		inputWrite.Close()
		t.Fatal(err)
	}
	p := &lspCLIProcess{input: inputWrite, output: outputRead, reader: bufio.NewReader(outputRead), done: make(chan struct{})}
	p.command = exec.Command(binary, "lsp", "--gopls", "off", "--offline", "--go", filepath.Join(runtime.GOROOT(), "bin", "go"), input)
	p.command.Env = localEnv()
	p.command.Stdin, p.command.Stdout, p.command.Stderr = inputRead, outputWrite, &p.stderr
	p.command.WaitDelay = time.Second
	err = p.command.Start()
	inputRead.Close()
	outputWrite.Close()
	if err != nil {
		inputWrite.Close()
		outputRead.Close()
		t.Fatal(err)
	}
	go func() {
		p.err = p.command.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		p.input.Close()
		p.output.Close()
		select {
		case <-p.done:
		default:
			_ = p.command.Process.Kill()
			select {
			case <-p.done:
			case <-time.After(5 * time.Second):
				t.Error("goxide lsp process was not reaped")
			}
		}
	})
	return p
}

func (p *lspCLIProcess) send(t *testing.T, method string, params any, id int) {
	t.Helper()
	message := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if id != 0 {
		message["id"] = id
	}
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.input.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(p.input, "Content-Length: %d\r\n\r\n%s", len(data), data); err != nil {
		t.Fatal(err)
	}
}

func (p *lspCLIProcess) receive(t *testing.T) (lspCLIMessage, error) {
	t.Helper()
	if err := p.output.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return lspCLIMessage{}, err
	}
	line, err := p.reader.ReadString('\n')
	if err != nil {
		if err == io.EOF && line != "" {
			return lspCLIMessage{}, fmt.Errorf("unframed stdout at EOF: %q", line)
		}
		return lspCLIMessage{}, err
	}
	const header = "Content-Length: "
	if !strings.HasPrefix(line, header) || !strings.HasSuffix(line, "\r\n") {
		return lspCLIMessage{}, fmt.Errorf("unframed stdout: %q", line)
	}
	length, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(line, header), "\r\n"))
	if err != nil || length < 0 || length > 8<<20 {
		return lspCLIMessage{}, fmt.Errorf("invalid Content-Length: %q", line)
	}
	line, err = p.reader.ReadString('\n')
	if err != nil || line != "\r\n" {
		return lspCLIMessage{}, fmt.Errorf("invalid frame separator: %q (%v)", line, err)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(p.reader, data); err != nil {
		return lspCLIMessage{}, err
	}
	var message lspCLIMessage
	if err := json.Unmarshal(data, &message); err != nil {
		return message, err
	}
	if message.JSONRPC != "2.0" {
		return message, fmt.Errorf("invalid JSON-RPC response: %s", data)
	}
	return message, nil
}

func (p *lspCLIProcess) call(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	p.next++
	p.send(t, method, params, p.next)
	for {
		message, err := p.receive(t)
		if err != nil {
			t.Fatal(err)
		}
		if len(message.ID) == 0 {
			if message.Method == "" {
				t.Fatalf("response has neither ID nor notification method: %+v", message)
			}
			continue
		}
		if string(message.ID) != strconv.Itoa(p.next) || len(message.Error) != 0 {
			t.Fatalf("%s response: %+v", method, message)
		}
		return message.Result
	}
}

func TestLSPCLIStdio(t *testing.T) {
	// A non-default release identity catches a serverInfo version hardcoded to
	// dev even when --version and the SDK identity were stamped correctly.
	const version = "lsp-integration-test"
	binary := filepath.Join(t.TempDir(), "goxide")
	build := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "run", "./scripts/build.go", "-o", binary, "-ldflags=-X main.version="+version)
	build.Dir, build.Env = "../..", localEnv()
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build stamped CLI: %v\n%s", err, output)
	}
	t.Run("version", func(t *testing.T) {
		command := exec.Command(binary, "--version")
		command.Env = localEnv()
		if output, err := command.CombinedOutput(); err != nil || string(output) != fmt.Sprintf("goxide %s (%s)\n", version, runtime.Version()) {
			t.Fatalf("stamped CLI version: %v\n%s", err, output)
		}
	})
	t.Run("source_with_gopls_off", func(t *testing.T) {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(root, "main.gox")
		original := "package main\r\nfunc value() int { return 41 }\r\nfunc main() { _ = \"😀\"; println(value()) }\r\n"
		if err := os.WriteFile(name, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		uri := (&url.URL{Scheme: "file", Path: filepath.ToSlash(name)}).String()
		peer := startLSPCLI(t, binary, name)
		initialized := peer.call(t, "initialize", map[string]any{"capabilities": map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-16"}}}})
		var response struct {
			ServerInfo   struct{ Name, Version string } `json:"serverInfo"`
			Capabilities struct {
				PositionEncoding       string `json:"positionEncoding"`
				HoverProvider          bool   `json:"hoverProvider"`
				DefinitionProvider     bool   `json:"definitionProvider"`
				DocumentSymbolProvider bool   `json:"documentSymbolProvider"`
				CompletionProvider     any    `json:"completionProvider"`
				TextDocumentSync       struct {
					OpenClose bool `json:"openClose"`
					Change    int  `json:"change"`
				} `json:"textDocumentSync"`
			} `json:"capabilities"`
		}
		if err := json.Unmarshal(initialized, &response); err != nil {
			t.Fatal(err)
		}
		if response.ServerInfo.Name != "goxide" || response.ServerInfo.Version != version {
			t.Fatalf("initialize did not report the built CLI identity: %s", initialized)
		}
		capabilities := response.Capabilities
		if capabilities.PositionEncoding != "utf-16" || !capabilities.HoverProvider || !capabilities.DefinitionProvider || !capabilities.DocumentSymbolProvider || capabilities.CompletionProvider == nil || !capabilities.TextDocumentSync.OpenClose || capabilities.TextDocumentSync.Change != 2 {
			t.Fatalf("initialize capabilities: %s", initialized)
		}
		peer.send(t, "initialized", map[string]any{}, 0)
		peer.send(t, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "goxide", "version": 1, "text": original}}, 0)
		params := func(text string) map[string]any {
			position, err := source.New(text).Position(strings.LastIndex(text, "value()")+1, source.UTF16)
			if err != nil {
				t.Fatal(err)
			}
			return map[string]any{"textDocument": map[string]string{"uri": uri}, "position": position}
		}
		if hover := peer.call(t, "textDocument/hover", params(original)); !bytes.Contains(hover, []byte("int")) {
			t.Fatalf("source hover with gopls off: %s", hover)
		}
		edited := strings.Replace(original, "int { return 41 }", "string { return \"unsaved\" }", 1)
		peer.send(t, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2}, "contentChanges": []map[string]string{{"text": edited}}}, 0)
		if hover := peer.call(t, "textDocument/hover", params(edited)); !bytes.Contains(hover, []byte("string")) || bytes.Contains(hover, []byte("int")) {
			t.Fatalf("unsaved source hover: %s", hover)
		}
		// Reject a stale document version and deliberately emit a recoverable
		// input diagnostic. Every byte on stdout must still belong to a frame.
		peer.send(t, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 1}, "contentChanges": []map[string]string{{"text": original}}}, 0)
		peer.send(t, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3}, "contentChanges": "invalid changes"}, 0)
		if hover := peer.call(t, "textDocument/hover", params(edited)); !bytes.Contains(hover, []byte("string")) {
			t.Fatalf("stale or malformed change replaced the current document: %s", hover)
		}
		preview := peer.call(t, "goxide/generatedSource", map[string]string{"uri": uri})
		var generated struct{ URI, Content string }
		if err := json.Unmarshal(preview, &generated); err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(generated.URI, "goxide-generated:") || !strings.Contains(generated.Content, "func value() string") || !strings.Contains(generated.Content, `return "unsaved"`) {
			t.Fatalf("preview did not use the current document: %s", preview)
		}
		if shutdown := peer.call(t, "shutdown", nil); string(shutdown) != "null" {
			t.Fatalf("shutdown result: %s", shutdown)
		}
		peer.send(t, "exit", nil, 0)
		for {
			message, err := peer.receive(t)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(message.ID) != 0 || message.Method == "" {
				t.Fatalf("unexpected response during shutdown: %+v", message)
			}
		}
		select {
		case <-peer.done:
		case <-time.After(5 * time.Second):
			t.Fatal("goxide lsp did not exit after shutdown/exit")
		}
		if peer.err != nil || !strings.Contains(peer.stderr.String(), "LSP notification:") || strings.Contains(peer.stderr.String(), "gopls unavailable") {
			t.Fatalf("process status or stderr: %v\n%s", peer.err, &peer.stderr)
		}
		if data, err := os.ReadFile(name); err != nil || string(data) != original {
			t.Fatalf("LSP changed the author source: %v\n%s", err, data)
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 1 || entries[0].Name() != "main.gox" {
			t.Fatalf("LSP wrote into the author directory: %v, %v", entries, err)
		}
	})
	t.Run("argument_rejection", func(t *testing.T) {
		for _, test := range []struct {
			name, want string
			args       []string
		}{
			{"extra_inputs", "at most one INPUT", []string{"lsp", "one.gox", "two.gox"}},
			{"unknown_flag", "unknown flag", []string{"lsp", "--not-an-option"}},
			{"missing_target", "needs an argument", []string{"lsp", "--target"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				command := exec.CommandContext(ctx, binary, test.args...)
				command.Env = localEnv()
				var stdout, stderr bytes.Buffer
				command.Stdout, command.Stderr = &stdout, &stderr
				err := command.Run()
				if err == nil || ctx.Err() != nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), test.want) {
					t.Fatalf("args %v: %v, stdout=%q stderr=%q", test.args, err, &stdout, &stderr)
				}
			})
		}
	})
}
