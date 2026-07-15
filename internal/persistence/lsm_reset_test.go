package persistence

import (
	"path/filepath"
	"testing"
)

func TestLSMEnergy_ResetClearsPersistentState(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultOptions()
	opts.EnableOffloading = false

	e, err := OpenLSMEnergy(dir, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := e.Put([]byte("k1"), []byte("v1")); err != nil {
		t.Fatalf("put: %v", err)
	}
	_ = e.Close()

	e2, err := OpenLSMEnergy(dir, opts)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if v, ok := e2.Get([]byte("k1")); !ok || string(v) != "v1" {
		t.Fatalf("expected k1=v1, got ok=%v v=%q", ok, string(v))
	}

	if err := e2.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, ok := e2.Get([]byte("k1")); ok {
		t.Fatalf("expected k1 to be cleared")
	}
	_ = e2.Close()

	e3, err := OpenLSMEnergy(dir, opts)
	if err != nil {
		t.Fatalf("reopen2: %v", err)
	}
	if _, ok := e3.Get([]byte("k1")); ok {
		t.Fatalf("expected k1 to stay cleared after reopen")
	}
	if _, err := filepath.Glob(filepath.Join(dir, "wal", "*.wal")); err != nil {
		t.Fatalf("glob: %v", err)
	}
	_ = e3.Close()
}

