package gopls

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("GOXIDE_GOPLS_HELPER"); mode != "" {
		if err := helperServer(mode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperServer(mode string) error {
	reader := bufio.NewReader(os.Stdin)
	write := func(msg message) error {
		msg.JSONRPC = "2.0"
		data, _ := json.Marshal(msg)
		_, err := fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(data), data)
		return err
	}
	var held json.RawMessage
	for {
		msg, err := readMessage(reader)
		if err != nil {
			return err
		}
		switch msg.Method {
		case "initialize":
			requests := []struct{ method, params, want string }{
				{"workspace/configuration", `{"items":[{"section":"gopls"},{"section":"other"}]}`, `[null,null]`},
				{"client/registerCapability", `{"registrations":[]}`, `null`},
				{"window/workDoneProgress/create", `{"token":"work"}`, `null`},
				{"workspace/applyEdit", `{"edit":{"changes":{}}}`, `"applied":false`},
				{"workspace/executeCommand", `{"command":"do-not-execute"}`, `unsupported server request`},
			}
			for i, request := range requests {
				id := json.RawMessage(fmt.Sprintf(`"server-%d"`, i))
				if err := write(message{ID: id, Method: request.method, Params: json.RawMessage(request.params)}); err != nil {
					return err
				}
				response, err := readMessage(reader)
				if err != nil {
					return err
				}
				data, _ := json.Marshal(response)
				if string(response.ID) != string(id) || !strings.Contains(string(data), request.want) {
					return fmt.Errorf("response to %s: %s", request.method, data)
				}
			}
			if err := write(message{ID: msg.ID, Result: json.RawMessage(`{"capabilities":{}}`)}); err != nil {
				return err
			}
		case "initialized":
			fmt.Fprint(os.Stderr, strings.Repeat("x", 70<<10)+"stderr-tail")
			if mode == "flood" {
				for i := 0; i < 128; i++ {
					if err := write(message{Method: "test/event", Params: json.RawMessage(`{}`)}); err != nil {
						return err
					}
				}
			}
		case "test/hold":
			held = msg.ID
			if err := write(message{Method: "test/holding", Params: held}); err != nil {
				return err
			}
		case "$/cancelRequest":
			var params struct {
				ID json.RawMessage `json:"id"`
			}
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				return err
			}
			if string(params.ID) != string(held) {
				return fmt.Errorf("cancel ID %s != held ID %s", params.ID, held)
			}
			if err := write(message{ID: held, Error: &ResponseError{Code: -32800, Message: "canceled"}}); err != nil {
				return err
			}
			if err := write(message{Method: "test/canceled", Params: held}); err != nil {
				return err
			}
		case "test/echo":
			if err := write(message{ID: msg.ID, Result: msg.Params}); err != nil {
				return err
			}
		case "test/crash":
			return nil
		case "test/malformed":
			if _, err := fmt.Fprint(os.Stdout, "Content-Length: invalid\r\n\r\n{}"); err != nil {
				return err
			}
		case "shutdown":
			if mode == "unresponsive" {
				continue
			}
			if err := write(message{ID: msg.ID, Result: json.RawMessage("null")}); err != nil {
				return err
			}
		case "exit":
			return nil
		}
	}
}

func startHelper(t *testing.T, mode string, notify func(context.Context, Notification)) *Client {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	lifetime, stop := context.WithCancel(context.Background())
	c, err := Start(lifetime, Options{Path: path, Env: []string{"GOXIDE_GOPLS_HELPER=" + mode}, Notification: notify})
	if err != nil {
		stop()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = c.Close(ctx)
		select {
		case <-c.Done():
		case <-ctx.Done():
			t.Error("helper process or notification worker did not exit")
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx, map[string]any{}); err != nil {
		t.Fatalf("initialize: %v; stderr: %s", err, c.Stderr())
	}
	return c
}

func TestRequestCancellationAndShutdown(t *testing.T) {
	events := make(chan Notification, 4)
	c := startHelper(t, "normal", func(ctx context.Context, n Notification) {
		select {
		case events <- n:
		case <-ctx.Done():
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	result := make(chan error, 1)
	go func() {
		_, err := c.Request(ctx, "test/hold", nil)
		result <- err
	}()
	awaitNotification(t, events, func(n Notification) bool { return n.Method == "test/holding" })
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request: %v", err)
	}
	awaitNotification(t, events, func(n Notification) bool { return n.Method == "test/canceled" })
	value, err := c.Request(t.Context(), "test/echo", "still alive: 你好")
	if err != nil || string(value) != `"still alive: 你好"` {
		t.Fatalf("request after cancellation: %s, %v", value, err)
	}
	closeCtx, closeCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer closeCancel()
	if err := c.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(closeCtx); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
	if tail := c.Stderr(); len(tail) != 64<<10 || !strings.HasSuffix(tail, "stderr-tail") {
		t.Fatalf("stderr tail: %d bytes, missing suffix=%v", len(tail), !strings.HasSuffix(tail, "stderr-tail"))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.pending) != 0 {
		t.Fatalf("pending requests after close: %d", len(c.pending))
	}
}

func TestNotificationBackpressureStopsAndReaps(t *testing.T) {
	c := startHelper(t, "flood", func(ctx context.Context, _ Notification) { <-ctx.Done() })
	select {
	case <-c.Done():
		if err := c.Err(); err == nil || !strings.Contains(err.Error(), "queue is full") {
			t.Fatalf("overflow error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notification backlog blocked protocol reader or cleanup")
	}
}

func TestLifetimeCancellationReleasesPending(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	c, err := Start(ctx, Options{Path: path, Env: []string{"GOXIDE_GOPLS_HELPER=normal"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(t.Context(), map[string]any{}); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := c.Request(t.Context(), "test/hold", nil)
		result <- err
	}()
	cancel()
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("canceled process did not exit")
	}
	if err := <-result; err == nil {
		t.Fatal("pending request succeeded after process cancellation")
	}
}

func TestServerFailureReleasesPending(t *testing.T) {
	for _, method := range []string{"test/crash", "test/malformed"} {
		t.Run(method, func(t *testing.T) {
			c := startHelper(t, "normal", nil)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if _, err := c.Request(ctx, method, nil); err == nil || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("server failure did not release request: %v", err)
			}
			select {
			case <-c.Done():
			case <-ctx.Done():
				t.Fatal("failed server was not reaped")
			}
		})
	}
}

func TestShutdownDeadlineReapsProcess(t *testing.T) {
	c := startHelper(t, "unresponsive", nil)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := c.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown deadline: %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("unresponsive server was not reaped")
	}
}

func awaitNotification(t *testing.T, events <-chan Notification, matches func(Notification) bool) Notification {
	t.Helper()
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()
	for {
		select {
		case n := <-events:
			if matches(n) {
				return n
			}
		case <-timer.C:
			t.Fatal("timed out waiting for protocol notification")
			return Notification{}
		}
	}
}
