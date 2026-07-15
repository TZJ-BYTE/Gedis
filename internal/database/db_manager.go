package database

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/persistence"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

// DBManager 数据库管理器
type DBManager struct {
	databases []*Database
	lock      sync.RWMutex
	config    *config.Config

	// LSM 持久化支持
	persistenceEnabled bool
	dataDir            string
	lsmOptions         *persistence.Options
}

// NewDBManager 创建数据库管理器
func NewDBManager(cfg *config.Config) *DBManager {
	databases := make([]*Database, cfg.DBCount)

	// 检查是否启用持久化
	persistenceEnabled := cfg.PersistenceEnabled
	var dataDir string
	var lsmOptions *persistence.Options

	if persistenceEnabled && strings.Contains(strings.ToLower(os.Args[0]), ".test") {
		persistenceEnabled = false
	}
	runBackground := !strings.Contains(strings.ToLower(os.Args[0]), ".test")

	if persistenceEnabled {
		dataDir = cfg.DataDir
		if dataDir == "" {
			dataDir = "./data"
		}

		lsmOptions = persistence.DefaultOptions()
		// 可以根据需要调整 LSM 配置
		lsmOptions.BlockSize = cfg.BlockSize
		lsmOptions.MemTableSize = cfg.MemTableSize
		switch strings.ToLower(cfg.PersistenceDurability) {
		case "", "wal_fsync", "lsm":
			lsmOptions.SyncWAL = true
		case "wal":
			lsmOptions.SyncWAL = false
		default:
			lsmOptions.SyncWAL = true
		}

		// 存算分离配置
		lsmOptions.EnableOffloading = cfg.OffloadEnabled
		lsmOptions.OffloadBackend = cfg.OffloadBackend
		lsmOptions.OffloadEndpoint = cfg.OffloadEndpoint
		lsmOptions.OffloadAccessKey = cfg.OffloadAccessKey
		lsmOptions.OffloadSecretKey = cfg.OffloadSecretKey
		lsmOptions.OffloadBucket = cfg.OffloadBucket
		lsmOptions.OffloadUseSSL = cfg.OffloadUseSSL
		lsmOptions.OffloadRegion = cfg.OffloadRegion
		lsmOptions.OffloadMinLevel = cfg.OffloadMinLevel
		lsmOptions.OffloadKeepLocal = cfg.OffloadKeepLocal
		lsmOptions.OffloadFSRoot = cfg.OffloadFSRoot

		// 如果使用 minio，确保前缀区分不同 DB
		if cfg.OffloadEnabled && cfg.OffloadBackend == "minio" {
			logger.Info("启用存算分离 (Backend: MinIO, Endpoint: %s, Bucket: %s)", cfg.OffloadEndpoint, cfg.OffloadBucket)
		} else if cfg.OffloadEnabled && cfg.OffloadBackend == "fs" {
			logger.Info("启用存算分离 (Backend: FS)")
		}

		logger.Info("启用 Rust 持久化引擎，数据目录：%s", dataDir)
	}

	// 创建所有数据库
	for i := 0; i < cfg.DBCount; i++ {
		var db *Database
		var err error

		if persistenceEnabled {
			// 为每个数据库创建独立的子目录
			dbDataDir := filepath.Join(dataDir, fmt.Sprintf("db_%d", i))

			// 为每个 DB 配置独立的前缀（避免对象存储上 Key 冲突）
			dbOptions := *lsmOptions // 浅拷贝基础配置
			if dbOptions.EnableOffloading {
				base := cfg.OffloadBasePrefix
				if base != "" && !strings.HasSuffix(base, "/") {
					base += "/"
				}
				dbOptions.OffloadPrefix = base + fmt.Sprintf("db_%d/", i)
			}

			dbConfig := &DatabaseConfig{
				Type:              LSMPersistent,
				DataDir:           dbDataDir,
				Options:           &dbOptions,
				ColdStartStrategy: cfg.ColdStartStrategy,
				WriteMode:         cfg.PersistenceWriteMode,
				Durability:        cfg.PersistenceDurability,
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
		if runBackground {
			db.StartExpireCleaner()
		}
	}

	if persistenceEnabled {
		logger.Info("初始化 %d 个数据库（Rust 持久化模式）", cfg.DBCount)
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
func (m *DBManager) FlushAll() error {
	for _, db := range m.databases {
		if err := db.Clear(); err != nil {
			return err
		}
	}
	logger.Info("清空所有数据库")
	return nil
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
