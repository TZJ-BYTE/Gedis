package database

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

func (db *Database) HSet(key, field, value string) (int64, error) {
	if !db.evictIfNeeded() {
		return 0, fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", db.usedMemory, db.maxMemory)
	}

	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	var memDelta int64
	var stableKey string
	var lsmBytes []byte
	var created int64
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
	var h *datastruct.Hash
	if dv.Value == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		h = &datastruct.Hash{Data: make(map[string]string)}
		dv.Value = h
		dv.ExpireTime = 0
		created = 1
	} else {
		hv, ok := dv.Value.(*datastruct.Hash)
		if !ok {
			sh.lock.Unlock()
			return 0, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		h = hv
		if h.Data == nil {
			h.Data = make(map[string]string)
		}
		if _, ok := h.Data[field]; !ok {
			created = 1
		}
	}

	h.Data[strings.Clone(field)] = strings.Clone(value)
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
	return created, nil
}

func (db *Database) HMSet(key string, fields []string, values []string) error {
	if len(fields) != len(values) {
		return fmt.Errorf("ERR wrong number of arguments for 'hmset' command")
	}
	if len(fields) == 0 {
		return nil
	}
	if !db.evictIfNeeded() {
		return fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", db.usedMemory, db.maxMemory)
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
	var h *datastruct.Hash
	if dv.Value == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		h = &datastruct.Hash{Data: make(map[string]string)}
		dv.Value = h
		dv.ExpireTime = 0
	} else {
		hv, ok := dv.Value.(*datastruct.Hash)
		if !ok {
			sh.lock.Unlock()
			return fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		h = hv
		if h.Data == nil {
			h.Data = make(map[string]string)
		}
	}

	for i := range fields {
		h.Data[strings.Clone(fields[i])] = strings.Clone(values[i])
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
			return serErr
		}
	}

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}
	if lsmBytes != nil {
		if err := db.lsmPut(stableKey, lsmBytes); err != nil {
			return err
		}
	}
	return nil
}

func (db *Database) HGet(key, field string) (string, bool, error) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return "", false, nil
	}
	h, ok := dv.Value.(*datastruct.Hash)
	if !ok {
		sh.lock.RUnlock()
		return "", false, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	if h.Data == nil {
		sh.lock.RUnlock()
		return "", false, nil
	}
	v, ok := h.Data[field]
	sh.lock.RUnlock()
	return v, ok, nil
}

func (db *Database) HMGet(key string, fields []string) ([]string, error) {
	out := make([]string, len(fields))
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return out, nil
	}
	h, ok := dv.Value.(*datastruct.Hash)
	if !ok {
		sh.lock.RUnlock()
		return nil, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	for i, f := range fields {
		if h.Data == nil {
			out[i] = ""
			continue
		}
		out[i] = h.Data[f]
	}
	sh.lock.RUnlock()
	return out, nil
}

func (db *Database) HMGetWithExists(key string, fields []string) ([]string, []bool, error) {
	out := make([]string, len(fields))
	ok := make([]bool, len(fields))
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return out, ok, nil
	}
	h, ht := dv.Value.(*datastruct.Hash)
	if !ht {
		sh.lock.RUnlock()
		return nil, nil, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	for i, f := range fields {
		if h.Data == nil {
			continue
		}
		v, okk := h.Data[f]
		if okk {
			out[i] = v
			ok[i] = true
		}
	}
	sh.lock.RUnlock()
	return out, ok, nil
}

func (db *Database) HDel(key string, fields []string) (int64, error) {
	if len(fields) == 0 {
		return 0, nil
	}

	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	var memDelta int64
	var deleted int64
	var lsmBytes []byte
	var deleteKey bool
	var serErr error

	sh.lock.Lock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.Unlock()
		return 0, nil
	}
	h, ok := dv.Value.(*datastruct.Hash)
	if !ok {
		sh.lock.Unlock()
		return 0, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}

	oldSize := dv.ApproximateSize()
	if h.Data != nil {
		for _, f := range fields {
			if _, ok := h.Data[f]; ok {
				delete(h.Data, f)
				deleted++
			}
		}
	}
	if h.Size() == 0 {
		val := sh.data[key]
		delete(sh.data, key)
		deleteKey = true
		memDelta = -int64(len(key)) - val.ApproximateSize()
	} else {
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
	}
	sh.lock.Unlock()

	if serErr != nil {
		db.recordLSMError(serErr)
		if db.persistenceWriteMode() == "weak" {
			logger.Error("Failed to serialize value for key %s: %v", key, serErr)
		} else {
			return 0, serErr
		}
	}

	if memDelta != 0 {
		db.updateMemoryUsage(memDelta)
	}
	if deleteKey {
		if err := db.lsmDelete(key); err != nil {
			return 0, err
		}
	} else if lsmBytes != nil {
		if err := db.lsmPut(key, lsmBytes); err != nil {
			return 0, err
		}
	}
	return deleted, nil
}

