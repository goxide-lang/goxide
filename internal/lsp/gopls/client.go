// Package gopls communicates with an unmodified gopls subprocess over LSP.
package gopls

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Notification struct {
	Method string
	Params json.RawMessage
}

type Options struct {
	Path string // Empty selects gopls from PATH.
	Dir  string
	Env  []string // Additional entries override the inherited environment.
	// Notification runs serially, outside the protocol reader. It must return
	// when ctx is canceled. A full 64-message queue terminates the connection.
	Notification func(ctx context.Context, notification Notification)
}

type ResponseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *ResponseError) Error() string { return fmt.Sprintf("gopls: %s (%d)", e.Message, e.Code) }

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

type Client struct {
	cmd    *exec.Cmd
	stdin  *os.File
	stdout *os.File
	stderr tailBuffer
	write  chan struct{}
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu           sync.Mutex
	err          error
	nextID       uint64
	pending      map[string]chan message
	closing      bool
	initializing bool
	initialized  bool
	folders      json.RawMessage
	stopOnce     sync.Once
	notify       chan Notification
	cancelIDs    chan string
}

// Start launches the process. ctx owns its lifetime; canceling it kills the
// process. Initialize must complete before ordinary LSP traffic is sent.
func Start(ctx context.Context, options Options) (*Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := options.Path
	if path == "" {
		path = "gopls"
	}
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		inRead.Close()
		inWrite.Close()
		return nil, err
	}
	c := &Client{
		stdin: inWrite, stdout: outRead, write: make(chan struct{}, 1),
		done: make(chan struct{}), pending: make(map[string]chan message), cancelIDs: make(chan string, 64),
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.cmd = exec.Command(path)
	c.cmd.Dir = options.Dir
	c.cmd.Env = append(os.Environ(), options.Env...)
	c.cmd.Stdin, c.cmd.Stdout, c.cmd.Stderr = inRead, outWrite, &c.stderr
	c.cmd.WaitDelay = time.Second
	err = c.cmd.Start()
	inRead.Close()
	outWrite.Close()
	if err != nil {
		inWrite.Close()
		outRead.Close()
		c.cancel()
		return nil, fmt.Errorf("start gopls: %w", err)
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for {
			select {
			case <-c.ctx.Done():
				return
			case id := <-c.cancelIDs:
				cancelCtx, cancel := context.WithTimeout(c.ctx, time.Second)
				_ = c.Notify(cancelCtx, "$/cancelRequest", map[string]any{"id": json.RawMessage(id)})
				cancel()
			}
		}
	})
	if options.Notification != nil {
		c.notify = make(chan Notification, 64)
		workers.Go(func() {
			for {
				select {
				case <-c.ctx.Done():
					return
				case notification := <-c.notify:
					options.Notification(c.ctx, notification)
				}
			}
		})
	}
	workers.Go(c.read)
	go func() {
		err := c.cmd.Wait()
		c.mu.Lock()
		closing := c.closing
		c.mu.Unlock()
		if err == nil && !closing {
			err = io.ErrUnexpectedEOF
		}
		c.stop(err)
		workers.Wait()
		close(c.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			c.stop(ctx.Err())
		case <-c.done:
		}
	}()
	return c, nil
}

// Initialize preserves the server's capabilities verbatim for the coordinator.
func (c *Client) Initialize(ctx context.Context, params any) (json.RawMessage, error) {
	c.mu.Lock()
	if c.initializing || c.initialized || c.closing {
		c.mu.Unlock()
		return nil, errors.New("gopls initialization has already started or connection is closing")
	}
	c.initializing = true
	c.mu.Unlock()
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	var initialize struct {
		WorkspaceFolders json.RawMessage `json:"workspaceFolders"`
		RootURI          string          `json:"rootUri"`
	}
	if err := json.Unmarshal(data, &initialize); err != nil {
		return nil, err
	}
	if (len(initialize.WorkspaceFolders) == 0 || string(initialize.WorkspaceFolders) == "null") && initialize.RootURI != "" {
		initialize.WorkspaceFolders, _ = json.Marshal([]map[string]string{{"uri": initialize.RootURI, "name": initialize.RootURI}})
	}
	c.mu.Lock()
	c.folders = initialize.WorkspaceFolders
	c.mu.Unlock()
	result, err := c.Request(ctx, "initialize", json.RawMessage(data))
	if err != nil {
		return nil, err
	}
	if err := c.Notify(ctx, "initialized", struct{}{}); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return result, nil
}

func (c *Client) Request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	data, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.nextID++
	id := strconv.FormatUint(c.nextID, 10)
	reply := make(chan message, 1)
	c.pending[id] = reply
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.send(ctx, message{ID: json.RawMessage(id), Method: method, Params: data}); err != nil {
		return nil, err
	}
	select {
	case response := <-reply:
		if response.Error != nil {
			return nil, response.Error
		}
		return response.Result, nil
	case <-ctx.Done():
		select {
		case c.cancelIDs <- id:
		default:
			c.stop(errors.New("gopls cancellation queue is full"))
		}
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, c.connectionError()
	}
}

func (c *Client) Notify(ctx context.Context, method string, params any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.send(ctx, message{Method: method, Params: data})
}

