//go:build !js

package procmacro

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/sys"
)

// WASIHost owns a runtime and a bounded FIFO cache of compiled artifacts.
// Sessions have independent execution budgets and always use fresh instances.
type WASIHost struct {
	ctx       context.Context
	cancel    context.CancelFunc
	runtime   wazero.Runtime
	diskCache wazero.CompilationCache
	cacheDir  string
	traceDir  string

	mu          sync.Mutex
	active      bool
	activeDone  chan struct{}
	closed      bool
	closeOnce   sync.Once
	closeErr    error
	invocation  time.Duration
	compilation time.Duration
	defaultRun  *WASISession
	compiled    map[[sha256.Size]byte]wasiCompiled
	order       [][sha256.Size]byte
	cacheBytes  int
	stats       WASIHostStats
}

// WASISession owns cancellation and the cumulative budget for one compiler
// request. Closing a session does not discard the host's compiled artifacts.
type WASISession struct {
	host   *WASIHost
	ctx    context.Context
	cancel context.CancelFunc
	spent  time.Duration // Protected by host.mu.
}

type wasiCompiled struct {
	module wazero.CompiledModule
	bytes  int
}

// NewWASIHost creates an in-process runtime with only the fixed macro profile.
// Cancellation closes an executing guest, including its runtime and package init.
func NewWASIHost(ctx context.Context) (*WASIHost, error) {
	return NewWASIHostWithBudgets(ctx, MaxInvocationTime, MaxCompilationTime)
}

