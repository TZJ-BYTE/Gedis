package command

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/TZJ-BYTE/RediGo/internal/database"
	"github.com/TZJ-BYTE/RediGo/internal/protocol"
)

type InfoCommand struct{}

func (c *InfoCommand) Execute(db *database.Database, args [][]byte) *protocol.Response {
	stats := db.GetStats()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	var b strings.Builder
	writeSection(&b, "server")
	writeKV(&b, "go_version", runtime.Version())
	writeKV(&b, "go_os_arch", runtime.GOOS+"/"+runtime.GOARCH)
	writeKV(&b, "cpu_num", fmt.Sprintf("%d", runtime.NumCPU()))

	writeSection(&b, "database")
	writeKV(&b, "db_id", fmt.Sprintf("%v", stats["db_id"]))
	writeKV(&b, "mode", fmt.Sprintf("%v", stats["mode"]))
	writeKV(&b, "memory_keys", fmt.Sprintf("%v", stats["memory_keys"]))
	writeKV(&b, "used_memory_bytes", fmt.Sprintf("%v", stats["used_memory_bytes"]))
	writeKV(&b, "max_memory_bytes", fmt.Sprintf("%v", stats["max_memory_bytes"]))
	writeKV(&b, "max_memory_policy", fmt.Sprintf("%v", stats["max_memory_policy"]))

	writeSection(&b, "runtime")
	writeKV(&b, "heap_alloc_bytes", fmt.Sprintf("%d", ms.HeapAlloc))
	writeKV(&b, "heap_inuse_bytes", fmt.Sprintf("%d", ms.HeapInuse))
	writeKV(&b, "heap_objects", fmt.Sprintf("%d", ms.HeapObjects))
	writeKV(&b, "num_gc", fmt.Sprintf("%d", ms.NumGC))
	writeKV(&b, "gc_pause_total_ns", fmt.Sprintf("%d", ms.PauseTotalNs))
	if ms.NumGC > 0 {
		last := ms.PauseNs[(ms.NumGC-1)%uint32(len(ms.PauseNs))]
		writeKV(&b, "gc_last_pause_ns", fmt.Sprintf("%d", last))
	}

	if enabled, ok := stats["lsm_enabled"].(bool); ok && enabled {
		writeSection(&b, "lsm")
		lsm, _ := stats["lsm"].(map[string]interface{})
		writeKV(&b, "value_threshold", fmt.Sprintf("%v", lsm["value_threshold"]))
		writeKV(&b, "block_size", fmt.Sprintf("%v", lsm["block_size"]))
		writeKV(&b, "memtable_size_limit", fmt.Sprintf("%v", lsm["memtable_size_limit"]))
		writeKV(&b, "sync_wal", fmt.Sprintf("%v", lsm["sync_wal"]))
		writeKV(&b, "write_ahead_log", fmt.Sprintf("%v", lsm["write_ahead_log"]))
		writeKV(&b, "writes_per_sec", fmt.Sprintf("%v", lsm["writes_per_sec"]))
		writeKV(&b, "cache_hit_permille", fmt.Sprintf("%v", lsm["cache_hit_permille"]))

		if bc, ok := lsm["block_cache"].(map[string]interface{}); ok {
			writeSection(&b, "block_cache")
			keys := sortedKeys(bc)
			for _, k := range keys {
				writeKV(&b, k, fmt.Sprintf("%v", bc[k]))
			}
		}

		if tc, ok := lsm["table_cache"].(map[string]interface{}); ok {
			writeSection(&b, "table_cache")
			keys := sortedKeys(tc)
			for _, k := range keys {
				writeKV(&b, k, fmt.Sprintf("%v", tc[k]))
			}
		}

		if vs, ok := lsm["version_set"].(map[string]interface{}); ok {
			writeSection(&b, "version_set")
			writeKV(&b, "total_size_bytes", fmt.Sprintf("%v", vs["total_size"]))
			writeKV(&b, "total_files", fmt.Sprintf("%v", vs["total_files"]))
			if levels, ok := vs["levels"].([]map[string]interface{}); ok {
				for _, lv := range levels {
					level := fmt.Sprintf("%v", lv["level"])
					writeKV(&b, "level_"+level+"_files", fmt.Sprintf("%v", lv["files"]))
					writeKV(&b, "level_"+level+"_size_bytes", fmt.Sprintf("%v", lv["size"]))
				}
			}
		}
	}

	return protocol.MakeBulkString(b.String())
}

func writeSection(b *strings.Builder, name string) {
	b.WriteString("# ")
	b.WriteString(name)
	b.WriteString("\n")
}

func writeKV(b *strings.Builder, k string, v string) {
	b.WriteString(k)
	b.WriteString(":")
	b.WriteString(v)
	b.WriteString("\n")
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
