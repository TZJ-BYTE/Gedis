package database

import (
	"fmt"
	"sync"
	
	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
	"github.com/TZJ-BYTE/RediGo/internal/persistence"
)

// DBManager 数据库管理器
type DBManager struct {
	databases []*Database
	lock      sync.RWMutex
	config    *config.Config
	
	// LSM 持久化支持
	persistenceEnabled bool
	dataDir           string
	lsmOptions        *persistence.Options
}

// NewDBManager 创建数据库管理器
func NewDBManager(cfg *config.Config) *DBManager {
	databases := make([]*Database, cfg.DBCount)
	
	// 检查是否启用持久化
	persistenceEnabled := cfg.PersistenceEnabled
	var dataDir string
	var lsmOptions *persistence.Options
	
	if persistenceEnabled {
		dataDir = cfg.DataDir
		if dataDir == "" {
			dataDir = "./data"
		}
		
		lsmOptions = persistence.DefaultOptions()
		// 可以根据需要调整 LSM 配置
		lsmOptions.BlockSize = cfg.BlockSize
		lsmOptions.MemTableSize = cfg.MemTableSize
		
		logger.Info("启用 LSM 持久化，数据目录：%s", dataDir)
	}
	
	// 创建所有数据库
	for i := 0; i < cfg.DBCount; i++ {
		var db *Database
		var err error
		
		if persistenceEnabled {
			// 为每个数据库创建独立的子目录
			dbDataDir := fmt.Sprintf("%s/db_%d", dataDir, i)
			dbConfig := &DatabaseConfig{
				Type:    LSMPersistent,
				DataDir: dbDataDir,
				Options: lsmOptions,
			}
			
			db, err = NewDatabaseWithConfig(i, dbConfig)
			if err != nil {
				logger.Error("创建数据库 %d 失败：%v", i, err)
				// 如果创建失败，回退到内存模式
				db = NewDatabase(i)
			}
		} else {
			db = NewDatabase(i)
			logger.Debug("数据库 %d 使用纯内存模式", i)
		}
		
		databases[i] = db
	}
	
	if persistenceEnabled {
		logger.Info("初始化 %d 个数据库（LSM 持久化模式）", cfg.DBCount)
	} else {
		logger.Info("初始化 %d 个数据库（纯内存模式）", cfg.DBCount)
	}
	
	return &DBManager{
		databases:          databases,
		config:             cfg,
		persistenceEnabled: persistenceEnabled,
		dataDir:            dataDir,
		lsmOptions:         lsmOptions,
	}
}

// GetDefaultDB 获取默认数据库 (DB 0)
func (m *DBManager) GetDefaultDB() *Database {
	return m.databases[0]
}

// GetDBByIndex 根据索引获取数据库
func (m *DBManager) GetDBByIndex(index int) (*Database, error) {
	if index < 0 || index >= len(m.databases) {
		return nil, fmt.Errorf("DB index out of range")
	}
	return m.databases[index], nil
}

// FlushAll 清空所有数据库
func (m *DBManager) FlushAll() {
	for _, db := range m.databases {
		db.Clear()
	}
	logger.Info("清空所有数据库")
}

// DBCount 返回数据库数量
func (m *DBManager) DBCount() int {
	return len(m.databases)
}

// Close 关闭所有数据库
func (m *DBManager) Close() error {
	m.lock.Lock()
	defer m.lock.Unlock()
	
	var lastErr error
	for i, db := range m.databases {
		err := db.Close()
		if err != nil {
			logger.Error("关闭数据库 %d 失败：%v", i, err)
			lastErr = err
		}
	}
	
	if lastErr == nil {
		logger.Info("已关闭所有数据库")
	}
	
	return lastErr
}

// GetStats 获取统计信息
func (m *DBManager) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})
	
	stats["db_count"] = len(m.databases)
	stats["persistence_enabled"] = m.persistenceEnabled
	
	if m.persistenceEnabled {
		stats["data_dir"] = m.dataDir
	}
	
	// 收集所有数据库的统计信息
	dbStats := make([]map[string]interface{}, len(m.databases))
	for i, db := range m.databases {
		dbStats[i] = db.GetStats()
	}
	stats["databases"] = dbStats
	
	return stats
}
