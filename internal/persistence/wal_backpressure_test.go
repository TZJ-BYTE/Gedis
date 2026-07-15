package persistence

import (
	"testing"
	"time"
)

func TestWALWriter_QueueFullReturnsError(t *testing.T) {
	w := &WALWriter{
		reqCh: make(chan walRequest, 1),
	}
	w.enqueueTimeoutNanos.Store(int64(5 * time.Millisecond))

	w.reqCh <- walRequest{recordType: WALRecordTypePut, key: []byte("x"), value: []byte("y"), done: make(chan error, 1)}

	start := time.Now()
	err := w.Write(WALRecordTypePut, []byte("k"), []byte("v"))
	if err == nil {
		t.Fatalf("expected error")
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("timeout too slow: %v", time.Since(start))
	}
	if w.enqueueTimeouts.Load() == 0 {
		t.Fatalf("expected enqueue timeout counter")
	}
}
