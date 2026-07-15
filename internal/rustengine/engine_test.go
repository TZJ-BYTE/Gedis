//go:build windows

package rustengine

import "testing"

func TestEngineRoundTrip(t *testing.T) {
	dir := t.TempDir()
	engine, err := Open(Options{
		DataDir:            dir,
		SegmentSizeBytes:   4 * 1024,
		CheckpointAfterOps: 8,
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() {
		if closeErr := engine.Close(); closeErr != nil {
			t.Fatalf("close: %v", closeErr)
		}
	}()

	if err := engine.Put([]byte("alpha"), []byte("one")); err != nil {
		t.Fatalf("put: %v", err)
	}

	value, found, err := engine.Get([]byte("alpha"))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !found {
		t.Fatalf("expected key alpha")
	}
	if string(value) != "one" {
		t.Fatalf("expected value one, got %q", string(value))
	}

	all, err := engine.LoadAllKeys()
	if err != nil {
		t.Fatalf("load all: %v", err)
	}
	if string(all["alpha"]) != "one" {
		t.Fatalf("expected alpha in snapshot export")
	}

	if err := engine.Delete([]byte("alpha")); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, found, err := engine.Get([]byte("alpha")); err != nil {
		t.Fatalf("get after delete: %v", err)
	} else if found {
		t.Fatalf("expected key alpha to be deleted")
	}
}
