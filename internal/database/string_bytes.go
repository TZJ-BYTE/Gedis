package database

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

func (db *Database) SetStringBytes(key, value []byte) error {
	if !db.evictIfNeeded() {
		return fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", atomic.LoadInt64(&db.usedMemory), db.maxMemory)
	}

	k := bytesToString(key)
	shard := db.getShard(k)

	var memDelta int64

	shard.lock.Lock()
	if shard.data == nil {
		shard.data = make(map[string]*datastruct.DataValue)
	}

	dv, exists := shard.data[k]
	if exists && dv != nil && !dv.IsExpired() {
		oldSize := dv.ApproximateSize()
		dv.ExpireTime = 0
		switch vv := dv.Value.(type) {
		case *datastruct.BytesString:
			if cap(vv.Data) >= len(value) {
				vv.Data = vv.Data[:len(value)]
			} else {
				vv.Data = make([]byte, len(value))
			}
			copy(vv.Data, value)
		case *datastruct.String:
			b := make([]byte, len(value))
			copy(b, value)
			dv.Value = &datastruct.BytesString{Data: b}
		default:
			b := make([]byte, len(value))
			copy(b, value)
			dv.Value = &datastruct.BytesString{Data: b}
		}
		dv.LastAccessedAt = time.Now().UnixMilli()
		memDelta = dv.ApproximateSize() - oldSize
	} else {
		if !exists {
			k = strings.Clone(k)
		}
		b := make([]byte, len(value))
		copy(b, value)
		dv = &datastruct.DataValue{
			Value:          &datastruct.BytesString{Data: b},
			ExpireTime:     0,
			LastAccessedAt: time.Now().UnixMilli(),
		}
		shard.data[k] = dv
		memDelta = int64(len(k)) + dv.ApproximateSize()
	}
	shard.lock.Unlock()

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}

	if db.hasPersistence() {
		dataBytes, err := dv.Serialize()
		if err != nil {
			db.recordLSMError(err)
			if db.persistenceWriteMode() == "weak" {
				logger.Error("Failed to serialize value for key %s: %v", k, err)
			} else {
				return err
			}
		} else {
			if err := db.lsmPut(k, dataBytes); err != nil {
				return err
			}
		}
	}

	return nil
}

func (db *Database) SetStringBytesWithOptions(key, value []byte, ttlMs int64, keepTTL bool, nx bool, xx bool) (bool, error) {
	if nx && xx {
		return false, fmt.Errorf("ERR syntax error")
	}
	if keepTTL && ttlMs != 0 {
		return false, fmt.Errorf("ERR syntax error")
	}
	if !db.evictIfNeeded() {
		return false, fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", atomic.LoadInt64(&db.usedMemory), db.maxMemory)
	}

	k := bytesToString(key)
	shard := db.getShard(k)
	nowMs := time.Now().UnixMilli()

	var memDelta int64
	var stableKey string
	var dv *datastruct.DataValue

	shard.lock.Lock()
	if shard.data == nil {
		shard.data = make(map[string]*datastruct.DataValue)
	}

	cur, exists := shard.data[k]
	if exists && cur != nil && cur.IsExpired() {
		delete(shard.data, k)
		memDelta -= int64(len(k)) + cur.ApproximateSize()
		cur = nil
		exists = false
	}

	if nx && exists {
		shard.lock.Unlock()
		if memDelta != 0 {
			db.updateMemoryUsage(memDelta)
		}
		return false, nil
	}
	if xx && !exists {
		shard.lock.Unlock()
		if memDelta != 0 {
			db.updateMemoryUsage(memDelta)
		}
		return false, nil
	}

	b := make([]byte, len(value))
	copy(b, value)

	if exists && cur != nil && (cur.ExpireTime == 0 || nowMs <= cur.ExpireTime) {
		oldSize := cur.ApproximateSize()
		cur.Value = &datastruct.BytesString{Data: b}
		if !keepTTL {
			if ttlMs > 0 {
				cur.ExpireTime = nowMs + ttlMs
			} else {
				cur.ExpireTime = 0
			}
		}
		cur.LastAccessedAt = nowMs
		memDelta += cur.ApproximateSize() - oldSize
		stableKey = k
		dv = cur
	} else {
		stableKey = strings.Clone(k)
		expireAt := int64(0)
		if ttlMs > 0 {
			expireAt = nowMs + ttlMs
		}
		dv = &datastruct.DataValue{
			Value:          &datastruct.BytesString{Data: b},
			ExpireTime:     expireAt,
			LastAccessedAt: nowMs,
		}
		shard.data[stableKey] = dv
		memDelta += int64(len(stableKey)) + dv.ApproximateSize()
	}
	shard.lock.Unlock()

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}

	if db.hasPersistence() {
		dataBytes, err := dv.Serialize()
		if err != nil {
			db.recordLSMError(err)
			if db.persistenceWriteMode() == "weak" {
				logger.Error("Failed to serialize value for key %s: %v", stableKey, err)
				return true, nil
			}
			return true, err
		}
		if err := db.lsmPut(stableKey, dataBytes); err != nil {
			return true, err
		}
	}

	return true, nil
}

