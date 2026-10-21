//go:build !js

package procmacro

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"

	pm "github.com/goxide-lang/syntax/proc_macro"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Preview 1 ABI constants. The host exports no functions beyond this profile.
const (
	wasiBadf      = 8
	wasiFault     = 21
	wasiInval     = 28
	wasiIO        = 29
	wasiNotsup    = 58
	wasiNonblock  = 4
	wasiFatalExit = 137
	wasiClockStep = 1_000_000
	wasiArgv      = "goxide-proc-macro\x00"
)

type wasiStateKey struct{}

type wasiState struct {
	input          []byte
	read           int
	stdout, stderr bytes.Buffer
	closed         [3]bool
	flags          [3]uint16
	clock          uint64
	random         uint32
	fatal          error
}

func newWASIState(input []byte) *wasiState {
	return &wasiState{input: input, random: 0x6d2b79f5}
}

func (state *wasiState) executionError(err error) error {
	if state.stderr.Len() == 0 {
		return err
	}
	return fmt.Errorf("%w\nstderr: %s", err, state.stderr.String())
}

func (state *wasiState) fail(ctx context.Context, module api.Module, err error) uint32 {
	if state.fatal == nil {
		state.fatal = err
	}
	_ = module.CloseWithExitCode(ctx, wasiFatalExit)
	return wasiIO
}

func (state *wasiState) canceled(ctx context.Context, module api.Module) bool {
	if err := ctx.Err(); err != nil {
		_ = module.CloseWithExitCode(ctx, wasiFatalExit)
		return true
	}
	return false
}

type wasiFunction func(context.Context, api.Module, *wasiState, []uint64) uint32

func installWASIProfile(ctx context.Context, runtime wazero.Runtime) error {
	builder := runtime.NewHostModuleBuilder("wasi_snapshot_preview1")
	add := func(name string, params []api.ValueType, function wasiFunction) {
		builder.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, module api.Module, stack []uint64) {
			state, ok := ctx.Value(wasiStateKey{}).(*wasiState)
			if !ok {
				_ = module.CloseWithExitCode(ctx, wasiFatalExit)
				stack[0] = wasiIO
				return
			}
			stack[0] = uint64(function(ctx, module, state, stack))
		}), params, []api.ValueType{api.ValueTypeI32}).Export(name)
	}
	i32 := api.ValueTypeI32
	two, three, four := []api.ValueType{i32, i32}, []api.ValueType{i32, i32, i32}, []api.ValueType{i32, i32, i32, i32}
	add("args_sizes_get", two, func(_ context.Context, module api.Module, _ *wasiState, p []uint64) uint32 {
		memory := module.Memory()
		if !memory.WriteUint32Le(uint32(p[0]), 1) || !memory.WriteUint32Le(uint32(p[1]), uint32(len(wasiArgv))) {
			return wasiFault
		}
		return 0
	})
	add("args_get", two, func(_ context.Context, module api.Module, _ *wasiState, p []uint64) uint32 {
		memory := module.Memory()
		if !memory.WriteUint32Le(uint32(p[0]), uint32(p[1])) || !memory.Write(uint32(p[1]), []byte(wasiArgv)) {
			return wasiFault
		}
		return 0
	})
	add("environ_sizes_get", two, func(_ context.Context, module api.Module, _ *wasiState, p []uint64) uint32 {
		if !module.Memory().WriteUint32Le(uint32(p[0]), 0) || !module.Memory().WriteUint32Le(uint32(p[1]), 0) {
			return wasiFault
		}
		return 0
	})
	add("environ_get", two, func(context.Context, api.Module, *wasiState, []uint64) uint32 { return 0 })
	add("fd_read", four, wasiRead)
	add("fd_write", four, wasiWrite)
	add("fd_close", []api.ValueType{i32}, func(_ context.Context, _ api.Module, state *wasiState, p []uint64) uint32 {
		fd := uint32(p[0])
		if fd >= 3 || state.closed[fd] {
			return wasiBadf
		}
		state.closed[fd] = true
		return 0
	})
	add("fd_fdstat_get", two, wasiFDStat)
	add("fd_fdstat_set_flags", two, func(_ context.Context, _ api.Module, state *wasiState, p []uint64) uint32 {
		fd, flags := uint32(p[0]), uint32(p[1])
		if fd >= 3 || state.closed[fd] {
			return wasiBadf
		}
		if flags & ^uint32(wasiNonblock) != 0 {
			return wasiNotsup
		}
		state.flags[fd] = uint16(flags)
		return 0
	})
	add("fd_prestat_get", two, func(context.Context, api.Module, *wasiState, []uint64) uint32 { return wasiBadf })
	add("fd_prestat_dir_name", three, func(context.Context, api.Module, *wasiState, []uint64) uint32 { return wasiBadf })
	add("clock_time_get", []api.ValueType{i32, api.ValueTypeI64, i32}, wasiClock)
	add("random_get", two, wasiRandom)
	add("poll_oneoff", four, wasiPoll)
	add("sched_yield", nil, func(context.Context, api.Module, *wasiState, []uint64) uint32 { return 0 })
	builder.NewFunctionBuilder().WithFunc(func(ctx context.Context, module api.Module, code uint32) {
		_ = module.CloseWithExitCode(ctx, code)
	}).Export("proc_exit")
	_, err := builder.Instantiate(ctx)
	return err
}

