package persistence

import (
	"testing"
	"time"
)

func TestLSMEnergy_MemTableSizeTriggersFlush(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultOptions()
	opts.EnableOffloading = false
	opts.UseCache = false
	opts.SyncWAL = false
	opts.ValueThreshold = -1
	opts.MemTableSize = 1024 * 1024

	e, err := OpenLSMEnergy(dir, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	val := make([]byte, 800*1024)
	if err := e.Put([]byte("k1"), val); err != nil {
		t.Fatalf("put1: %v", err)
	}
	if err := e.Put([]byte("k2"), val); err != nil {
		t.Fatalf("put2: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for e.flushCount.Load() == 0 && time.Now().Before(deadline) {
		select {
		case <-e.flushDone:
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	if e.flushCount.Load() == 0 {
		t.Fatalf("expected flush to happen when memtable exceeds MemTableSize")
	}
}
