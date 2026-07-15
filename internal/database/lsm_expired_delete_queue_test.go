package database

import (
	"testing"
	"time"

	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/internal/persistence"
)

func TestLSMExpiredKeyIsDeletedViaQueue(t *testing.T) {
	dir := t.TempDir()
	opts := persistence.DefaultOptions()
	opts.EnableOffloading = false
	opts.UseCache = false
	opts.SyncWAL = false
	opts.ValueThreshold = -1

	db, err := NewDatabaseWithConfig(0, &DatabaseConfig{
		Type:      LSMPersistent,
		DataDir:   dir,
		Options:   opts,
		WriteMode: "strong",
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	now := time.Now().UnixMilli()
	dv := &datastruct.DataValue{
		Value:          &datastruct.String{Data: "v"},
		ExpireTime:     now - 1,
		LastAccessedAt: now,
	}
	b, err := dv.Serialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	if err := db.lsmPut("k", b); err != nil {
		t.Fatalf("lsm put: %v", err)
	}

	if _, ok := db.Get("k"); ok {
		t.Fatalf("expected expired key to be treated as not found")
	}
	if db.lsmDeleteEnq.Load() == 0 {
		t.Fatalf("expected expired delete to be enqueued")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, found, err := db.persistenceGet("k"); err == nil && !found {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected expired key to be deleted from lsm")
}