// Close sends shutdown/exit, then reaps the subprocess and notification worker.
// If ctx expires, the process is killed; Done signals completed cleanup.
func (c *Client) Close(ctx context.Context) error {
	c.mu.Lock()
	first, initialized := !c.closing, c.initialized
	c.closing = true
	c.mu.Unlock()
	if first {
		if initialized {
			if _, err := c.Request(ctx, "shutdown", nil); err != nil {
				c.stop(err)
			}
			if err := c.Notify(ctx, "exit", nil); err != nil {
				c.stop(err)
			}
			c.stdin.Close()
		} else {
			c.stop(nil)
		}
	}
	select {
	case <-c.done:
		return c.Err()
	case <-ctx.Done():
		c.stop(ctx.Err())
		return ctx.Err()
	}
}

func (c *Client) Done() <-chan struct{} { return c.done }

func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Stderr returns at most the final 64 KiB of server output.
func (c *Client) Stderr() string { return c.stderr.String() }

func (c *Client) connectionError() error {
	if err := c.Err(); err != nil {
		return err
	}
	return io.ErrClosedPipe
}

func (c *Client) stop(err error) {
	c.stopOnce.Do(func() {
		c.mu.Lock()
		c.err = err
		clear(c.pending)
		c.mu.Unlock()
		c.cancel()
		c.stdin.Close()
		c.stdout.Close()
		_ = c.cmd.Process.Kill()
	})
}

func (c *Client) send(ctx context.Context, msg message) error {
	msg.JSONRPC = "2.0"
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	select {
	case c.write <- struct{}{}:
		defer func() { <-c.write }()
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return c.connectionError()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.ctx.Err() != nil {
		return c.connectionError()
	}
	c.mu.Lock()
	closing := c.closing
	c.mu.Unlock()
	if closing && msg.Method != "" && msg.Method != "shutdown" && msg.Method != "exit" && msg.Method != "$/cancelRequest" {
		return io.ErrClosedPipe
	}
	// Pipe deadlines interrupt a stalled write without spawning a writer per
	// message. A partial frame makes this connection unusable.
	canceled := make(chan struct{})
	stopCancel := context.AfterFunc(ctx, func() {
		_ = c.stdin.SetWriteDeadline(time.Now())
		close(canceled)
	})
	defer func() {
		if !stopCancel() {
			<-canceled
		}
		_ = c.stdin.SetWriteDeadline(time.Time{})
	}()
	frame := append(fmt.Appendf(nil, "Content-Length: %d\r\n\r\n", len(data)), data...)
	_, err = c.stdin.Write(frame)
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		c.stop(err)
	}
	return err
}

func (c *Client) read() {
	reader := bufio.NewReader(c.stdout)
	for {
		msg, err := readMessage(reader)
		if err != nil {
			c.mu.Lock()
			closing := c.closing
			c.mu.Unlock()
			if !closing || !errors.Is(err, io.EOF) {
				c.stop(err)
			}
			return
		}
		if msg.Method != "" {
			if len(msg.ID) != 0 {
				if err := c.answer(msg); err != nil {
					c.stop(err)
					return
				}
			} else if c.notify != nil {
				select {
				case c.notify <- Notification{Method: msg.Method, Params: msg.Params}:
				default:
					c.stop(errors.New("gopls notification queue is full"))
					return
				}
			}
			continue
		}
		c.mu.Lock()
		reply := c.pending[string(msg.ID)]
		delete(c.pending, string(msg.ID))
		c.mu.Unlock()
		if reply != nil {
			reply <- msg
		}
	}
}

func (c *Client) answer(request message) error {
	response := message{ID: request.ID, Result: json.RawMessage("null")}
	switch request.Method {
	case "workspace/configuration":
		var params struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(request.Params, &params); err != nil {
			response.Result = nil
			response.Error = &ResponseError{Code: -32602, Message: err.Error()}
		} else {
			response.Result, _ = json.Marshal(make([]any, len(params.Items)))
		}
	case "workspace/workspaceFolders":
		c.mu.Lock()
		if len(c.folders) != 0 {
			response.Result = c.folders
		}
		c.mu.Unlock()
	case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create", "window/showMessageRequest",
		"workspace/diagnostic/refresh", "workspace/semanticTokens/refresh", "workspace/codeLens/refresh", "workspace/inlayHint/refresh":
	case "workspace/applyEdit":
		response.Result = json.RawMessage(`{"applied":false,"failureReason":"edits require source-level validation by Goxide"}`)
	default:
		response.Result = nil
		response.Error = &ResponseError{Code: -32601, Message: "unsupported server request: " + request.Method}
	}
	return c.send(c.ctx, response)
}

func readMessage(reader *bufio.Reader) (message, error) {
	length := -1
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return message{}, err
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			break
		}
		key, value, ok := strings.Cut(string(line), ":")
		if !ok {
			return message{}, errors.New("invalid gopls frame header")
		}
		if strings.EqualFold(key, "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return message{}, err
			}
		}
	}
	if length < 0 || length > 64<<20 {
		return message{}, fmt.Errorf("invalid gopls Content-Length: %d", length)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return message{}, err
	}
	var msg message
	if err := json.Unmarshal(data, &msg); err != nil {
		return message{}, err
	}
	return msg, nil
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(data)
	const limit = 64 << 10
	if len(data) >= limit {
		b.data = append(b.data[:0], data[len(data)-limit:]...)
	} else {
		if excess := len(b.data) + len(data) - limit; excess > 0 {
			copy(b.data, b.data[excess:])
			b.data = b.data[:len(b.data)-excess]
		}
		b.data = append(b.data, data...)
	}
	return n, nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
