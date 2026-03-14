package config

// ColdStartStrategy 冷启动数据加载策略
type ColdStartStrategy int

const (
	// NoLoad 不加载（默认，快速启动）
	NoLoad ColdStartStrategy = iota
	// LoadAll 启动时全量加载到内存
	LoadAll
	// LazyLoad 懒加载（读取时 fallback 到 LSM）
	LazyLoad
)

// Config 服务器配置
type Config struct {
	Host string `yaml:"host" json:"host"`
	Port int    `yaml:"port" json:"port"`
	
	// 数据库配置
	DBCount int `yaml:"db_count" json:"db_count"`
	
	// 内存配置
	MaxMemory int64 `yaml:"max_memory" json:"max_memory"`
	
	// 持久化配置
	PersistenceEnabled bool              `yaml:"persistence_enabled" json:"persistence_enabled"`
	PersistenceType    string            `yaml:"persistence_type" json:"persistence_type"` // "aof", "lsm", "hybrid"
	DataDir            string            `yaml:"data_dir" json:"data_dir"`
	ColdStartStrategy  string            `yaml:"cold_start_strategy" json:"cold_start_strategy"` // "no_load", "load_all", "lazy_load"
	AOFEnabled         bool              `yaml:"aof_enabled" json:"aof_enabled"`  // 保留向后兼容
	AOFPath            string            `yaml:"aof_path" json:"aof_path"`
	RDBPath            string            `yaml:"rdb_path" json:"rdb_path"`
	
	// LSM 配置
	BlockSize       int   `yaml:"block_size" json:"block_size"`           // SSTable block 大小
	MemTableSize    int   `yaml:"memtable_size" json:"memtable_size"`     // MemTable 最大大小
	WriteBufferSize int64 `yaml:"write_buffer_size" json:"write_buffer_size"`
	MaxOpenFiles    int   `yaml:"max_open_files" json:"max_open_files"`
	BloomFilterBits int   `yaml:"bloom_filter_bits" json:"bloom_filter_bits"`
	
	// 日志配置
	LogLevel string `yaml:"log_level" json:"log_level"`
	LogPath  string `yaml:"log_path" json:"log_path"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Host:      "0.0.0.0",
		Port:      16379,  // 修改为 16379，避免与 Redis 的 6379 冲突
		DBCount:   16,
		MaxMemory: 256 * 1024 * 1024, // 256MB
		
		// 持久化配置（默认启用 LSM）
		PersistenceEnabled: true,
		PersistenceType:    "lsm",  // 默认使用 LSM
		DataDir:            "./data",
		ColdStartStrategy:  "load_all", // 修改为全量加载，测试数据恢复
		
		// AOF 配置（保留向后兼容）
		AOFEnabled: false,  // 默认关闭 AOF，使用 LSM 替代
		AOFPath:    "./data/appendonly.aof",
		RDBPath:    "./data/dump.rdb",
		
		// LSM 配置
		BlockSize:       4096,    // 4KB
		MemTableSize:    4 << 20, // 4MB
		WriteBufferSize: 64 << 20, // 64MB
		MaxOpenFiles:    1000,
		BloomFilterBits: 10,
		
		// 日志配置
		LogLevel: "info",
		LogPath:  "./logs/gedis.log",
	}
}

// GetColdStartStrategy 解析冷启动策略
func (c *Config) GetColdStartStrategy() ColdStartStrategy {
	switch c.ColdStartStrategy {
	case "load_all":
		return LoadAll
	case "lazy_load":
		return LazyLoad
	default:
		return NoLoad
	}
}
