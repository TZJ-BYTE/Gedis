package database

import (
	"fmt"
	"sync"
	"time"
	
	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/internal/persistence"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

// DatabaseType 数据库类型
type DatabaseType int

const (
	// MemoryOnly 纯内存模式（默认）
	MemoryOnly DatabaseType = iota
	// LSMPersistent LSM 持久化模式
	LSMPersistent
)

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Type       DatabaseType          // 数据库类型
	DataDir    string                // 数据目录（仅 LSM 模式需要）
	Options    *persistence.Options  // LSM 选项（仅 LSM 模式需要）
}

// Database 数据库结构
type Database struct {
	id         int
	data       map[string]*datastruct.DataValue  // 内存数据
	lock       sync.RWMutex
	
	// LSM 引擎（可选）
	lsmEngine  *persistence.LSMEnergy
	config     *DatabaseConfig
}

// DefaultDatabaseConfig 返回默认配置
func DefaultDatabaseConfig() *DatabaseConfig {
	return &DatabaseConfig{
		Type:    MemoryOnly,
		DataDir: "",
	}
}

// NewDatabase 创建新数据库（使用默认配置，纯内存）
func NewDatabase(id int) *Database {
	return &Database{
		id:     id,
		data:   make(map[string]*datastruct.DataValue),
		config: DefaultDatabaseConfig(),
	}
}

// NewDatabaseWithConfig 使用配置创建数据库
func NewDatabaseWithConfig(id int, config *DatabaseConfig) (*Database, error) {
	db := &Database{
		id:     id,
		data:   make(map[string]*datastruct.DataValue),
		config: config,
	}
	
	// 如果是 LSM 持久化模式，初始化 LSM 引擎
	if config.Type == LSMPersistent {
		if config.DataDir == "" {
			return nil, fmt.Errorf("data directory is required for LSM mode")
		}
		
		options := config.Options
		if options == nil {
			options = persistence.DefaultOptions()
		}
		
		var err error
		db.lsmEngine, err = persistence.OpenLSMEnergy(config.DataDir, options)
		if err != nil {
			return nil, fmt.Errorf("failed to open LSM engine: %v", err)
		}
		
		// 根据冷启动策略加载数据
		strategy := getColdStartStrategyFromConfig()
		switch strategy {
		case "load_all":
			// 全量加载到内存
			if err := db.loadAllFromLSM(); err != nil {
				logger.Warn("Failed to load all data from LSM: %v", err)
			}
		case "lazy_load":
			// 懒加载：不主动加载，读取时 fallback
			logger.Info("LSM lazy load enabled, will fallback on read")
		default:
			// 不加载（默认）
			logger.Info("LSM cold start: no data loading")
		}
	}
	
	return db, nil
}

// getColdStartStrategyFromConfig 从全局配置读取冷启动策略
func getColdStartStrategyFromConfig() string {
	cfg := config.DefaultConfig()
	// 将字符串配置转换为对应的策略
	switch cfg.ColdStartStrategy {
	case "load_all":
		return "load_all"
	case "lazy_load":
		return "lazy_load"
	default:
		return "no_load"
	}
}

// loadAllFromLSM 从 LSM 全量加载所有数据到内存
func (db *Database) loadAllFromLSM() error {
	if db.lsmEngine == nil {
		return fmt.Errorf("LSM engine not initialized")
	}
	
	logger.Info("Loading all data from LSM into memory...")
	logger.Info("LSM Engine SSTable count: %d", db.lsmEngine.GetSSTableCount())
	
	// 使用 LSM Engine 提供的公开方法加载所有键值对
	allData, err := db.lsmEngine.LoadAllKeys()
	if err != nil {
		return fmt.Errorf("failed to load keys from LSM: %v", err)
	}
	
	logger.Info("LoadAllKeys returned %d keys", len(allData))
	
	keysLoaded := 0
	deserializeErrors := 0
	
	for key, valueBytes := range allData {
		// 反序列化 DataValue
		dataValue, err := datastruct.DeserializeDataValue(valueBytes)
		if err != nil {
			logger.Warn("Failed to deserialize key %s: %v", key, err)
			logger.Warn("  Value bytes (first 50): %v", valueBytes[:min(50, len(valueBytes))])
			logger.Warn("  Value size: %d", len(valueBytes))
			deserializeErrors++
			continue
		}
		
		db.data[key] = dataValue
		keysLoaded++
		logger.Debug("Loaded key: %s, value type: %T", key, dataValue.Value)
	}
	
	logger.Info("Loaded %d keys from LSM into memory (%d deserialize errors)", keysLoaded, deserializeErrors)
	return nil
}

// deserializeDataValue 反序列化 DataValue
func deserializeDataValue(data []byte) (*datastruct.DataValue, error) {
	return datastruct.DeserializeDataValue(data)
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
	
	// 如果启用了 LSM，同时写入 LSM 引擎
	if db.lsmEngine != nil {
		// 将数据序列化后写入 LSM
		dataBytes, err := value.Serialize()
		if err == nil {
			db.lsmEngine.Put([]byte(key), dataBytes)
		}
	}
}

// Delete 删除键
func (db *Database) Delete(key string) bool {
	db.lock.Lock()
	defer db.lock.Unlock()
	
	_, exists := db.data[key]
	if exists {
		delete(db.data, key)
		
		// 如果启用了 LSM，同时从 LSM 删除
		if db.lsmEngine != nil {
			db.lsmEngine.Delete([]byte(key))
		}
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
	
	// 设置为绝对时间戳（当前时间 + TTL 毫秒）
	value.ExpireTime = time.Now().UnixMilli() + milliseconds
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
	
	// 如果启用了 LSM，清空 LSM 引擎（通过删除所有 SSTable）
	// 注意：这是一个重量级操作，实际实现可能需要优化
	if db.lsmEngine != nil {
		// TODO: 实现 LSM 的清空操作
	}
}

// Close 关闭数据库
func (db *Database) Close() error {
	db.lock.Lock()
	defer db.lock.Unlock()
	
	if db.lsmEngine != nil {
		return db.lsmEngine.Close()
	}
	return nil
}

// GetStats 获取数据库统计信息
func (db *Database) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})
	
	db.lock.RLock()
	stats["memory_keys"] = len(db.data)
	db.lock.RUnlock()
	
	if db.lsmEngine != nil {
		stats["mode"] = "LSM"
		// TODO: 添加 LSM 引擎的 GetStats 方法
		stats["lsm_enabled"] = true
	} else {
		stats["mode"] = "Memory"
		stats["lsm_enabled"] = false
	}
	
	return stats
}