// NewWASIHostWithBudgets creates a runtime with explicit execution budgets.
// Compiler requests use NewWASIHost; tests can account for instrumentation cost.
func NewWASIHostWithBudgets(ctx context.Context, invocation, cumulative time.Duration) (*WASIHost, error) {
	if ctx == nil {
		return nil, fmt.Errorf("procedural macro host requires a context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	cache, directory, cacheErr := newWASICompilationCache()
	runtime, err := newWASIRuntime(ctx, cache)
	if err != nil {
		cancel()
		if cache != nil {
			_ = cache.Close(context.Background())
		}
		return nil, err
	}
	host := &WASIHost{ctx: ctx, cancel: cancel, runtime: runtime, diskCache: cache, cacheDir: directory,
		traceDir: os.Getenv("GOXIDE_WASM_TRACE_DIR"), invocation: invocation, compilation: cumulative,
		compiled: map[[sha256.Size]byte]wasiCompiled{}, stats: WASIHostStats{DiskCache: cache != nil, CacheFallback: cacheErr != nil}}
	defaultCtx, defaultCancel := context.WithCancel(ctx)
	host.defaultRun = &WASISession{host: host, ctx: defaultCtx, cancel: defaultCancel}
	return host, nil
}

func newWASIRuntime(ctx context.Context, cache wazero.CompilationCache) (wazero.Runtime, error) {
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithCompilationCache(cache).
		WithCoreFeatures(api.CoreFeaturesV2).
		WithMemoryLimitPages(MaxMemoryPages).
		WithCloseOnContextDone(true))
	if err := installWASIProfile(ctx, runtime); err != nil {
		_ = runtime.Close(context.Background())
		return nil, err
	}
	return runtime, nil
}

// Close cancels active execution, waits for it to finish, and releases the
// runtime and compiled artifacts. Concurrent and repeated calls share one close.
func (host *WASIHost) Close() error {
	host.closeOnce.Do(func() {
		host.mu.Lock()
		host.closed = true
		done := host.activeDone
		host.mu.Unlock()
		host.cancel()
		if host.defaultRun != nil {
			host.defaultRun.Close()
		}
		if done != nil {
			<-done
		}
		host.closeErr = host.runtime.Close(context.Background())
		if host.diskCache != nil {
			host.closeErr = errors.Join(host.closeErr, host.diskCache.Close(context.Background()))
		}
		host.compiled, host.order, host.cacheBytes = nil, nil, 0
		host.writeStats()
	})
	return host.closeErr
}

// NewSession starts an independent compiler request using this host's cache.
// At most one invocation may execute across all sessions; concurrent Run calls
// return an error instead of retaining a queue of requests and input artifacts.
func (host *WASIHost) NewSession(ctx context.Context) (*WASISession, error) {
	if ctx == nil {
		return nil, fmt.Errorf("procedural macro session requires a context")
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.closed {
		return nil, fmt.Errorf("procedural macro host is closed")
	}
	if err := host.ctx.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	return &WASISession{host: host, ctx: ctx, cancel: cancel}, nil
}

// Close cancels this session's active invocation and rejects subsequent runs.
// It is safe to call repeatedly. Host.Close waits for active execution to finish.
func (session *WASISession) Close() {
	session.cancel()
}

// Run accepts one frozen SDK request and returns its admitted response bytes.
// Error diagnostics remain protocol responses so the expansion layer can retain
// their ordered diagnostics and precise spans. Calls share the original compiler
// request's cumulative budget; long-lived callers should create a session.
func (host *WASIHost) Run(wasm []byte, request pm.Request) ([]byte, error) {
	return host.defaultRun.Run(wasm, request)
}

// Run accepts a frozen SDK request within this session's execution budget.
func (session *WASISession) Run(wasm []byte, request pm.Request) (response []byte, resultErr error) {
	host := session.host
	host.mu.Lock()
	if host.closed {
		host.mu.Unlock()
		return nil, fmt.Errorf("procedural macro host is closed")
	}
	if err := host.ctx.Err(); err != nil {
		host.mu.Unlock()
		return nil, fmt.Errorf("procedural macro execution canceled: %w", err)
	}
	if err := session.ctx.Err(); err != nil {
		host.mu.Unlock()
		return nil, fmt.Errorf("procedural macro execution canceled: %w", err)
	}
	if host.active {
		host.mu.Unlock()
		return nil, fmt.Errorf("procedural macro host already has an active invocation")
	}
	remaining := host.compilation - session.spent
	if remaining <= 0 {
		host.mu.Unlock()
		return nil, fmt.Errorf("procedural macro cumulative execution budget exceeded (%s)", host.compilation)
	}
	host.active = true
	host.activeDone = make(chan struct{})
	host.mu.Unlock()
	ctx, cancel := context.WithTimeout(session.ctx, min(host.invocation, remaining))
	stopHostCancel := context.AfterFunc(host.ctx, cancel)
	started := time.Now()
	stats := WASIHostStats{Invocations: 1}
	var state *wasiState
	defer func() {
		if ctx.Err() != nil && (state == nil || state.fatal == nil) {
			response, resultErr = nil, session.budgetError(ctx, remaining)
			if state != nil {
				resultErr = state.executionError(resultErr)
			}
		}
		stopHostCancel()
		cancel()
		host.mu.Lock()
		session.spent += time.Since(started)
		host.stats.Invocations += stats.Invocations
		host.stats.MemoryHits += stats.MemoryHits
		host.stats.ModuleLoads += stats.ModuleLoads
		host.stats.ModuleLoadTime += stats.ModuleLoadTime
		host.stats.ExecutionTime += stats.ExecutionTime
		if stats.CacheFallback {
			host.stats.DiskCache, host.stats.CacheFallback = false, true
		}
		host.active = false
		close(host.activeDone)
		host.activeDone = nil
		host.mu.Unlock()
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var input bytes.Buffer
	if err := pm.WriteRequest(&input, request); err != nil {
		return nil, fmt.Errorf("procedural macro request: %w", err)
	}
	prepared, err := PrepareWASM(wasm)
	if err != nil {
		return nil, err
	}
	id := sha256.Sum256(prepared)
	compiled, cached := host.compiled[id]
	if cached {
		stats.MemoryHits++
	} else {
		for host.cacheBytes+len(prepared) > MaxWASMBytes {
			oldID := host.order[0]
			old := host.compiled[oldID]
			if err := old.module.Close(ctx); err != nil {
				return nil, err
			}
			host.cacheBytes -= old.bytes
			delete(host.compiled, oldID)
			host.order = host.order[1:]
		}
		load := func() (wazero.CompiledModule, error) {
			stats.ModuleLoads++
			started := time.Now()
			defer func() { stats.ModuleLoadTime += time.Since(started) }()
			return host.runtime.CompileModule(ctx, prepared)
		}
		module, err := load()
		if err != nil && host.diskCache != nil && ctx.Err() == nil {
			// Cache I/O and corrupt cache entries must not prevent compilation.
			// Retry once without disk caching, within the original deadline.
			_ = host.runtime.Close(context.Background())
			_ = host.diskCache.Close(context.Background())
			host.diskCache, host.cacheDir = nil, ""
			host.compiled = map[[sha256.Size]byte]wasiCompiled{}
			host.order, host.cacheBytes = nil, 0
			stats.CacheFallback = true
			var runtime wazero.Runtime
			runtime, err = newWASIRuntime(ctx, nil)
			if err == nil {
				host.runtime = runtime
				module, err = load()
			}
		}
		if err != nil {
			return nil, fmt.Errorf("procedural macro WASM validation: %w", err)
		}
		if err := ctx.Err(); err != nil {
			_ = module.Close(context.Background())
			return nil, err
		}
		start := module.ExportedFunctions()["_start"]
		if start == nil || len(start.ParamTypes()) != 0 || len(start.ResultTypes()) != 0 {
			_ = module.Close(ctx)
			return nil, fmt.Errorf("procedural macro WASM requires _start with no parameters or results")
		}
		compiled = wasiCompiled{module, len(prepared)}
		host.compiled[id] = compiled
		host.order = append(host.order, id)
		host.cacheBytes += compiled.bytes
	}
	state = newWASIState(input.Bytes())
	ctx = context.WithValue(ctx, wasiStateKey{}, state)
	executionStarted := time.Now()
	module, runErr := host.runtime.InstantiateModule(ctx, compiled.module, wazero.NewModuleConfig().WithName(""))
	stats.ExecutionTime += time.Since(executionStarted)
	if module != nil {
		_ = module.Close(context.Background())
	}
	if state.fatal != nil {
		return nil, state.executionError(state.fatal)
	}
	if err := ctx.Err(); err != nil {
		return nil, err // The deferred budget check adds the precise cause.
	}
	if runErr != nil {
		var exit *sys.ExitError
		if !errors.As(runErr, &exit) || exit.ExitCode() != 0 {
			return nil, state.executionError(fmt.Errorf("procedural macro execution: %w", runErr))
		}
	}
	response = state.stdout.Bytes()
	if err := ValidateResponse(request, response, state.stderr.Len()); err != nil {
		return nil, state.executionError(err)
	}
	return append([]byte(nil), response...), nil
}

func (session *WASISession) budgetError(ctx context.Context, remaining time.Duration) error {
	host := session.host
	if err := host.ctx.Err(); err != nil {
		return fmt.Errorf("procedural macro execution canceled: %w", err)
	}
	if err := session.ctx.Err(); err != nil {
		return fmt.Errorf("procedural macro execution canceled: %w", err)
	}
	if remaining <= host.invocation {
		return fmt.Errorf("procedural macro cumulative execution budget exceeded (%s): %w", host.compilation, ctx.Err())
	}
	return fmt.Errorf("procedural macro invocation time budget exceeded (%s): %w", host.invocation, ctx.Err())
}