func (db *Database) IncrStringBytes(key []byte) (int64, error) {
	return db.incrByStringBytes(key, 1)
}

func (db *Database) DecrStringBytes(key []byte) (int64, error) {
	return db.incrByStringBytes(key, -1)
}

func (db *Database) incrByStringBytes(key []byte, delta int64) (int64, error) {
	if !db.evictIfNeeded() {
		return 0, fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", atomic.LoadInt64(&db.usedMemory), db.maxMemory)
	}

	k := bytesToString(key)
	shard := db.getShard(k)

	var out int64
	var memDelta int64
	var dv *datastruct.DataValue

	shard.lock.Lock()
	if shard.data == nil {
		shard.data = make(map[string]*datastruct.DataValue)
	}

	cur, exists := shard.data[k]
	if !exists || cur == nil || cur.IsExpired() {
		if !exists {
			k = strings.Clone(k)
		}
		out = delta
		b := make([]byte, 0, 32)
		b = strconv.AppendInt(b, out, 10)
		dv = &datastruct.DataValue{
			Value:          &datastruct.BytesString{Data: b},
			ExpireTime:     0,
			LastAccessedAt: time.Now().UnixMilli(),
		}
		shard.data[k] = dv
		memDelta = int64(len(k)) + dv.ApproximateSize()
	} else {
		dv = cur
		oldSize := dv.ApproximateSize()
		var s string
		switch vv := dv.Value.(type) {
		case *datastruct.BytesString:
			s = bytesToString(vv.Data)
		case *datastruct.String:
			s = vv.Data
		default:
			shard.lock.Unlock()
			return 0, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
		}

		current, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			shard.lock.Unlock()
			return 0, fmt.Errorf("ERR value is not an integer or out of range")
		}

		out = current + delta
		switch vv := dv.Value.(type) {
		case *datastruct.BytesString:
			vv.Data = strconv.AppendInt(vv.Data[:0], out, 10)
		case *datastruct.String:
			b := make([]byte, 0, 32)
			b = strconv.AppendInt(b, out, 10)
			dv.Value = &datastruct.BytesString{Data: b}
		}
		dv.LastAccessedAt = time.Now().UnixMilli()
		memDelta = dv.ApproximateSize() - oldSize
	}
	shard.lock.Unlock()

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}

	if db.hasPersistence() {
		dataBytes, err := dv.Serialize()
		if err != nil {
			db.recordLSMError(err)
			if db.persistenceWriteMode() == "weak" {
				logger.Error("Failed to serialize value for key %s: %v", k, err)
			} else {
				return 0, err
			}
		} else {
			if err := db.lsmPut(k, dataBytes); err != nil {
				return 0, err
			}
		}
	}

	return out, nil
}
