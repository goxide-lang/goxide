//go:build !js

package procmacro

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/tetratelabs/wazero"
)

func TestWASISessionsReuseCompiledCodeWithFreshInstances(t *testing.T) {
	host := wasiHostForTest(t, context.Background())
	request, wasm, want := wasiSessionResponse(t)
	var previous wazero.CompiledModule
	for range 3 {
		session := wasiSessionForTest(t, host, context.Background())
		for range 2 {
			got, err := session.Run(wasm, request)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("fresh instance changed output: %v\n%s", err, got)
			}
			current := wasiCachedModule(t, host, wasm)
			if previous != nil && previous != current {
				t.Fatal("a new session recompiled an already cached artifact")
			}
			previous = current
		}
		session.Close()
		session.Close()
		if _, err := session.Run(wasm, request); !errors.Is(err, context.Canceled) {
			t.Fatalf("closed session admitted another instance: %v", err)
		}
	}
	if _, err := host.Run(wasm, request); err != nil {
		t.Fatalf("session closure changed the default CLI session: %v", err)
	}
}

func TestWASISessionBudgetsAreIndependent(t *testing.T) {
	host := wasiHostForTest(t, context.Background())
	host.invocation, host.compilation = 30*time.Millisecond, 55*time.Millisecond
	wasm, request := wasiLoopModule(false), wasiRequest(t, "loop")
	first := wasiSessionForTest(t, host, context.Background())
	for index, message := range []string{"invocation time budget", "cumulative execution budget", "cumulative execution budget"} {
		if _, err := first.Run(wasm, request); err == nil || !strings.Contains(err.Error(), message) {
			t.Fatalf("first session call %d = %v; want %s", index, err, message)
		}
	}
	compiled := wasiCachedModule(t, host, wasm)
	second := wasiSessionForTest(t, host, context.Background())
	if _, err := second.Run(wasm, request); err == nil || !strings.Contains(err.Error(), "invocation time budget") {
		t.Fatalf("new session inherited an exhausted cumulative budget: %v", err)
	}
	if compiled != wasiCachedModule(t, host, wasm) {
		t.Fatal("guest timeout discarded already validated compiled code")
	}
	responseRequest, responseWASM, want := wasiSessionResponse(t)
	if got, err := second.Run(responseWASM, responseRequest); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("second session cannot run after one invocation timeout: %v", err)
	}
}

