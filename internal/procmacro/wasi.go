//go:build !js

package procmacro

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"time"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/sys"
)

// WASIHost owns the macro execution budget for one compiler request. Every Run
// creates a new instance. Compiled artifacts share a bounded FIFO cache.
type WASIHost struct {
	ctx     context.Context
	cancel  context.CancelFunc
	runtime wazero.Runtime

	mu          sync.Mutex
	active      bool
	closed      bool
	spent       time.Duration
	invocation  time.Duration
	compilation time.Duration
	compiled    map[[sha256.Size]byte]wasiCompiled
	order       [][sha256.Size]byte
	cacheBytes  int
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
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCoreFeatures(api.CoreFeaturesV2).
		WithMemoryLimitPages(MaxMemoryPages).
		WithCloseOnContextDone(true))
	host := &WASIHost{ctx: ctx, cancel: cancel, runtime: runtime, invocation: invocation, compilation: cumulative, compiled: map[[sha256.Size]byte]wasiCompiled{}}
	if err := installWASIProfile(ctx, runtime); err != nil {
		_ = host.Close()
		return nil, err
	}
	return host, nil
}

// Close cancels active execution and releases the runtime and compiled artifact.
func (host *WASIHost) Close() error {
	host.mu.Lock()
	if host.closed {
		host.mu.Unlock()
		return nil
	}
	host.closed = true
	host.mu.Unlock()
	host.cancel()
	return host.runtime.Close(context.Background())
}

// Run accepts one frozen SDK request and returns its admitted response bytes.
// Error diagnostics remain protocol responses so the expansion layer can retain
// their ordered diagnostics and precise spans.
func (host *WASIHost) Run(wasm []byte, request pm.Request) (response []byte, resultErr error) {
	host.mu.Lock()
	if host.closed {
		host.mu.Unlock()
		return nil, fmt.Errorf("procedural macro host is closed")
	}
	if host.active {
		host.mu.Unlock()
		return nil, fmt.Errorf("procedural macro host already has an active invocation")
	}
	remaining := host.compilation - host.spent
	if remaining <= 0 {
		host.mu.Unlock()
		return nil, fmt.Errorf("procedural macro cumulative execution budget exceeded (%s)", host.compilation)
	}
	host.active = true
	host.mu.Unlock()
	ctx, cancel := context.WithTimeout(host.ctx, min(host.invocation, remaining))
	defer cancel()
	started := time.Now()
	var state *wasiState
	defer func() {
		host.mu.Lock()
		host.spent += time.Since(started)
		host.active = false
		host.mu.Unlock()
		if ctx.Err() != nil && (state == nil || state.fatal == nil) {
			response, resultErr = nil, host.budgetError(ctx, remaining)
			if state != nil {
				resultErr = state.executionError(resultErr)
			}
		}
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
	if !cached {
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
		module, err := host.runtime.CompileModule(ctx, prepared)
		if err != nil {
			return nil, fmt.Errorf("procedural macro WASM validation: %w", err)
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
	module, runErr := host.runtime.InstantiateModule(ctx, compiled.module, wazero.NewModuleConfig().WithName(""))
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

func (host *WASIHost) budgetError(ctx context.Context, remaining time.Duration) error {
	if err := host.ctx.Err(); err != nil {
		return fmt.Errorf("procedural macro execution canceled: %w", err)
	}
	if remaining <= host.invocation {
		return fmt.Errorf("procedural macro cumulative execution budget exceeded (%s): %w", host.compilation, ctx.Err())
	}
	return fmt.Errorf("procedural macro invocation time budget exceeded (%s): %w", host.invocation, ctx.Err())
}
