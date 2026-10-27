package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type rpcPeer struct {
	connection  *Connection
	input       *os.File
	output      *os.File
	reader      *bufio.Reader
	done        chan struct{}
	err         error
	readTimeout time.Duration
}

func startRPC(t *testing.T, handler RequestHandler, notifications NotificationHandler) *rpcPeer {
	t.Helper()
	in, input, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	output, out, err := os.Pipe()
	if err != nil {
		in.Close()
		input.Close()
		t.Fatal(err)
	}
	p := &rpcPeer{connection: NewConnection(in, out), input: input, output: output, reader: bufio.NewReader(output), done: make(chan struct{})}
	go func() {
		p.err = p.connection.Serve(context.Background(), func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			if method == "test/ready" {
				return true, nil
			}
			if handler == nil {
				return nil, &RPCError{Code: CodeMethodNotFound, Message: "unknown method"}
			}
			return handler(ctx, method, params)
		}, notifications)
		close(p.done)
	}()
	t.Cleanup(func() {
		_ = p.connection.Close()
		input.Close()
		output.Close()
		awaitRPC(t, p.done)
	})
	p.send(t, `{"jsonrpc":"2.0","id":"ready","method":"test/ready"}`)
	if got := p.receive(t); string(got.Result) != "true" {
		t.Fatalf("server not ready: %+v", got)
	}
	return p
}

func (p *rpcPeer) send(t *testing.T, body string) {
	t.Helper()
	if err := p.input.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(p.input, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		t.Fatal(err)
	}
}

func (p *rpcPeer) receive(t *testing.T) rpcMessage {
	t.Helper()
	timeout := p.readTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	if err := p.output.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		t.Fatal(err)
	}
	body, err := readRPCFrame(p.reader)
	if err != nil {
		t.Fatal(err)
	}
	var message rpcMessage
	if err := json.Unmarshal(body, &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func awaitRPC(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RPC handler or transport did not finish")
	}
}

func TestRPCCancellationDoesNotBlockNotifications(t *testing.T) {
	started, canceled, changed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := startRPC(t, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method == "hold" {
			close(started)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		}
		return params, nil
	}, func(_ context.Context, method string, _ json.RawMessage) error {
		if method == "textDocument/didChange" {
			close(changed)
		}
		return nil
	})
	p.send(t, `{"jsonrpc":"2.0","id":"sl\u006fw","method":"hold"}`)
	awaitRPC(t, started)
	p.send(t, `{"jsonrpc":"2.0","method":"textDocument/didChange","params":{}}`)
	awaitRPC(t, changed)
	p.send(t, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":"slow"}}`)
	awaitRPC(t, canceled)
	response := p.receive(t)
	var id string
	_ = json.Unmarshal(response.ID, &id)
	if id != "slow" || response.Error == nil || response.Error.Code != CodeRequestCancelled {
		t.Fatalf("cancel response: %+v", response)
	}
	for _, id := range []string{`9007199254740993`, `"9007199254740993"`, `7`, `"7"`} {
		p.send(t, `{"jsonrpc":"2.0","id":`+id+`,"method":"echo","params":{"text":"你好😀"}}`)
		response := p.receive(t)
		if string(response.ID) != id || string(response.Result) != `{"text":"你好😀"}` {
			t.Fatalf("request ID or Unicode response changed: %+v", response)
		}
	}
	p.send(t, `{"jsonrpc":"2.0","method":"exit"}`)
	awaitRPC(t, p.done)
	if p.err != nil {
		t.Fatal(p.err)
	}
}

