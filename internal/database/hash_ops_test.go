package database

import "testing"

func TestHashOps_HMGetWithExists(t *testing.T) {
	db := NewDatabase(0)
	if _, err := db.HSet("h", "f1", "v1"); err != nil {
		t.Fatalf("hset: %v", err)
	}
	values, ok, err := db.HMGetWithExists("h", []string{"f1", "f2"})
	if err != nil {
		t.Fatalf("hmget: %v", err)
	}
	if len(values) != 2 || len(ok) != 2 {
		t.Fatalf("lens")
	}
	if !ok[0] || values[0] != "v1" {
		t.Fatalf("f1")
	}
	if ok[1] {
		t.Fatalf("f2 should be missing")
	}
}

