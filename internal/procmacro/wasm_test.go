package procmacro

import (
	"bytes"
	"strings"
	"testing"
)

func wasmSections(sections ...[]byte) []byte {
	data := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	for _, section := range sections {
		data = append(data, section[0])
		data = appendWASMU32(data, uint32(len(section)-1))
		data = append(data, section[1:]...)
	}
	return data
}

func TestPrepareWASMBoundsArtifact(t *testing.T) {
	// The unbounded input needs no change to its code to gain resource limits.
	input := wasmSections([]byte{4, 1, 0x70, 0, 2}, []byte{5, 1, 0, 17})
	bounded, err := PrepareWASM(input)
	if err != nil {
		t.Fatal(err)
	}
	want := wasmSections([]byte{4, 1, 0x70, 1, 2, 2}, []byte{5, 1, 1, 17, 0x80, 0x20})
	if !bytes.Equal(bounded, want) {
		t.Fatalf("bounded artifact = %x, want %x", bounded, want)
	}
	again, err := PrepareWASM(bounded)
	if err != nil || !bytes.Equal(again, bounded) {
		t.Fatalf("bounded artifact is not stable: %x, %v", again, err)
	}
	smaller := wasmSections([]byte{4, 1, 0x70, 1, 0, 9}, []byte{5, 1, 1, 17, 18})
	bounded, err = PrepareWASM(smaller)
	if err != nil {
		t.Fatal(err)
	}
	want = wasmSections([]byte{4, 1, 0x70, 1, 0, 0}, []byte{5, 1, 1, 17, 18})
	if !bytes.Equal(bounded, want) {
		t.Fatal("packaging enlarged an existing memory limit or left a growable table")
	}
}

func TestPrepareWASMRejectsCapabilitiesAndInvalidLimits(t *testing.T) {
	table, memory := []byte{4, 1, 0x70, 0, 2}, []byte{5, 1, 0, 17}
	importSection := func(module, name string, kind byte) []byte {
		data := []byte{2, 1}
		for _, name := range []string{module, name} {
			data = appendWASMU32(data, uint32(len(name)))
			data = append(data, name...)
		}
		return append(data, kind, 0)
	}
	for _, test := range []struct {
		name string
		data []byte
		want string
	}{
		{"filesystem", wasmSections(importSection("wasi_snapshot_preview1", "path_open", 0), table, memory), "outside"},
		{"network", wasmSections(importSection("wasi_snapshot_preview1", "sock_recv", 0), table, memory), "outside"},
		{"other_module", wasmSections(importSection("env", "fd_write", 0), table, memory), "outside"},
		{"imported_memory", wasmSections(importSection("wasi_snapshot_preview1", "fd_write", 2), table, memory), "outside"},
		{"shared_memory", wasmSections(table, []byte{5, 1, 3, 17, 18}), "nonshared"},
		{"memory64", wasmSections(table, []byte{5, 1, 4, 17}), "32-bit"},
		{"two_memories", wasmSections(table, []byte{5, 2, 0, 17, 0, 17}), "exactly one"},
		{"two_tables", wasmSections([]byte{4, 2, 0x70, 0, 2, 0x70, 0, 2}, memory), "exactly one"},
		{"externref", wasmSections([]byte{4, 1, 0x6f, 0, 2}, memory), "funcref"},
		{"large_memory", wasmSections(table, []byte{5, 1, 0, 0x81, 0x20}), "exceeds"},
		{"large_table", wasmSections([]byte{4, 1, 0x70, 0, 0x81, 0x80, 4}, memory), "exceeds"},
		{"inverted_limits", wasmSections(table, []byte{5, 1, 1, 17, 16}), "below"},
		{"duplicate", wasmSections(table, memory, memory), "duplicate"},
		{"reordered", wasmSections(memory, table), "out-of-order"},
		{"trailing", wasmSections(table, append(append([]byte{}, memory...), 0)), "trailing"},
		{"missing", wasmSections(table), "one internal"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := PrepareWASM(test.data); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("PrepareWASM error = %v, want %q", err, test.want)
			}
		})
	}
	allowed := wasmSections(importSection("wasi_snapshot_preview1", "fd_write", 0), table, memory)
	if _, err := PrepareWASM(allowed); err != nil {
		t.Fatal(err)
	}
}

func TestPrepareWASMRejectsMalformedFraming(t *testing.T) {
	valid := wasmSections([]byte{4, 1, 0x70, 0, 2}, []byte{5, 1, 0, 17})
	for n := range len(valid) {
		if _, err := PrepareWASM(valid[:n]); err == nil {
			t.Fatalf("accepted truncated artifact of %d bytes", n)
		}
	}
	for _, section := range [][]byte{
		{5, 0xff, 0xff, 0xff, 0xff, 0x10},
		{5, 1, 0x80, 0x80, 0x80, 0x80, 0x80, 0},
	} {
		if _, err := PrepareWASM(wasmSections(section)); err == nil {
			t.Fatal("accepted overflowing LEB128 integer")
		}
	}
}
