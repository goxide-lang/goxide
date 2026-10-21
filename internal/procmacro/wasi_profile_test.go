//go:build !js

package procmacro

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"testing"

	"github.com/tetratelabs/wazero/api"
)

func wasiProfileModule(t *testing.T) api.Module {
	t.Helper()
	host := wasiHostForTest(t, context.Background())
	module, err := host.runtime.Instantiate(context.Background(), wasmSections([]byte{4, 1, 0x70, 1, 0, 0}, []byte{5, 1, 1, 2, 2}))
	if err != nil {
		t.Fatal(err)
	}
	return module
}

func TestWASIProfileClockAndRandom(t *testing.T) {
	module := wasiProfileModule(t)
	state := newWASIState(nil)
	ctx := context.Background()
	for index, id := range []uint64{0, 1, 0} {
		if errno := wasiClock(ctx, module, state, []uint64{id, 999999, 0}); errno != 0 {
			t.Fatalf("clock errno = %d", errno)
		}
		if got, _ := module.Memory().ReadUint64Le(0); got != uint64(index)*wasiClockStep {
			t.Fatalf("clock = %d", got)
		}
	}
	if got := wasiClock(ctx, module, state, []uint64{2, 0, 0}); got != wasiInval || state.clock != 3*wasiClockStep {
		t.Fatal("unsupported clock advanced logical time")
	}
	if errno := wasiRandom(ctx, module, state, []uint64{0, 17}); errno != 0 {
		t.Fatal(errno)
	}
	first, _ := module.Memory().Read(0, 17)
	first = append([]byte(nil), first...)
	state = newWASIState(nil)
	for _, part := range []struct{ start, length uint64 }{{0, 3}, {3, 0}, {3, 7}, {10, 7}} {
		if errno := wasiRandom(ctx, module, state, []uint64{part.start, part.length}); errno != 0 {
			t.Fatal(errno)
		}
	}
	second, _ := module.Memory().Read(0, 17)
	if !bytes.Equal(first, second) || bytes.Equal(first, make([]byte, 17)) {
		t.Fatalf("random stream changes with read segmentation: %x / %x", first, second)
	}
}

func TestWASIProfileVirtualDescriptorsAndBounds(t *testing.T) {
	module := wasiProfileModule(t)
	memory, ctx := module.Memory(), context.Background()
	state := newWASIState([]byte("abc"))
	memory.WriteUint32Le(0, 100)
	memory.WriteUint32Le(4, 8)
	if errno := wasiRead(ctx, module, state, []uint64{0, 0, 1, 16}); errno != 0 {
		t.Fatal(errno)
	}
	if count, _ := memory.ReadUint32Le(16); count != 3 {
		t.Fatalf("read count = %d", count)
	}
	if errno := wasiRead(ctx, module, state, []uint64{0, 0, 1, 16}); errno != 0 {
		t.Fatal(errno)
	}
	if count, _ := memory.ReadUint32Le(16); count != 0 {
		t.Fatal("EOF did not return a successful zero-byte read")
	}
	if errno := wasiRead(ctx, module, state, []uint64{1, 0, 1, 16}); errno != wasiBadf {
		t.Fatalf("stdout allowed reads: %d", errno)
	}
	if errno := wasiWrite(ctx, module, state, []uint64{0, 0, 1, 16}); errno != wasiBadf {
		t.Fatalf("stdin allowed writes: %d", errno)
	}
	state.closed[1] = true
	if errno := wasiWrite(ctx, module, state, []uint64{1, 0, 1, 16}); errno != wasiBadf {
		t.Fatalf("closed stdout allowed writes: %d", errno)
	}
	if errno := wasiRead(ctx, module, state, []uint64{0, 0, 1 << 31, 16}); errno != wasiFault {
		t.Fatalf("overflowing iovec count error = %d", errno)
	}
	state.flags[0] = wasiNonblock
	if errno := wasiFDStat(ctx, module, state, []uint64{0, 32}); errno != 0 {
		t.Fatal(errno)
	}
	stat, _ := memory.Read(32, 24)
	if stat[0] != 2 || binary.LittleEndian.Uint16(stat[2:]) != wasiNonblock || binary.LittleEndian.Uint64(stat[8:])&(1<<1) == 0 || binary.LittleEndian.Uint64(stat[8:])&(1<<6) != 0 {
		t.Fatalf("stdin descriptor stat = %x", stat)
	}
}

func TestWASIProfilePollOrderingAndVirtualTime(t *testing.T) {
	module := wasiProfileModule(t)
	memory, ctx := module.Memory(), context.Background()
	state := newWASIState(nil)
	clock := func(id, timeout uint64) []byte {
		data := make([]byte, 48)
		binary.LittleEndian.PutUint64(data, id)
		binary.LittleEndian.PutUint32(data[16:], 1)
		binary.LittleEndian.PutUint64(data[24:], timeout)
		return data
	}
	fd := func(id uint64, kind byte, number uint32) []byte {
		data := make([]byte, 48)
		binary.LittleEndian.PutUint64(data, id)
		data[8] = kind
		binary.LittleEndian.PutUint32(data[16:], number)
		return data
	}
	run := func(subscriptions ...[]byte) []byte {
		t.Helper()
		memory.Write(0, bytes.Join(subscriptions, nil))
		if errno := wasiPoll(ctx, module, state, []uint64{0, 1024, uint64(len(subscriptions)), 2048}); errno != 0 {
			t.Fatalf("poll errno = %d", errno)
		}
		count, _ := memory.ReadUint32Le(2048)
		output, _ := memory.Read(1024, count*32)
		return append([]byte(nil), output...)
	}
	output := run(clock(10, 20), fd(11, 1, 0), clock(12, 0), fd(13, 2, 0))
	if len(output) != 3*32 || binary.LittleEndian.Uint64(output) != 11 || binary.LittleEndian.Uint64(output[32:]) != 12 || binary.LittleEndian.Uint64(output[64:]) != 13 || state.clock != 0 || binary.LittleEndian.Uint16(output[72:]) != wasiBadf {
		t.Fatalf("immediate event order or time = %x, %d", output, state.clock)
	}
	output = run(clock(1, 30), clock(2, 20), clock(3, 20))
	if len(output) != 64 || binary.LittleEndian.Uint64(output) != 2 || binary.LittleEndian.Uint64(output[32:]) != 3 || state.clock != 20 {
		t.Fatalf("earliest clock events = %x, %d", output, state.clock)
	}
	absolute := clock(4, 1)
	binary.LittleEndian.PutUint16(absolute[40:], 1)
	output = run(absolute)
	if binary.LittleEndian.Uint16(output[8:]) != wasiNotsup || state.clock != 20 {
		t.Fatal("absolute clock did not return an immediate ENOTSUP event")
	}
	if errno := wasiPoll(ctx, module, state, []uint64{0, 1024, 0, 2048}); errno != wasiInval {
		t.Fatalf("zero subscriptions error = %d", errno)
	}
	if errno := wasiPoll(ctx, module, state, []uint64{0, 1024, 1 << 31, 2048}); errno != wasiFault {
		t.Fatalf("overflow subscriptions error = %d", errno)
	}
	if got := wasiAddTime(math.MaxUint64-1, 5); got != math.MaxUint64 {
		t.Fatal("logical time wrapped")
	}
}
