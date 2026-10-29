package gopls

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
)

var ErrStale = errors.New("gopls projection is stale or not synchronized")

type Projection struct {
	Epoch uint64
	Files map[string]string // Current Go file URI to text.
	// Masked identifies owned disk outputs that must not reappear when their
	// current projection is absent. Values are legal Go package names.
	Masked map[string]string
}

// Coordinator owns one process's complete overlay set. It does not classify
// files, map source positions, or publish raw server diagnostics.
type Coordinator struct {
	client *Client
	gate   chan struct{}

	mu       sync.Mutex
	epoch    uint64
	epochCtx context.Context
	cancel   context.CancelFunc
	ready    bool

	// Accessed only while holding gate. A single counter also keeps reopened
	// URI versions increasing without retaining deleted file entries.
	files   map[string]string
	masked  map[string]bool
	version int32
	reload  bool
}

func NewCoordinator(client *Client) *Coordinator {
	ctx, cancel := context.WithCancel(client.ctx)
	return &Coordinator{
		client: client, gate: make(chan struct{}, 1), files: make(map[string]string), masked: make(map[string]bool),
		epochCtx: ctx, cancel: cancel,
	}
}

// Invalidate advances the received-input epoch without waiting for analysis or
// I/O. Older or repeated epochs cannot revive an obsolete projection.
func (c *Coordinator) Invalidate(epoch uint64) {
	c.mu.Lock()
	if epoch <= c.epoch {
		c.mu.Unlock()
		return
	}
	oldCancel := c.cancel
	c.epoch = epoch
	c.ready = false
	c.epochCtx, c.cancel = context.WithCancel(c.client.ctx)
	c.mu.Unlock()
	oldCancel()
}