func wasiMemory(memory api.Memory, offset uint32, count uint32, size uint64) ([]byte, bool) {
	length := uint64(count) * size
	if length > math.MaxUint32 {
		return nil, false
	}
	return memory.Read(offset, uint32(length))
}

func wasiRead(ctx context.Context, module api.Module, state *wasiState, p []uint64) uint32 {
	if uint32(p[0]) != 0 || state.closed[0] {
		return wasiBadf
	}
	memory := module.Memory()
	iov, ok := wasiMemory(memory, uint32(p[1]), uint32(p[2]), 8)
	if !ok {
		return wasiFault
	}
	result, ok := memory.Read(uint32(p[3]), 4)
	if !ok {
		return wasiFault
	}
	var read uint32
	for index := uint32(0); index < uint32(p[2]); index++ {
		if index&1023 == 0 && state.canceled(ctx, module) {
			return wasiIO
		}
		descriptor := iov[uint64(index)*8:]
		destination, ok := memory.Read(binary.LittleEndian.Uint32(descriptor), binary.LittleEndian.Uint32(descriptor[4:]))
		if !ok {
			return wasiFault
		}
		n := copy(destination, state.input[state.read:])
		state.read += n
		read += uint32(n)
		if state.read == len(state.input) {
			break
		}
	}
	binary.LittleEndian.PutUint32(result, read)
	return 0
}

func wasiWrite(ctx context.Context, module api.Module, state *wasiState, p []uint64) uint32 {
	fd := uint32(p[0])
	if fd < 1 || fd > 2 || state.closed[fd] {
		return wasiBadf
	}
	destination, limit, name := &state.stdout, pm.MaxMessageBytes, "stdout"
	if fd == 2 {
		destination, limit, name = &state.stderr, pm.MaxDiagnosticBytes, "stderr"
	}
	memory := module.Memory()
	iov, ok := wasiMemory(memory, uint32(p[1]), uint32(p[2]), 8)
	if !ok {
		return wasiFault
	}
	result, ok := memory.Read(uint32(p[3]), 4)
	if !ok {
		return wasiFault
	}
	var written uint64
	for index := uint32(0); index < uint32(p[2]); index++ {
		if index&1023 == 0 && state.canceled(ctx, module) {
			return wasiIO
		}
		descriptor := iov[uint64(index)*8:]
		length := binary.LittleEndian.Uint32(descriptor[4:])
		if _, ok := memory.Read(binary.LittleEndian.Uint32(descriptor), length); !ok {
			return wasiFault
		}
		written += uint64(length)
		if written > uint64(limit-destination.Len()) {
			return state.fail(ctx, module, fmt.Errorf("procedural macro %s exceeds %d bytes", name, limit))
		}
	}
	for index := uint32(0); index < uint32(p[2]); index++ {
		if index&1023 == 0 && state.canceled(ctx, module) {
			return wasiIO
		}
		descriptor := iov[uint64(index)*8:]
		data, _ := memory.Read(binary.LittleEndian.Uint32(descriptor), binary.LittleEndian.Uint32(descriptor[4:]))
		_, _ = destination.Write(data)
	}
	binary.LittleEndian.PutUint32(result, uint32(written))
	return 0
}

func wasiFDStat(_ context.Context, module api.Module, state *wasiState, p []uint64) uint32 {
	fd := uint32(p[0])
	if fd >= 3 || state.closed[fd] {
		return wasiBadf
	}
	stat, ok := module.Memory().Read(uint32(p[1]), 24)
	if !ok {
		return wasiFault
	}
	clear(stat)
	stat[0] = 2 // character device
	binary.LittleEndian.PutUint16(stat[2:], state.flags[fd])
	rights := uint64(1<<3 | 1<<27) // fd_fdstat_set_flags and poll_fd_readwrite
	if fd == 0 {
		rights |= 1 << 1
	} else {
		rights |= 1 << 6
	}
	binary.LittleEndian.PutUint64(stat[8:], rights)
	return 0
}

