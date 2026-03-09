package database

import (
	"sync"
	
	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

// DBManager 数据库管理器
type DBManager struct {
	databases []*Database
	currentDB int
	lock      sync.RWMutex
	config    *config.Config
}

// NewDBManager 创建数据库管理器
func NewDBManager(cfg *config.Config) *DBManager {
	databases := make([]*Database, cfg.DBCount)
	for i := 0; i < cfg.DBCount; i++ {
		databases[i] = NewDatabase(i)
	}
	
	logger.Info("初始化 %d 个数据库", cfg.DBCount)
	
	return &DBManager{
		databases: databases,
		currentDB: 0,
		config:    cfg,
	}
}

// GetDB 获取当前数据库
func (m *DBManager) GetDB() *Database {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.databases[m.currentDB]
}

// SelectDB 选择数据库
func (m *DBManager) SelectDB(index int) bool {
	if index < 0 || index >= len(m.databases) {
		return false
	}
	
	m.lock.Lock()
	defer m.lock.Unlock()
	
	m.currentDB = index
	logger.Info("切换到数据库 %d", index)
	return true
}

// GetDBByIndex 根据索引获取数据库
func (m *DBManager) GetDBByIndex(index int) (*Database, bool) {
	if index < 0 || index >= len(m.databases) {
		return nil, false
	}
	return m.databases[index], true
}

// FlushDB 清空当前数据库
func (m *DBManager) FlushDB() {
	m.GetDB().Clear()
	logger.Info("清空数据库 %d", m.currentDB)
}

// FlushAll 清空所有数据库
func (m *DBManager) FlushAll() {
	for _, db := range m.databases {
		db.Clear()
	}
	logger.Info("清空所有数据库")
}

// DBSize 返回当前数据库大小
func (m *DBManager) DBSize() int {
	return m.GetDB().Size()
}

// DBCcount 返回数据库数量
func (m *DBManager) DBCount() int {
	return len(m.databases)
}
