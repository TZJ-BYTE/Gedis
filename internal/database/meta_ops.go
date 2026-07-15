package database

import (
	"errors"
	"strings"
	"time"

	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

func (db *Database) GetExpireTime(key string) (int64, bool) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()
	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return 0, false
	}
	exp := dv.ExpireTime
	sh.lock.RUnlock()
	return exp, true
}

func (db *Database) Rename(oldKey, newKey string, nx bool) (bool, error) {
	if oldKey == newKey {
		if nx {
			return false, nil
		}
		_, ok := db.GetExpireTime(oldKey)
		if !ok {
			return false, errors.New("ERR no such key")
		}
		return true, nil
	}

	oldIdx := getShardIndex(oldKey)
	newIdx := getShardIndex(newKey)
	first := oldIdx
	second := newIdx
	if first > second {
		first, second = second, first
	}
	s1 := &db.shards[first]
	s2 := &db.shards[second]
	if first == second {
		s2 = s1
	}

	var memDelta int64
	var lsmBytes []byte
	var doPut bool
	var doDeleteOld bool
	var stableNewKey string
	var serErr error

	s1.lock.Lock()
	if s2 != s1 {
		s2.lock.Lock()
	}

	now := time.Now().UnixMilli()

	oldShard := &db.shards[oldIdx]
	newShard := &db.shards[newIdx]
	oldVal, oldExists := oldShard.data[oldKey]
	if !oldExists || oldVal == nil || (oldVal.ExpireTime > 0 && now > oldVal.ExpireTime) {
		if s2 != s1 {
			s2.lock.Unlock()
		}
		s1.lock.Unlock()
		return false, errors.New("ERR no such key")
	}

	if nx {
		if nv, ok := newShard.data[newKey]; ok && nv != nil && !(nv.ExpireTime > 0 && now > nv.ExpireTime) {
			if s2 != s1 {
				s2.lock.Unlock()
			}
			s1.lock.Unlock()
			return false, nil
		}
	}

	if newShard.data == nil {
		newShard.data = make(map[string]*datastruct.DataValue)
	}

	newVal, newExists := newShard.data[newKey]
	var newOldSize int64
	if newVal != nil {
		newOldSize = newVal.ApproximateSize()
	}

	delete(oldShard.data, oldKey)

	stableNewKey = newKey
	if !newExists {
		stableNewKey = strings.Clone(newKey)
		memDelta += int64(len(stableNewKey))
	}
	newShard.data[stableNewKey] = oldVal

	if newExists {
		memDelta -= newOldSize
	}
	memDelta += -int64(len(oldKey))

	if db.hasPersistence() {
		b, err := oldVal.Serialize()
		if err == nil {
			lsmBytes = b
			doPut = true
			doDeleteOld = true
		} else {
			serErr = err
		}
	}

	if s2 != s1 {
		s2.lock.Unlock()
	}
	s1.lock.Unlock()

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}
	if serErr != nil {
		db.recordLSMError(serErr)
		if db.persistenceWriteMode() == "weak" {
			logger.Error("Failed to serialize value for key %s: %v", oldKey, serErr)
		} else {
			return false, serErr
		}
	}
	if doDeleteOld {
		if err := db.lsmDelete(oldKey); err != nil {
			return false, err
		}
	}
	if doPut {
		if err := db.lsmPut(stableNewKey, lsmBytes); err != nil {
			return false, err
		}
	}

	return true, nil
}