func (db *Database) HLen(key string) (int64, error) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return 0, nil
	}
	h, ok := dv.Value.(*datastruct.Hash)
	if !ok {
		sh.lock.RUnlock()
		return 0, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	n := int64(h.Size())
	sh.lock.RUnlock()
	return n, nil
}

func (db *Database) HExists(key, field string) (bool, error) {
	_, ok, err := db.HGet(key, field)
	return ok, err
}

func (db *Database) HKeys(key string) ([]string, error) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return []string{}, nil
	}
	h, ok := dv.Value.(*datastruct.Hash)
	if !ok {
		sh.lock.RUnlock()
		return nil, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	out := make([]string, 0, len(h.Data))
	for k := range h.Data {
		out = append(out, k)
	}
	sort.Strings(out)
	sh.lock.RUnlock()
	return out, nil
}

func (db *Database) HVals(key string) ([]string, error) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return []string{}, nil
	}
	h, ok := dv.Value.(*datastruct.Hash)
	if !ok {
		sh.lock.RUnlock()
		return nil, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	out := make([]string, 0, len(h.Data))
	for _, v := range h.Data {
		out = append(out, v)
	}
	sh.lock.RUnlock()
	return out, nil
}

func (db *Database) HGetAll(key string) ([]string, error) {
	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	sh.lock.RLock()
	dv, exists := sh.data[key]
	if !exists || dv == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		sh.lock.RUnlock()
		return []string{}, nil
	}
	h, ok := dv.Value.(*datastruct.Hash)
	if !ok {
		sh.lock.RUnlock()
		return nil, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
	}
	keys := make([]string, 0, len(h.Data))
	for k := range h.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, 2*len(keys))
	for _, k := range keys {
		out = append(out, k, h.Data[k])
	}
	sh.lock.RUnlock()
	return out, nil
}

func (db *Database) HIncrBy(key, field string, delta int64) (int64, error) {
	if !db.evictIfNeeded() {
		return 0, fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", db.usedMemory, db.maxMemory)
	}

	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	var memDelta int64
	var stableKey string
	var lsmBytes []byte
	var out int64
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
	var h *datastruct.Hash
	if dv.Value == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		h = &datastruct.Hash{Data: make(map[string]string)}
		dv.Value = h
		dv.ExpireTime = 0
	} else {
		hv, ok := dv.Value.(*datastruct.Hash)
		if !ok {
			sh.lock.Unlock()
			return 0, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		h = hv
		if h.Data == nil {
			h.Data = make(map[string]string)
		}
	}

	cur := int64(0)
	if s, ok := h.Data[field]; ok && s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			sh.lock.Unlock()
			return 0, fmt.Errorf("ERR hash value is not an integer")
		}
		cur = v
	}
	out = cur + delta
	h.Data[strings.Clone(field)] = strconv.FormatInt(out, 10)

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
	return out, nil
}

func (db *Database) HIncrByFloat(key, field string, delta float64) (float64, error) {
	if !db.evictIfNeeded() {
		return 0, fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", db.usedMemory, db.maxMemory)
	}

	sh := db.getShard(key)
	now := time.Now().UnixMilli()

	var memDelta int64
	var stableKey string
	var lsmBytes []byte
	var out float64
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
	var h *datastruct.Hash
	if dv.Value == nil || (dv.ExpireTime > 0 && now > dv.ExpireTime) {
		h = &datastruct.Hash{Data: make(map[string]string)}
		dv.Value = h
		dv.ExpireTime = 0
	} else {
		hv, ok := dv.Value.(*datastruct.Hash)
		if !ok {
			sh.lock.Unlock()
			return 0, fmt.Errorf("WRONGTYPE Operation against a key holding the wrong kind of value")
		}
		h = hv
		if h.Data == nil {
			h.Data = make(map[string]string)
		}
	}

	cur := float64(0)
	if s, ok := h.Data[field]; ok && s != "" {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			sh.lock.Unlock()
			return 0, fmt.Errorf("ERR hash value is not a float")
		}
		cur = v
	}
	out = cur + delta
	h.Data[strings.Clone(field)] = strconv.FormatFloat(out, 'f', -1, 64)

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
	return out, nil
}