// Sync applies a complete file set. Files take precedence over Masked at the
// same URI. The caller keeps both maps immutable until Sync returns.
func (c *Coordinator) Sync(ctx context.Context, projection Projection) error {
	c.Invalidate(projection.Epoch)
	requestCtx, cancel, err := c.lock(ctx, projection.Epoch, false)
	if err != nil {
		return err
	}
	defer cancel()
	defer func() { <-c.gate }()
	c.mu.Lock()
	c.ready = false
	c.mu.Unlock()
	desired := make(map[string]string, len(projection.Files)+len(projection.Masked))
	masked := make(map[string]bool, len(projection.Masked))
	for uri, text := range projection.Files {
		desired[uri] = text
	}
	for uri, pkg := range projection.Masked {
		if _, exists := desired[uri]; !exists {
			desired[uri] = "//go:build ignore && !ignore\n\npackage " + pkg + "\n"
			masked[uri] = true
		}
	}
	var uris []string
	for uri := range desired {
		uris = append(uris, uri)
	}
	for uri := range c.files {
		if _, exists := desired[uri]; !exists {
			uris = append(uris, uri)
		}
	}
	slices.Sort(uris)
	for _, uri := range uris {
		if err := c.status(ctx, projection.Epoch, false); err != nil {
			return err
		}
		text, wanted := desired[uri]
		old, opened := c.files[uri]
		if c.masked[uri] && !masked[uri] {
			c.reload = true
		}
		if wanted && opened && old == text {
			c.masked[uri] = masked[uri]
			continue
		}
		// An invalidation between frames stops the old batch. It does not
		// interrupt an already-writing frame and corrupt the shared stream.
		if !wanted {
			err = c.client.Notify(ctx, "textDocument/didClose", map[string]any{
				"textDocument": map[string]any{"uri": uri},
			})
		} else {
			if c.version == math.MaxInt32 {
				return errors.New("gopls document versions exhausted; restart the process")
			}
			c.version++
			if opened {
				err = c.client.Notify(ctx, "textDocument/didChange", map[string]any{
					"textDocument":   map[string]any{"uri": uri, "version": c.version},
					"contentChanges": []any{map[string]any{"text": text}},
				})
			} else {
				err = c.client.Notify(ctx, "textDocument/didOpen", map[string]any{
					"textDocument": map[string]any{"uri": uri, "languageId": "go", "version": c.version, "text": text},
				})
			}
		}
		if err != nil {
			if current := c.status(requestCtx, projection.Epoch, false); current != nil {
				return current
			}
			return err
		}
		if wanted {
			c.files[uri] = text
			c.masked[uri] = masked[uri]
		} else {
			delete(c.files, uri)
			delete(c.masked, uri)
		}
	}
	if err := c.status(ctx, projection.Epoch, false); err != nil {
		return err
	}
	if c.reload {
		// gopls v0.24.0 does not invalidate missing-import metadata when the
		// last excluded package file loses its build constraint. Recreate the
		// workspace views using the completed overlays before querying them.
		c.client.mu.Lock()
		folders := c.client.folders
		c.client.mu.Unlock()
		var list []json.RawMessage
		if err := json.Unmarshal(folders, &list); err != nil || len(list) == 0 {
			return errors.New("restoring a masked Go file requires gopls workspace folders or a process restart")
		}
		if err := c.client.Notify(ctx, "workspace/didChangeWorkspaceFolders", map[string]any{
			"event": map[string]any{"removed": folders, "added": folders},
		}); err != nil {
			return err
		}
		c.reload = false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != projection.Epoch {
		return ErrStale
	}
	c.ready = true
	return nil
}

// Query pins its epoch through the response. Invalidate cancels it promptly;
// even a successful late response cannot be interpreted using a newer epoch.
func (c *Coordinator) Query(ctx context.Context, epoch uint64, method string, params any) (json.RawMessage, error) {
	requestCtx, cancel, err := c.lock(ctx, epoch, true)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer func() { <-c.gate }()
	result, err := c.client.Request(requestCtx, method, params)
	if current := c.status(ctx, epoch, true); current != nil {
		return nil, current
	}
	return result, err
}

// Diagnostics holds the same epoch for the entire set. No overlay update can
// occur between requests; a canceled or stale batch returns no partial results.
func (c *Coordinator) Diagnostics(ctx context.Context, epoch uint64, uris []string) (map[string]json.RawMessage, error) {
	requestCtx, cancel, err := c.lock(ctx, epoch, true)
	if err != nil {
		return nil, err
	}
	defer cancel()
	defer func() { <-c.gate }()
	results := make(map[string]json.RawMessage, len(uris))
	for _, uri := range uris {
		if _, exists := results[uri]; exists {
			continue
		}
		result, err := c.client.Request(requestCtx, "textDocument/diagnostic", map[string]any{
			"textDocument": map[string]any{"uri": uri},
		})
		if current := c.status(ctx, epoch, true); current != nil {
			return nil, current
		}
		if err != nil {
			return nil, fmt.Errorf("diagnostics for %s: %w", uri, err)
		}
		results[uri] = result
	}
	if err := c.status(ctx, epoch, true); err != nil {
		return nil, err
	}
	return results, nil
}

func (c *Coordinator) lock(ctx context.Context, epoch uint64, ready bool) (context.Context, context.CancelFunc, error) {
	c.mu.Lock()
	if epoch != c.epoch || ready && !c.ready {
		c.mu.Unlock()
		return nil, nil, ErrStale
	}
	epochCtx := c.epochCtx
	c.mu.Unlock()
	if err := c.status(ctx, epoch, ready); err != nil {
		return nil, nil, err
	}
	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(epochCtx, cancel)
	cleanup := func() { stop(); cancel() }
	select {
	case c.gate <- struct{}{}:
		if err := c.status(ctx, epoch, ready); err != nil {
			<-c.gate
			cleanup()
			return nil, nil, err
		}
		return requestCtx, cleanup, nil
	case <-requestCtx.Done():
		cleanup()
		if err := c.status(ctx, epoch, ready); err != nil {
			return nil, nil, err
		}
		return nil, nil, requestCtx.Err()
	}
}

func (c *Coordinator) status(ctx context.Context, epoch uint64, ready bool) error {
	if c.client.ctx.Err() != nil {
		return c.client.connectionError()
	}
	c.mu.Lock()
	stale := epoch != c.epoch || ready && !c.ready
	c.mu.Unlock()
	if stale {
		return ErrStale
	}
	return ctx.Err()
}
