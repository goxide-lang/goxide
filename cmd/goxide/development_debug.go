package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Delve owns all debugger semantics. This bridge supplies the private compiled
// binary at launch and transports its standard TCP DAP over editor stdio.
func debugDevelopment(ctx context.Context, req developmentRequest, in io.Reader, out, stderr io.Writer) (result error) {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	dlv, err := exec.LookPath(req.Delve)
	if err != nil {
		return fmt.Errorf("Delve executable %q is unavailable; install the official dlv tool or set goxide.dlv: %w", req.Delve, err)
	}
	command := exec.Command(dlv, "dap", "--listen=127.0.0.1:0")
	configureDevelopmentDebugger(command)
	command.WaitDelay = time.Second
	stdout, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return err
	}
	var view *developmentView
	var binaryDir string
	var connection net.Conn
	var sequence int
	disconnected := false
	defer func() {
		if errors.Is(result, context.Canceled) {
			result = context.Cause(ctx)
			if result == io.EOF {
				result = nil
			}
		}
		if err := stopDevelopmentDebugger(command, connection, sequence+1, disconnected); err != nil && result == nil {
			result = err
		}
		_ = stdout.Close()
		cancel(nil)
		if view != nil {
			view.Close()
		}
		if binaryDir != "" {
			os.RemoveAll(binaryDir)
		}
	}()
	requests := make(chan []byte, 1)
	inputDone := make(chan error, 1)
	go func() {
		client := bufio.NewReader(in)
		for {
			data, err := developmentReadDAP(client)
			if err != nil {
				inputDone <- err
				cancel(err)
				return
			}
			select {
			case requests <- data:
			case <-ctx.Done():
				return
			}
		}
	}()
	reader := bufio.NewReader(stdout)
	type startup struct {
		line string
		err  error
	}
	started := make(chan startup, 1)
	go func() {
		line, err := reader.ReadString('\n')
		started <- startup{line, err}
	}()
	var line string
	select {
	case answer := <-started:
		line, err = answer.line, answer.err
	case err := <-inputDone:
		if err == io.EOF {
			return nil
		}
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("start Delve DAP: %w", err)
	}
	const prefix = "DAP server listening at: "
	if !strings.HasPrefix(line, prefix) {
		return fmt.Errorf("unexpected Delve startup response: %s", strings.TrimSpace(line))
	}
	connection, err = (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimSpace(strings.TrimPrefix(line, prefix)))
	if err != nil {
		return err
	}
	writer := &developmentDAPWriter{out: out}
	go func() {
		buffer := make([]byte, 4096)
		for {
			n, err := reader.Read(buffer)
			if n > 0 {
				_ = writer.send(map[string]any{"seq": 0, "type": "event", "event": "output", "body": map[string]any{"category": "stdout", "output": string(buffer[:n])}})
			}
			if err != nil {
				return
			}
		}
	}()
	backendDone := make(chan error, 1)
	go func() {
		backend := bufio.NewReader(connection)
		for {
			message, err := developmentReadDAP(backend)
			if err != nil {
				backendDone <- err
				cancel(err)
				return
			}
			if err := writer.raw(message); err != nil {
				backendDone <- err
				cancel(err)
				return
			}
		}
	}()
	for {
		var message []byte
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-backendDone:
			if err == io.EOF {
				return nil
			}
			return err
		case err := <-inputDone:
			if err == io.EOF {
				return nil
			}
			return err
		case message = <-requests:
		}
		var request struct {
			Seq           int
			Type, Command string
			Arguments     map[string]any
		}
		if err := json.Unmarshal(message, &request); err != nil {
			return err
		}
		if request.Seq > sequence {
			sequence = request.Seq
		}
		if request.Type == "request" && request.Command == "launch" {
			if view != nil {
				_ = writer.failure(request.Seq, request.Command, fmt.Errorf("a debug session has already been launched"))
				continue
			}
			var logs bytes.Buffer
			view, err = prepareDevelopment(ctx, req, &logs)
			if err == nil {
				binaryDir, err = os.MkdirTemp("", "goxide-debug-")
			}
			binary := filepath.Join(binaryDir, "program")
			if err == nil {
				err = view.buildDebug(ctx, req.Package, req.Test, binary, &logs)
			}
			if logs.Len() > 0 {
				_ = writer.send(map[string]any{"seq": 0, "type": "event", "event": "output", "body": map[string]any{"category": "console", "output": logs.String()}})
			}
			if err != nil {
				_ = writer.failure(request.Seq, request.Command, err)
				return nil
			}
			arguments := request.Arguments
			if arguments == nil {
				arguments = map[string]any{}
			}
			arguments["mode"], arguments["program"] = "exec", binary
			arguments["outputMode"] = "remote"
			if arguments["cwd"] == nil {
				arguments["cwd"] = view.SourceRoot
			}
			if req.Test {
				args, _ := arguments["args"].([]any)
				if req.Run != "" {
					args = append([]any{"-test.run", req.Run}, args...)
				}
				arguments["args"] = args
			}
			var envelope map[string]any
			if err := json.Unmarshal(message, &envelope); err != nil {
				return err
			}
			envelope["arguments"] = arguments
			message, err = json.Marshal(envelope)
			if err != nil {
				return err
			}
		}
		_ = connection.SetWriteDeadline(time.Now().Add(time.Second))
		if _, err := fmt.Fprintf(connection, "Content-Length: %d\r\n\r\n", len(message)); err != nil {
			return err
		}
		if _, err := connection.Write(message); err != nil {
			return err
		}
		if request.Command == "disconnect" {
			disconnected = true
			return nil
		}
	}
}

func (v *developmentView) buildDebug(ctx context.Context, selected string, test bool, binary string, log io.Writer) error {
	if selected != "" {
		packages, err := v.packages(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, item := range packages {
			found = found || item.ImportPath == selected
		}
		if !found {
			return fmt.Errorf("debug package %q is not part of the selected project", selected)
		}
	} else {
		selected = v.Entry
	}
	if v.Single != "" {
		selected = v.Entry
	}
	args := []string{"build"}
	if test {
		args = []string{"test", "-c"}
	}
	args = append(args, developmentFlags(v.Tags)...)
	args = append(args, "-gcflags=all=-N -l", "-o", binary, selected)
	command := v.command(ctx, args...)
	command.Stdout, command.Stderr = log, log
	return command.Run()
}

type developmentDAPWriter struct {
	mu  sync.Mutex
	out io.Writer
}

func (w *developmentDAPWriter) raw(message []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := fmt.Fprintf(w.out, "Content-Length: %d\r\n\r\n", len(message)); err != nil {
		return err
	}
	_, err := w.out.Write(message)
	return err
}

func (w *developmentDAPWriter) send(message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return w.raw(data)
}

func (w *developmentDAPWriter) failure(seq int, command string, err error) error {
	return w.send(map[string]any{"seq": 0, "type": "response", "request_seq": seq, "command": command, "success": false, "message": err.Error()})
}

func developmentReadDAP(reader *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if line == "\r\n" || line == "\n" {
			break
		}
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && strings.EqualFold(key, "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length < 0 || length > 64<<20 {
		return nil, fmt.Errorf("invalid DAP content length %d", length)
	}
	message := make([]byte, length)
	_, err := io.ReadFull(reader, message)
	return message, err
}