func TestRPCBoundsRequestExecutionAndQueue(t *testing.T) {
	started := make(chan struct{}, 80)
	var running, maximum atomic.Int32
	p := startRPC(t, func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		active := running.Add(1)
		defer running.Add(-1)
		for old := maximum.Load(); active > old && !maximum.CompareAndSwap(old, active); old = maximum.Load() {
		}
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}, nil)
	for i := 0; i < 8; i++ {
		p.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"hold"}`, i))
	}
	for range 8 {
		awaitRPC(t, started)
	}
	for i := 8; i <= 72; i++ {
		p.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"hold"}`, i))
	}
	response := p.receive(t)
	if string(response.ID) != "72" || response.Error == nil || response.Error.Code != CodeServerBusy {
		t.Fatalf("queue overload did not return an explicit error: %+v", response)
	}
	p.send(t, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":0}}`)
	response = p.receive(t)
	if string(response.ID) != "0" || response.Error == nil || response.Error.Code != CodeRequestCancelled {
		t.Fatalf("reader could not cancel under queue pressure: %+v", response)
	}
	p.send(t, `{"jsonrpc":"2.0","method":"exit"}`)
	awaitRPC(t, p.done)
	if p.err != nil || running.Load() != 0 || maximum.Load() != 8 {
		t.Fatalf("workers did not stay bounded or finish: running=%d, maximum=%d, err=%v", running.Load(), maximum.Load(), p.err)
	}
}

func TestRPCReceiptSequenceProtectsQueuedPositionRequests(t *testing.T) {
	started, changed := make(chan struct{}, 8), make(chan struct{})
	var latestChange atomic.Uint64
	p := startRPC(t, func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "hold" {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		}
		if ReceiveSequence(ctx) < latestChange.Load() {
			return nil, &RPCError{Code: CodeContentModified, Message: "document changed while the request was queued"}
		}
		return "new text", nil
	}, func(ctx context.Context, method string, _ json.RawMessage) error {
		if method == "textDocument/didChange" {
			latestChange.Store(ReceiveSequence(ctx))
			close(changed)
		}
		return nil
	})
	for i := 0; i < 8; i++ {
		p.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"hold"}`, i))
	}
	for range 8 {
		awaitRPC(t, started)
	}
	p.send(t, `{"jsonrpc":"2.0","id":"position","method":"textDocument/hover","params":{"position":{"line":0,"character":3}}}`)
	p.send(t, `{"jsonrpc":"2.0","method":"textDocument/didChange","params":{}}`)
	awaitRPC(t, changed)
	p.send(t, `{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":0}}`)
	responses := make(map[string]rpcMessage)
	for range 2 {
		message := p.receive(t)
		responses[string(message.ID)] = message
	}
	position := responses[`"position"`]
	if position.Error == nil || position.Error.Code != CodeContentModified {
		t.Fatalf("queued position request was bound to newer text: %+v", position)
	}
}

