package database

import (
	"sync"
	
	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
)

// Database 数据库结构
type Database struct {
	id   int
	data map[string]*datastruct.DataValue
	lock sync.RWMutex
}

// NewDatabase 创建新数据库
func NewDatabase(id int) *Database {
	return &Database{
		id:   id,
		data: make(map[string]*datastruct.DataValue),
	}
}

// Get 获取键值
func (db *Database) Get(key string) (*datastruct.DataValue, bool) {
	db.lock.RLock()
	defer db.lock.RUnlock()
	
	value, exists := db.data[key]
	if !exists {
		return nil, false
	}
	
	// 检查是否过期
	if value.IsExpired() {
		return nil, false
	}
	
	return value, true
}

// Set 设置键值
func (db *Database) Set(key string, value *datastruct.DataValue) {
	db.lock.Lock()
	defer db.lock.Unlock()
	
	db.data[key] = value
}

// Delete 删除键
func (db *Database) Delete(key string) bool {
	db.lock.Lock()
	defer db.lock.Unlock()
	
	_, exists := db.data[key]
	if exists {
		delete(db.data, key)
	}
	return exists
}

// Exists 检查键是否存在
func (db *Database) Exists(key string) bool {
	db.lock.RLock()
	defer db.lock.RUnlock()
	
	_, exists := db.data[key]
	return exists && !db.data[key].IsExpired()
}

// Expire 设置过期时间
func (db *Database) Expire(key string, milliseconds int64) bool {
	db.lock.Lock()
	defer db.lock.Unlock()
	
	value, exists := db.data[key]
	if !exists {
		return false
	}
	
	value.ExpireTime = milliseconds
	return true
}

// Keys 返回所有键
func (db *Database) Keys() []string {
	db.lock.RLock()
	defer db.lock.RUnlock()
	
	keys := make([]string, 0, len(db.data))
	for key, value := range db.data {
		if !value.IsExpired() {
			keys = append(keys, key)
		}
	}
	return keys
}

// Size 返回数据库大小
func (db *Database) Size() int {
	db.lock.RLock()
	defer db.lock.RUnlock()
	
	count := 0
	for _, value := range db.data {
		if !value.IsExpired() {
			count++
		}
	}
	return count
}

// Clear 清空数据库
func (db *Database) Clear() {
	db.lock.Lock()
	defer db.lock.Unlock()
	
	db.data = make(map[string]*datastruct.DataValue)
}
