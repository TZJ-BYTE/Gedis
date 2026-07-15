package persistence

import (
	"path/filepath"
	"testing"
	"time"
)

func TestOffload_RemoteObjectsDeletedOnReset(t *testing.T) {
	dbDir := t.TempDir()
	remote := t.TempDir()

	opts := DefaultOptions()
	opts.EnableOffloading = true
	opts.OffloadBackend = "fs"
	opts.OffloadFSRoot = remote
	opts.OffloadMinLevel = 0
	opts.OffloadKeepLocal = false
	opts.ValueThreshold = 1 << 30

	e, err := OpenLSMEnergy(dbDir, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer e.Close()

	val := make([]byte, 1024*1024)
	for i := 0; i < 6; i++ {
		key := []byte{byte('k'), byte('0' + i)}
		if err := e.Put(key, val); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	deadline := time.Now().Add(3 * time.Second)
	for e.flushCount.Load() == 0 && time.Now().Before(deadline) {
		select {
		case <-e.flushDone:
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	before, err := filepath.Glob(filepath.Join(remote, "sstable", "*.sstable"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(before) == 0 {
		t.Fatalf("expected remote objects to exist")
	}

	if err := e.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}

	after, err := filepath.Glob(filepath.Join(remote, "sstable", "*.sstable"))
	if err != nil {
		t.Fatalf("glob2: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("expected remote objects to be deleted, got %v", after)
	}
}