func TestRPCSerializesConcurrentResponsesAndNotifications(t *testing.T) {
	p := startRPC(t, func(_ context.Context, _ string, params json.RawMessage) (any, error) { return params, nil }, nil)
	var notifications sync.WaitGroup
	for i := 0; i < 16; i++ {
		notifications.Go(func() {
			if err := p.connection.Notify(t.Context(), "textDocument/publishDiagnostics", map[string]any{"uri": "file:///你好.gox", "sequence": i}); err != nil {
				t.Error(err)
			}
		})
	}
	for i := 0; i < 8; i++ {
		p.send(t, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"echo","params":{"value":%d}}`, i, i))
	}
	notifications.Wait()
	responses, notices := 0, 0
	for range 24 {
		message := p.receive(t)
		if message.Method != "" {
			notices++
		} else {
			responses++
		}
	}
	if responses != 8 || notices != 16 {
		t.Fatalf("lost or interleaved frames: responses=%d, notifications=%d", responses, notices)
	}
}

func TestRPCProtocolFailureAndEOFReleaseHandlers(t *testing.T) {
	for _, failure := range []string{"eof", "frame", "duplicate"} {
		t.Run(failure, func(t *testing.T) {
			started, canceled := make(chan struct{}), make(chan struct{})
			p := startRPC(t, func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
				close(started)
				<-ctx.Done()
				close(canceled)
				return nil, ctx.Err()
			}, nil)
			p.send(t, `{"jsonrpc":"2.0","id":1,"method":"hold"}`)
			awaitRPC(t, started)
			switch failure {
			case "eof":
				p.input.Close()
			case "frame":
				if _, err := fmt.Fprint(p.input, "Content-Length: invalid\r\n\r\n"); err != nil {
					t.Fatal(err)
				}
			case "duplicate":
				p.send(t, `{"jsonrpc":"2.0","id":1,"method":"hold"}`)
			}
			awaitRPC(t, canceled)
			awaitRPC(t, p.done)
			if failure == "eof" && p.err != nil || failure != "eof" && p.err == nil {
				t.Fatalf("unexpected termination result: %v", p.err)
			}
			if failure == "duplicate" {
				var rpcErr *RPCError
				if !errors.As(p.err, &rpcErr) || rpcErr.Code != CodeInvalidRequest {
					t.Fatalf("duplicate request ID error: %v", p.err)
				}
			}
		})
	}
}

func TestRPCInvalidJSONGetsErrorAndCanContinue(t *testing.T) {
	p := startRPC(t, nil, nil)
	p.send(t, `{"jsonrpc":"2.0",`)
	response := p.receive(t)
	if string(response.ID) != "null" || response.Error == nil || response.Error.Code != CodeParseError {
		t.Fatalf("parse error response: %+v", response)
	}
	p.send(t, `{"jsonrpc":"2.0","id":"unknown","method":"unknown"}`)
	response = p.receive(t)
	if string(response.ID) != `"unknown"` || response.Error == nil || response.Error.Code != CodeMethodNotFound {
		t.Fatalf("connection did not recover after invalid JSON: %+v", response)
	}
}

type blockedRPCWriter struct {
	*io.PipeWriter
	started chan struct{}
}

func (w *blockedRPCWriter) Write(data []byte) (int, error) {
	close(w.started)
	return w.PipeWriter.Write(data)
}

func TestRPCBlockedWriterDoesNotPreventCancellationOrClose(t *testing.T) {
	in, input := io.Pipe()
	output, out := io.Pipe()
	writeStarted := make(chan struct{})
	connection := NewConnection(in, &blockedRPCWriter{PipeWriter: out, started: writeStarted})
	ready, started, canceled, done := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var serveErr error
	go func() {
		serveErr = connection.Serve(context.Background(), func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
			close(started)
			<-ctx.Done()
			close(canceled)
			return nil, ctx.Err()
		}, func(_ context.Context, _ string, _ json.RawMessage) error { close(ready); return nil })
		close(done)
	}()
	t.Cleanup(func() {
		_ = connection.Close()
		input.Close()
		output.Close()
		awaitRPC(t, done)
	})
	send := func(body string) {
		if _, err := fmt.Fprintf(input, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
			t.Fatal(err)
		}
	}
	send(`{"jsonrpc":"2.0","method":"ready"}`)
	awaitRPC(t, ready)
	if err := connection.Notify(t.Context(), "diagnostics", nil); err != nil {
		t.Fatal(err)
	}
	// The unread pipe keeps this first write blocked while the queue fills.
	awaitRPC(t, writeStarted)
	count := 0
	for ; count < 100; count++ {
		if err := connection.Notify(t.Context(), "diagnostics", nil); err != nil {
			break
		}
	}
	if count != 64 {
		t.Fatalf("output queue was not bounded: %d messages accepted", count)
	}
	send(`{"jsonrpc":"2.0","id":1,"method":"hold"}`)
	awaitRPC(t, started)
	send(`{"jsonrpc":"2.0","method":"$/cancelRequest","params":{"id":1}}`)
	awaitRPC(t, canceled)
	awaitRPC(t, done)
	if serveErr == nil || !strings.Contains(serveErr.Error(), "output queue is full") {
		t.Fatalf("blocked writer did not terminate cleanly on response backpressure: %v", serveErr)
	}
}
