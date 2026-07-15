package database

import (
	"fmt"

	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/internal/rustengine"
)

func rustSyncPolicy(durability string) rustengine.SyncPolicy {
	switch durability {
	case "wal", "flush":
		return rustengine.SyncFlush
	case "wal_fsync", "lsm", "sync":
		return rustengine.SyncData
	default:
		return rustengine.SyncNone
	}
}

func (db *Database) hasPersistence() bool {
	return db != nil && (db.rustEngine != nil || db.lsmEngine != nil)
}

func (db *Database) persistenceMode() string {
	switch {
	case db == nil:
		return "Memory"
	case db.rustEngine != nil:
		return "Rust"
	case db.lsmEngine != nil:
		return "LSM"
	default:
		return "Memory"
	}
}

func (db *Database) persistenceGet(key string) ([]byte, bool, error) {
	switch {
	case db == nil:
		return nil, false, nil
	case db.rustEngine != nil:
		return db.rustEngine.Get(stringToBytesRO(key))
	case db.lsmEngine != nil:
		value, found := db.lsmEngine.Get(stringToBytesRO(key))
		return value, found, nil
	default:
		return nil, false, nil
	}
}

func (db *Database) persistenceReset() error {
	switch {
	case db == nil:
		return nil
	case db.rustEngine != nil:
		return db.rustEngine.Reset()
	case db.lsmEngine != nil:
		return db.lsmEngine.Reset()
	default:
		return nil
	}
}

func (db *Database) persistenceClose() error {
	switch {
	case db == nil:
		return nil
	case db.rustEngine != nil:
		return db.rustEngine.Close()
	case db.lsmEngine != nil:
		return db.lsmEngine.Close()
	default:
		return nil
	}
}

func (db *Database) persistenceStats() map[string]interface{} {
	switch {
	case db == nil:
		return map[string]interface{}{}
	case db.rustEngine != nil:
		stats, err := db.rustEngine.Stats()
		if err != nil {
			return map[string]interface{}{
				"error": err.Error(),
			}
		}
		return map[string]interface{}{
			"keys":              stats.Keys,
			"active_segment_id": stats.ActiveSegmentID,
			"segment_count":     stats.SegmentCount,
			"cached_readers":    stats.CachedReaders,
			"pinned_segments":   stats.PinnedSegments,
			"pending_reclaims":  stats.PendingReclaims,
		}
	case db.lsmEngine != nil:
		return db.lsmEngine.GetStats()
	default:
		return map[string]interface{}{}
	}
}

func (db *Database) loadAllFromPersistence() error {
	switch {
	case db == nil:
		return fmt.Errorf("database is nil")
	case db.rustEngine != nil:
		allData, err := db.rustEngine.LoadAllKeys()
		if err != nil {
			return fmt.Errorf("failed to load keys from rust engine: %v", err)
		}
		for key, valueBytes := range allData {
			dataValue, err := deserializeDataValue(valueBytes)
			if err != nil || dataValue.IsExpired() {
				continue
			}
			shard := db.getShard(key)
			shard.lock.Lock()
			if shard.data == nil {
				shard.data = make(map[string]*datastruct.DataValue)
			}
			shard.data[key] = dataValue
			shard.lock.Unlock()
			db.updateMemoryUsage(int64(len(key)) + dataValue.ApproximateSize())
		}
		return nil
	case db.lsmEngine != nil:
		return db.loadAllFromLSM()
	default:
		return fmt.Errorf("persistence engine not initialized")
	}
}