func wasiClock(_ context.Context, module api.Module, state *wasiState, p []uint64) uint32 {
	if uint32(p[0]) > 1 {
		return wasiInval
	}
	if !module.Memory().WriteUint64Le(uint32(p[2]), state.clock) {
		return wasiFault
	}
	state.clock = wasiAddTime(state.clock, wasiClockStep)
	return 0
}

func wasiRandom(ctx context.Context, module api.Module, state *wasiState, p []uint64) uint32 {
	data, ok := module.Memory().Read(uint32(p[0]), uint32(p[1]))
	if !ok {
		return wasiFault
	}
	for index := range data {
		if index&4095 == 0 && state.canceled(ctx, module) {
			return wasiIO
		}
		state.random ^= state.random << 13
		state.random ^= state.random >> 17
		state.random ^= state.random << 5
		data[index] = byte(state.random)
	}
	return 0
}

func wasiAddTime(current, delta uint64) uint64 {
	if delta > math.MaxUint64-current {
		return math.MaxUint64
	}
	return current + delta
}

// wasiSubscription returns the event's deadline or immediate readiness/error.
// All clock deadlines use the time at entry to poll, even after time advances.
func wasiSubscription(input []byte, state *wasiState, base uint64) (deadline uint64, errno uint32, immediate bool) {
	switch input[8] {
	case 0: // clock
		clock, flags := binary.LittleEndian.Uint32(input[16:]), binary.LittleEndian.Uint16(input[40:])
		if clock > 1 || flags & ^uint16(1) != 0 {
			return 0, wasiInval, true
		}
		if flags == 1 {
			return 0, wasiNotsup, true
		}
		timeout := binary.LittleEndian.Uint64(input[24:])
		return wasiAddTime(base, timeout), 0, timeout == 0
	case 1, 2: // fd_read / fd_write
		fd := binary.LittleEndian.Uint32(input[16:])
		if fd >= 3 || state.closed[fd] || input[8] == 1 && fd != 0 || input[8] == 2 && fd == 0 {
			return 0, wasiBadf, true
		}
		return 0, 0, true // finite stdin (including EOF) and captured output
	default:
		return 0, wasiInval, true
	}
}

func wasiPoll(ctx context.Context, module api.Module, state *wasiState, p []uint64) uint32 {
	count := uint32(p[2])
	if count == 0 {
		return wasiInval
	}
	memory := module.Memory()
	input, ok := wasiMemory(memory, uint32(p[0]), count, 48)
	if !ok {
		return wasiFault
	}
	output, ok := wasiMemory(memory, uint32(p[1]), count, 32)
	if !ok {
		return wasiFault
	}
	result, ok := memory.Read(uint32(p[3]), 4)
	if !ok {
		return wasiFault
	}
	// Aliased guest input/output still observes the original subscriptions.
	inputStart, outputStart := uint64(uint32(p[0])), uint64(uint32(p[1]))
	if inputStart < outputStart+uint64(len(output)) && outputStart < inputStart+uint64(len(input)) {
		input = append([]byte(nil), input...)
	}
	base, earliest, ready := state.clock, uint64(math.MaxUint64), false
	for index := uint32(0); index < count; index++ {
		if index&1023 == 0 && state.canceled(ctx, module) {
			return wasiIO
		}
		subscription := input[uint64(index)*48:]
		if subscription[8] > 2 {
			return wasiInval
		}
		deadline, _, immediate := wasiSubscription(subscription, state, base)
		ready = ready || immediate
		if !immediate {
			earliest = min(earliest, deadline)
		}
	}
	if !ready {
		state.clock = earliest
	}
	var events uint32
	for index := uint32(0); index < count; index++ {
		if index&1023 == 0 && state.canceled(ctx, module) {
			return wasiIO
		}
		subscription := input[uint64(index)*48:]
		deadline, errno, immediate := wasiSubscription(subscription, state, base)
		if !immediate && deadline > state.clock {
			continue
		}
		event := output[uint64(events)*32 : uint64(events+1)*32]
		clear(event)
		copy(event, subscription[:8])
		binary.LittleEndian.PutUint16(event[8:], uint16(errno))
		event[10] = subscription[8]
		if errno == 0 && subscription[8] == 1 {
			binary.LittleEndian.PutUint64(event[16:], uint64(len(state.input)-state.read))
		}
		events++
	}
	binary.LittleEndian.PutUint32(result, events)
	return 0
}