func TestWASISessionCancellationAndActiveGate(t *testing.T) {
	for _, closeSession := range []bool{false, true} {
		t.Run(fmt.Sprintf("close_session_%t", closeSession), func(t *testing.T) {
			host := wasiHostForTest(t, context.Background())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			first := wasiSessionForTest(t, host, ctx)
			second := wasiSessionForTest(t, host, context.Background())
			request, wasm, want := wasiSessionResponse(t)
			// Cache a valid artifact before cancellation. It must survive with
			// its compiled identity intact and a fresh guest instance afterward.
			if _, err := first.Run(wasm, request); err != nil {
				t.Fatal(err)
			}
			compiled := wasiCachedModule(t, host, wasm)
			done := make(chan error, 1)
			go func() {
				_, err := first.Run(wasiLoopModule(false), request)
				done <- err
			}()
			wasiWaitActive(t, host, done)
			if _, err := second.Run(wasm, request); err == nil || !strings.Contains(err.Error(), "active invocation") {
				t.Fatalf("another session admitted a concurrent instance: %v", err)
			}
			if closeSession {
				first.Close()
				first.Close()
			} else {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("session cancellation lost its cause: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("session cancellation did not stop guest execution")
			}
			if got, err := second.Run(wasm, request); err != nil || !bytes.Equal(got, want) {
				t.Fatalf("canceled session poisoned the next session: %v", err)
			}
			if compiled != wasiCachedModule(t, host, wasm) {
				t.Fatal("session cancellation discarded compiled artifacts")
			}
		})
	}
}

func TestWASISessionCanceledCompilationIsNotCached(t *testing.T) {
	host := wasiHostForTest(t, context.Background())
	request, wasm, want := wasiSessionResponse(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session := wasiSessionForTest(t, host, ctx)
	original := host.runtime
	host.runtime = &wasiCompileInterceptor{Runtime: original, compile: func(ctx context.Context, wasm []byte) (wazero.CompiledModule, error) {
		module, err := original.CompileModule(ctx, wasm)
		cancel()
		return module, err
	}}
	if _, err := session.Run(wasm, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("compilation cancellation lost its cause: %v", err)
	}
	if len(host.compiled) != 0 || len(host.order) != 0 || host.cacheBytes != 0 {
		t.Fatal("canceled compilation populated the cache")
	}
	host.runtime = original
	next := wasiSessionForTest(t, host, context.Background())
	if got, err := next.Run(wasm, request); err != nil || !bytes.Equal(got, want) {
		t.Fatalf("canceled compilation poisoned the next session: %v", err)
	}
}

func TestWASIHostCloseWaitsForActiveCompilation(t *testing.T) {
	host := wasiHostForTest(t, context.Background())
	request, wasm, _ := wasiSessionResponse(t)
	session := wasiSessionForTest(t, host, context.Background())
	original := host.runtime
	entered, release, runtimeClosed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseCompile := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseCompile()
	host.runtime = &wasiCompileInterceptor{Runtime: original,
		compile: func(ctx context.Context, _ []byte) (wazero.CompiledModule, error) {
			close(entered)
			<-release
			<-ctx.Done()
			return nil, ctx.Err()
		},
		close: func(ctx context.Context) error {
			close(runtimeClosed)
			return original.Close(ctx)
		},
	}
	runDone := make(chan error, 1)
	go func() { _, err := session.Run(wasm, request); runDone <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("compilation did not start")
	}
	closeDone := make(chan error, 2)
	for range 2 {
		go func() { closeDone <- host.Close() }()
	}
	select {
	case <-host.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("Close did not cancel the active invocation")
	}
	select {
	case <-runtimeClosed:
		t.Fatal("runtime closed before active compilation returned")
	case err := <-closeDone:
		t.Fatalf("Close returned before active compilation: %v", err)
	default:
	}
	if _, err := host.NewSession(context.Background()); err == nil {
		t.Fatal("closing host admitted a new session")
	}
	releaseCompile()
	select {
	case err := <-runDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("host closure lost cancellation cause: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("active compilation did not finish")
	}
	for range 2 {
		select {
		case err := <-closeDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("Close did not finish after active compilation returned")
		}
	}
	if host.compiled != nil || host.order != nil || host.cacheBytes != 0 {
		t.Fatal("host closure retained compiled artifacts")
	}
}

type wasiCompileInterceptor struct {
	wazero.Runtime
	compile func(context.Context, []byte) (wazero.CompiledModule, error)
	close   func(context.Context) error
}

func (runtime *wasiCompileInterceptor) CompileModule(ctx context.Context, wasm []byte) (wazero.CompiledModule, error) {
	return runtime.compile(ctx, wasm)
}

func (runtime *wasiCompileInterceptor) Close(ctx context.Context) error {
	if runtime.close != nil {
		return runtime.close(ctx)
	}
	return runtime.Runtime.Close(ctx)
}

func wasiSessionForTest(t *testing.T, host *WASIHost, ctx context.Context) *WASISession {
	t.Helper()
	session, err := host.NewSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	return session
}

func wasiSessionResponse(t *testing.T) (pm.Request, []byte, []byte) {
	t.Helper()
	request := wasiRequest(t, "echo")
	var response bytes.Buffer
	if err := pm.WriteResponse(&response, request, pm.Success(request.Input())); err != nil {
		t.Fatal(err)
	}
	return request, wasiResponseModule(response.Bytes()), response.Bytes()
}

func wasiCachedModule(t *testing.T, host *WASIHost, wasm []byte) wazero.CompiledModule {
	t.Helper()
	compiled, ok := host.compiled[sha256.Sum256(wasm)]
	if !ok {
		t.Fatal("successful compilation was not cached")
	}
	return compiled.module
}

func wasiWaitActive(t *testing.T, host *WASIHost, done <-chan error) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		host.mu.Lock()
		active := host.active
		host.mu.Unlock()
		if active {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("guest returned before cancellation: %v", err)
		case <-deadline.C:
			t.Fatal("guest did not become active")
		case <-tick.C:
		}
	}
}
