package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

const (
	CodeParseError       = -32700
	CodeInvalidRequest   = -32600
	CodeMethodNotFound   = -32601
	CodeInvalidParams    = -32602
	CodeInternalError    = -32603
	CodeServerBusy       = -32000
	CodeRequestCancelled = -32800
	CodeContentModified  = -32801
)

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return e.Message }

type RequestHandler func(context.Context, string, json.RawMessage) (any, error)
type NotificationHandler func(context.Context, string, json.RawMessage) error

type receiveSequenceKey struct{}

// ReceiveSequence orders requests and notifications at receipt, before worker
// scheduling. Services can reject a queued position request if a later change
// already advanced its document version before the handler starts.
func ReceiveSequence(ctx context.Context) uint64 {
	sequence, _ := ctx.Value(receiveSequenceKey{}).(uint64)
	return sequence
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

type rpcRequest struct {
	message rpcMessage
	key     string
	ctx     context.Context
	cancel  context.CancelFunc
}

// Connection bounds request execution to eight workers and 64 queued requests.
// Handlers must honor cancellation; notifications must only record changes and
// schedule work. A writer without Close must not block indefinitely in Write.
type Connection struct {
	reader io.ReadCloser
	writer io.Writer
	work   chan *rpcRequest
	out    chan []byte

	mu        sync.Mutex
	pending   map[string]*rpcRequest
	ctx       context.Context
	cancel    context.CancelFunc
	started   bool
	closed    bool
	err       error
	closeOnce sync.Once
}

func NewConnection(reader io.ReadCloser, writer io.Writer) *Connection {
	return &Connection{
		reader: reader, writer: writer, work: make(chan *rpcRequest, 64),
		out: make(chan []byte, 64), pending: make(map[string]*rpcRequest),
	}
}

// Serve dispatches requests independently of its reader. Notification handling
// preserves input order. A notification handler error terminates the connection;
// the service should handle recoverable input errors before returning.
func (c *Connection) Serve(ctx context.Context, requests RequestHandler, notifications NotificationHandler) error {
	c.mu.Lock()
	if c.started || c.closed {
		c.mu.Unlock()
		return errors.New("LSP connection is already started or closed")
	}
	c.started = true
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()
	var workers sync.WaitGroup
	workers.Go(c.write)
	for range 8 {
		workers.Go(func() { c.runRequests(requests) })
	}
	stop := context.AfterFunc(c.ctx, func() { _ = c.Close() })
	defer func() {
		stop()
		_ = c.Close()
		workers.Wait()
	}()
	err := c.read(notifications)
	c.mu.Lock()
	failure, closed := c.err, c.closed
	c.mu.Unlock()
	if failure != nil {
		return failure
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if closed || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// Notify queues a server notification. A full output queue returns an error;
// response backpressure instead closes the connection so requests are not lost.
func (c *Connection) Notify(ctx context.Context, method string, params any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.enqueue(ctx, rpcMessage{Method: method, Params: data})
}

// Close interrupts reading and writing, cancels pending handlers, and drops
// queued work. Serve returns after its workers finish handling cancellation.
func (c *Connection) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		cancel := c.cancel
		clear(c.pending)
	drainWork:
		for {
			select {
			case <-c.work:
			default:
				break drainWork
			}
		}
	drainOutput:
		for {
			select {
			case <-c.out:
			default:
				break drainOutput
			}
		}
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		_ = c.reader.Close()
		if closer, ok := c.writer.(io.Closer); ok {
			_ = closer.Close()
		}
	})
	return nil
}

func (c *Connection) fail(err error) {
	c.mu.Lock()
	if c.err == nil && !c.closed {
		c.err = err
	}
	c.mu.Unlock()
	_ = c.Close()
}

func (c *Connection) enqueue(ctx context.Context, message rpcMessage) error {
	message.JSONRPC = "2.0"
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(data) > maxRPCBody {
		return errors.New("LSP response exceeds the frame size limit")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || !c.started {
		return io.ErrClosedPipe
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case c.out <- data:
		return nil
	default:
		return errors.New("LSP output queue is full")
	}
}

func (c *Connection) respond(id json.RawMessage, result any, err error) {
	response := rpcMessage{ID: id}
	if err == nil {
		response.Result, err = json.Marshal(result)
	}
	if err != nil {
		var rpcErr *RPCError
		switch {
		case errors.As(err, &rpcErr):
			response.Error = rpcErr
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			response.Error = &RPCError{Code: CodeRequestCancelled, Message: "request canceled"}
		default:
			response.Error = &RPCError{Code: CodeInternalError, Message: err.Error()}
		}
	}
	if err := c.enqueue(c.ctx, response); err != nil {
		c.fail(err)
	}
}

func (c *Connection) write() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case data := <-c.out:
			if c.ctx.Err() != nil {
				return
			}
			frame := append(fmt.Appendf(nil, "Content-Length: %d\r\n\r\n", len(data)), data...)
			n, err := c.writer.Write(frame)
			if err == nil && n != len(frame) {
				err = io.ErrShortWrite
			}
			if err != nil {
				c.fail(err)
				return
			}
		}
	}
}

