package procmacro

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

const (
	ProfileID        = "goxide-wasi-command-1"
	MaxWASMBytes     = 64 << 20
	MaxMemoryPages   = 4096
	MaxTableElements = 65536
)

// PrepareWASM bounds the artifact itself, so all execution hosts load the same
// memory and table limits. The execution engine still validates the full module.
func PrepareWASM(data []byte) ([]byte, error) {
	if len(data) > MaxWASMBytes {
		return nil, fmt.Errorf("procedural macro WASM exceeds %d bytes", MaxWASMBytes)
	}
	header := []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}
	if !bytes.HasPrefix(data, header) {
		return nil, fmt.Errorf("procedural macro artifact is not a WASM 1 module")
	}
	output := append([]byte(nil), header...)
	r := wasmReader{data: data[len(header):]}
	var seen [13]bool
	lastOrder := byte(0)
	for r.remaining() > 0 {
		id, err := r.byte()
		if err != nil {
			return nil, err
		}
		size, err := r.u32()
		if err != nil {
			return nil, err
		}
		section, err := r.take(size)
		if err != nil {
			return nil, err
		}
		if id > 12 {
			return nil, fmt.Errorf("unsupported procedural macro WASM section %d", id)
		}
		if id != 0 {
			// data_count precedes code, despite its higher section ID.
			order := id
			if id == 12 {
				order = 10
			} else if id >= 10 {
				order++
			}
			if seen[id] || order <= lastOrder {
				return nil, fmt.Errorf("duplicate or out-of-order WASM section %d", id)
			}
			seen[id], lastOrder = true, order
		}
		switch id {
		case 2:
			err = validateWASIImports(section)
		case 4, 5:
			section, err = boundWASMLimits(section, id == 4)
		}
		if err != nil {
			return nil, err
		}
		output = append(output, id)
		output = appendWASMU32(output, uint32(len(section)))
		output = append(output, section...)
	}
	if !seen[4] || !seen[5] {
		return nil, fmt.Errorf("procedural macro WASM requires one internal table and memory")
	}
	if len(output) > MaxWASMBytes {
		return nil, fmt.Errorf("bounded procedural macro WASM exceeds %d bytes", MaxWASMBytes)
	}
	return output, nil
}

func allowedWASIImport(name string) bool {
	switch name {
	case "args_sizes_get", "args_get", "environ_sizes_get", "environ_get",
		"fd_read", "fd_write", "fd_close", "fd_fdstat_get", "fd_fdstat_set_flags",
		"fd_prestat_get", "fd_prestat_dir_name", "clock_time_get", "random_get",
		"poll_oneoff", "sched_yield", "proc_exit":
		return true
	}
	return false
}

func validateWASIImports(data []byte) error {
	r := wasmReader{data: data}
	count, err := r.u32()
	if err != nil {
		return err
	}
	for range count {
		module, err := r.name()
		if err != nil {
			return err
		}
		name, err := r.name()
		if err != nil {
			return err
		}
		kind, err := r.byte()
		if err != nil {
			return err
		}
		if kind != 0 || module != "wasi_snapshot_preview1" || !allowedWASIImport(name) {
			return fmt.Errorf("procedural macro WASM import %s.%s (kind %d) is outside %s", module, name, kind, ProfileID)
		}
		if _, err := r.u32(); err != nil {
			return err
		}
	}
	return r.end()
}

func boundWASMLimits(data []byte, table bool) ([]byte, error) {
	r := wasmReader{data: data}
	count, err := r.u32()
	if err != nil {
		return nil, err
	}
	name, cap := "memory", uint32(MaxMemoryPages)
	output := []byte{1}
	if table {
		name, cap = "table", MaxTableElements
	}
	if count != 1 {
		return nil, fmt.Errorf("procedural macro WASM requires exactly one %s", name)
	}
	if table {
		element, err := r.byte()
		if err != nil {
			return nil, err
		}
		if element != 0x70 {
			return nil, fmt.Errorf("procedural macro WASM table must contain funcref")
		}
		output = append(output, element)
	}
	flags, err := r.u32()
	if err != nil {
		return nil, err
	}
	if flags > 1 {
		return nil, fmt.Errorf("procedural macro WASM %s requires nonshared 32-bit limits", name)
	}
	initial, err := r.u32()
	if err != nil {
		return nil, err
	}
	if initial > cap {
		return nil, fmt.Errorf("procedural macro WASM %s initial %d exceeds %d", name, initial, cap)
	}
	maximum := cap
	if flags == 1 {
		maximum, err = r.u32()
		if err != nil {
			return nil, err
		}
		if maximum < initial {
			return nil, fmt.Errorf("procedural macro WASM %s maximum is below initial", name)
		}
		maximum = min(maximum, cap)
	}
	if table {
		maximum = initial
	}
	if err := r.end(); err != nil {
		return nil, err
	}
	output = append(output, 1)
	output = appendWASMU32(output, initial)
	return appendWASMU32(output, maximum), nil
}

type wasmReader struct {
	data []byte
	pos  int
}

func (r *wasmReader) remaining() int { return len(r.data) - r.pos }

func (r *wasmReader) take(size uint32) ([]byte, error) {
	if uint64(size) > uint64(r.remaining()) {
		return nil, fmt.Errorf("truncated procedural macro WASM")
	}
	data := r.data[r.pos : r.pos+int(size)]
	r.pos += int(size)
	return data, nil
}

func (r *wasmReader) byte() (byte, error) {
	data, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (r *wasmReader) u32() (uint32, error) {
	var result uint32
	for shift := uint(0); shift < 35; shift += 7 {
		b, err := r.byte()
		if err != nil {
			return 0, err
		}
		if shift == 28 && b > 15 {
			break
		}
		result |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return result, nil
		}
	}
	return 0, fmt.Errorf("invalid uint32 in procedural macro WASM")
}

func (r *wasmReader) name() (string, error) {
	size, err := r.u32()
	if err != nil {
		return "", err
	}
	data, err := r.take(size)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("invalid UTF-8 in procedural macro WASM name")
	}
	return string(data), nil
}

func (r *wasmReader) end() error {
	if r.remaining() != 0 {
		return fmt.Errorf("trailing bytes in procedural macro WASM section")
	}
	return nil
}

func appendWASMU32(dst []byte, n uint32) []byte {
	for n >= 0x80 {
		dst = append(dst, byte(n)|0x80)
		n >>= 7
	}
	return append(dst, byte(n))
}
