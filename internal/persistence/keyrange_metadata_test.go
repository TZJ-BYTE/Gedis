package persistence

import (
	"bytes"
	"testing"
	"time"
)

func TestLSMEnergy_FlushWritesKeyRangeMetadata(t *testing.T) {
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

	if err := e.Put([]byte("b"), []byte("1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := e.Put([]byte("a"), []byte("1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := e.Put([]byte("c"), make([]byte, 900*1024)); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := e.Put([]byte("d"), make([]byte, 900*1024)); err != nil {
		t.Fatalf("put: %v", err)
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
		t.Fatalf("expected flush")
	}
	e.flushWG.Wait()

	e.versionSet.mu.Lock()
	v := e.versionSet.currentVersion
	if len(v.Files) == 0 || len(v.Files[0]) == 0 {
		e.versionSet.mu.Unlock()
		t.Fatalf("expected level0 file")
	}
	fm := v.Files[0][0]
	smallest := append([]byte(nil), fm.SmallestKey...)
	largest := append([]byte(nil), fm.LargestKey...)
	e.versionSet.mu.Unlock()
	if smallest == nil || largest == nil {
		t.Fatalf("expected keyrange metadata")
	}
	if !bytes.Equal(smallest, []byte("a")) {
		t.Fatalf("smallest=%q", smallest)
	}
	if !bytes.Equal(largest, []byte("d")) {
		t.Fatalf("largest=%q", largest)
	}

	if err := e.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	e2, err := OpenLSMEnergy(dir, opts)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer e2.Close()
	e2.flushWG.Wait()

	e2.versionSet.mu.Lock()
	v2 := e2.versionSet.currentVersion
	if len(v2.Files) == 0 || len(v2.Files[0]) == 0 {
		e2.versionSet.mu.Unlock()
		t.Fatalf("expected level0 file after reopen")
	}
	fm2 := v2.Files[0][0]
	smallest2 := append([]byte(nil), fm2.SmallestKey...)
	largest2 := append([]byte(nil), fm2.LargestKey...)
	e2.versionSet.mu.Unlock()
	if !bytes.Equal(smallest2, []byte("a")) || !bytes.Equal(largest2, []byte("d")) {
		t.Fatalf("keyrange after reopen smallest=%q largest=%q", smallest2, largest2)
	}
}