func (c *Connection) runRequests(handler RequestHandler) {
	for {
		select {
		case <-c.ctx.Done():
			return
		case request := <-c.work:
			var result any
			err := request.ctx.Err()
			if err == nil {
				if handler == nil {
					err = &RPCError{Code: CodeMethodNotFound, Message: "unknown method: " + request.message.Method}
				} else {
					result, err = handler(request.ctx, request.message.Method, request.message.Params)
				}
			}
			if request.ctx.Err() != nil {
				err = request.ctx.Err()
			}
			c.mu.Lock()
			respond := c.pending[request.key] == request
			if respond {
				delete(c.pending, request.key)
			}
			c.mu.Unlock()
			request.cancel()
			if respond {
				c.respond(request.message.ID, result, err)
			}
		}
	}
}

func (c *Connection) read(notifications NotificationHandler) error {
	reader := bufio.NewReader(c.reader)
	var sequence uint64
	for {
		if c.ctx.Err() != nil {
			return c.ctx.Err()
		}
		data, err := readRPCFrame(reader)
		if err != nil {
			return err
		}
		if !utf8.Valid(data) || !json.Valid(data) {
			c.respond(json.RawMessage("null"), nil, &RPCError{Code: CodeParseError, Message: "invalid JSON"})
			continue
		}
		var message rpcMessage
		if err := json.Unmarshal(data, &message); err != nil || message.JSONRPC != "2.0" || message.Method == "" {
			c.respond(json.RawMessage("null"), nil, &RPCError{Code: CodeInvalidRequest, Message: "invalid JSON-RPC request"})
			continue
		}
		sequence++
		messageCtx := context.WithValue(c.ctx, receiveSequenceKey{}, sequence)
		if len(message.ID) == 0 {
			if message.Method == "$/cancelRequest" {
				var params struct {
					ID json.RawMessage `json:"id"`
				}
				if json.Unmarshal(message.Params, &params) == nil {
					if key, err := rpcIDKey(params.ID); err == nil {
						c.mu.Lock()
						request := c.pending[key]
						c.mu.Unlock()
						if request != nil {
							request.cancel()
						}
					}
				}
				continue
			}
			if notifications != nil {
				if err := notifications(messageCtx, message.Method, message.Params); err != nil {
					return err
				}
			}
			if message.Method == "exit" {
				return nil
			}
			continue
		}
		key, err := rpcIDKey(message.ID)
		if err != nil {
			c.respond(json.RawMessage("null"), nil, &RPCError{Code: CodeInvalidRequest, Message: err.Error()})
			continue
		}
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil
		}
		if old := c.pending[key]; old != nil {
			c.mu.Unlock()
			err := &RPCError{Code: CodeInvalidRequest, Message: "duplicate pending request ID"}
			c.fail(err)
			return err
		}
		requestCtx, cancel := context.WithCancel(messageCtx)
		request := &rpcRequest{message: message, key: key, ctx: requestCtx, cancel: cancel}
		c.pending[key] = request
		select {
		case c.work <- request:
			c.mu.Unlock()
		default:
			delete(c.pending, key)
			c.mu.Unlock()
			cancel()
			c.respond(message.ID, nil, &RPCError{Code: CodeServerBusy, Message: "too many pending requests"})
		}
	}
}

func rpcIDKey(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var id any
	if err := decoder.Decode(&id); err != nil {
		return "", err
	}
	switch id := id.(type) {
	case string:
		return "s:" + id, nil
	case json.Number:
		return "n:" + id.String(), nil
	default:
		return "", errors.New("request ID must be a string or number")
	}
}

const maxRPCBody = 64 << 20

func readRPCFrame(reader *bufio.Reader) ([]byte, error) {
	length, headerBytes := -1, 0
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && (headerBytes != 0 || len(line) != 0) {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
		headerBytes += len(line)
		if headerBytes > 8192 {
			return nil, errors.New("LSP frame headers exceed 8 KiB")
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			break
		}
		key, value, ok := strings.Cut(string(line), ":")
		if !ok {
			return nil, errors.New("invalid LSP frame header")
		}
		if strings.EqualFold(key, "Content-Length") {
			if length != -1 {
				return nil, errors.New("duplicate LSP Content-Length header")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length < 0 || length > maxRPCBody {
		return nil, fmt.Errorf("invalid LSP Content-Length: %d", length)
	}
	data := make([]byte, length)
	_, err := io.ReadFull(reader, data)
	return data, err
}
