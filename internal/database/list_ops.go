package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

func (db *Database) LPush(key string, values []string) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}
	if !db.evictIfNeeded() {
		return 0, fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", db.usedMemory, db.maxMemory)
	}

	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	var memDelta int64
	var stableKey string
	var lsmBytes []byte
	var serErr error

	sh.lock.Lock()
	if sh.data == nil {
		sh.data = make(map[string]*datastruct.DataValue)
	}
	dv, exists := sh.data[key]
	if !exists {
		stableKey = strings.Clone(key)
		dv = &datastruct.DataValue{ExpireTime: 0, LastAccessedAt: now}
		sh.data[stableKey] = dv
		memDelta += int64(len(stableKey))
	} else {
		stableKey = key
	}

	oldSize := dv.ApproximateSize()
	var list *datastruct.List
	if dv.Value == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		list = &datastruct.List{Data: make([]string, 0)}
		dv.Value = list
		dv.ExpireTime = 0
	} else {
		l, ok := dv.Value.(*datastruct.List)
		if !ok {
			sh.lock.Unlock()
			return 0, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		list = l
	}

	n := len(values)
	newData := make([]string, n+len(list.Data))
	for i := 0; i < n; i++ {
		newData[n-1-i] = strings.Clone(values[i])
	}
	copy(newData[n:], list.Data)
	list.Data = newData

	dv.LastAccessedAt = now
	newSize := dv.ApproximateSize()
	memDelta += newSize - oldSize

	if db.hasPersistence() {
		b, err := dv.Serialize()
		if err == nil {
			lsmBytes = b
		} else {
			serErr = err
		}
	}
	sh.lock.Unlock()

	if serErr != nil {
		db.recordLSMError(serErr)
		if db.persistenceWriteMode() == "weak" {
			logger.Error("Failed to serialize value for key %s: %v", stableKey, serErr)
		} else {
			return 0, serErr
		}
	}

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}
	if lsmBytes != nil {
		if err := db.lsmPut(stableKey, lsmBytes); err != nil {
			return 0, err
		}
	}
	return len(list.Data), nil
}

func (db *Database) RPush(key string, values []string) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}
	if !db.evictIfNeeded() {
		return 0, fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", db.usedMemory, db.maxMemory)
	}

	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	var memDelta int64
	var stableKey string
	var lsmBytes []byte
	var serErr error

	sh.lock.Lock()
	if sh.data == nil {
		sh.data = make(map[string]*datastruct.DataValue)
	}
	dv, exists := sh.data[key]
	if !exists {
		stableKey = strings.Clone(key)
		dv = &datastruct.DataValue{ExpireTime: 0, LastAccessedAt: now}
		sh.data[stableKey] = dv
		memDelta += int64(len(stableKey))
	} else {
		stableKey = key
	}

	oldSize := dv.ApproximateSize()
	var list *datastruct.List
	if dv.Value == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		list = &datastruct.List{Data: make([]string, 0)}
		dv.Value = list
		dv.ExpireTime = 0
	} else {
		l, ok := dv.Value.(*datastruct.List)
		if !ok {
			sh.lock.Unlock()
			return 0, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		list = l
	}

	for i := range values {
		list.Data = append(list.Data, strings.Clone(values[i]))
	}
	dv.LastAccessedAt = now
	newSize := dv.ApproximateSize()
	memDelta += newSize - oldSize

	if db.hasPersistence() {
		b, err := dv.Serialize()
		if err == nil {
			lsmBytes = b
		} else {
			serErr = err
		}
	}
	sh.lock.Unlock()

	if serErr != nil {
		db.recordLSMError(serErr)
		if db.persistenceWriteMode() == "weak" {
			logger.Error("Failed to serialize value for key %s: %v", stableKey, serErr)
		} else {
			return 0, serErr
		}
	}

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}
	if lsmBytes != nil {
		if err := db.lsmPut(stableKey, lsmBytes); err != nil {
			return 0, err
		}
	}
	return len(list.Data), nil
}

func (db *Database) LPop(key string) (string, bool, error) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	var memDelta int64
	var lsmBytes []byte
	var serErr error

	sh.lock.Lock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.Unlock()
		return "", false, nil
	}
	list, ok := dv.Value.(*datastruct.List)
	if !ok {
		sh.lock.Unlock()
		return "", false, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}

	oldSize := dv.ApproximateSize()
	v, ok := list.PopLeft()
	if !ok {
		sh.lock.Unlock()
		return "", false, nil
	}
	dv.LastAccessedAt = now
	newSize := dv.ApproximateSize()
	memDelta = newSize - oldSize

	if db.hasPersistence() {
		b, err := dv.Serialize()
		if err == nil {
			lsmBytes = b
		} else {
			serErr = err
		}
	}
	sh.lock.Unlock()

	if serErr != nil {
		db.recordLSMError(serErr)
		if db.persistenceWriteMode() == "weak" {
			logger.Error("Failed to serialize value for key %s: %v", key, serErr)
		} else {
			return "", false, serErr
		}
	}

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}
	if lsmBytes != nil {
		if err := db.lsmPut(key, lsmBytes); err != nil {
			return "", false, err
		}
	}
	return v, true, nil
}

func (db *Database) RPop(key string) (string, bool, error) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	var memDelta int64
	var lsmBytes []byte
	var serErr error

	sh.lock.Lock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.Unlock()
		return "", false, nil
	}
	list, ok := dv.Value.(*datastruct.List)
	if !ok {
		sh.lock.Unlock()
		return "", false, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}

	oldSize := dv.ApproximateSize()
	v, ok := list.PopRight()
	if !ok {
		sh.lock.Unlock()
		return "", false, nil
	}
	dv.LastAccessedAt = now
	newSize := dv.ApproximateSize()
	memDelta = newSize - oldSize

	if db.hasPersistence() {
		b, err := dv.Serialize()
		if err == nil {
			lsmBytes = b
		} else {
			serErr = err
		}
	}
	sh.lock.Unlock()

	if serErr != nil {
		db.recordLSMError(serErr)
		if db.persistenceWriteMode() == "weak" {
			logger.Error("Failed to serialize value for key %s: %v", key, serErr)
		} else {
			return "", false, serErr
		}
	}

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}
	if lsmBytes != nil {
		if err := db.lsmPut(key, lsmBytes); err != nil {
			return "", false, err
		}
	}
	return v, true, nil
}

func (db *Database) LLen(key string) (int, error) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return 0, nil
	}
	list, ok := dv.Value.(*datastruct.List)
	if !ok {
		sh.lock.RUnlock()
		return 0, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	n := len(list.Data)
	sh.lock.RUnlock()
	return n, nil
}

func (db *Database) LRange(key string, start, stop int) ([]string, error) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return []string{}, nil
	}
	list, ok := dv.Value.(*datastruct.List)
	if !ok {
		sh.lock.RUnlock()
		return nil, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	s := list.Range(start, stop)
	out := make([]string, len(s))
	copy(out, s)
	sh.lock.RUnlock()
	return out, nil
}
