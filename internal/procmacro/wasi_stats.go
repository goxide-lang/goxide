//go:build !js

package procmacro

import (
	"encoding/json"
	"os"
	"time"
)

// WASIHostStats counts admitted calls and module loads. A module load may read
// cached native code or compile WASM; wazero does not expose that distinction.
type WASIHostStats struct {
	Invocations    int           `json:"calls"`
	MemoryHits     int           `json:"memory_hits"`
	ModuleLoads    int           `json:"module_loads"`
	ModuleLoadTime time.Duration `json:"module_load_ns"`
	ExecutionTime  time.Duration `json:"execute_ns"`
	DiskCache      bool          `json:"disk_cache"`
	CacheFallback  bool          `json:"cache_fallback"`
}

func (host *WASIHost) Stats() WASIHostStats {
	host.mu.Lock()
	defer host.mu.Unlock()
	return host.stats
}

func (host *WASIHost) writeStats() {
	if host.traceDir == "" {
		return
	}
	file, err := os.CreateTemp(host.traceDir, "macro-host-*.json")
	if err != nil {
		return
	}
	defer file.Close()
	_ = json.NewEncoder(file).Encode(host.Stats())
}
