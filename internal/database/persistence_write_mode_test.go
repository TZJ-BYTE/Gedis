package database

import (
	"testing"

	"github.com/TZJ-BYTE/RediGo/internal/persistence"
)

func TestPersistenceWriteMode_StrongReturnsError(t *testing.T) {
	dir := t.TempDir()
	opts := persistence.DefaultOptions()
	opts.EnableOffloading = false
	opts.SyncWAL = false

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

	_ = db.persistenceClose()
	if err := db.SetStringBytes([]byte("k1"), []byte("v1")); err == nil {
		t.Fatalf("expected error")
	}
}

func TestPersistenceWriteMode_WeakSwallowsErrorAndRecords(t *testing.T) {
	dir := t.TempDir()
	opts := persistence.DefaultOptions()
	opts.EnableOffloading = false
	opts.SyncWAL = false

	db, err := NewDatabaseWithConfig(0, &DatabaseConfig{
		Type:      LSMPersistent,
		DataDir:   dir,
		Options:   opts,
		WriteMode: "weak",
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	_ = db.persistenceClose()
	if err := db.SetStringBytes([]byte("k1"), []byte("v1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if db.lsmPutErrors.Load() == 0 {
		t.Fatalf("expected lsm put error counter")
	}
}
