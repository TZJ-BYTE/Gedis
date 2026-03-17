package config

import (
	"os"
	"strconv"
)

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
	Host        string `yaml:"host" json:"host"`
	Port        int    `yaml:"port" json:"port"`
	NetworkType string `yaml:"network_type" json:"network_type"` // "std", "gnet"

	// 数据库配置
	DBCount int `yaml:"db_count" json:"db_count"`

	// 内存配置
	MaxMemory       int64  `yaml:"max_memory" json:"max_memory"`
	MaxMemoryPolicy string `yaml:"max_memory_policy" json:"max_memory_policy"` // "noeviction", "allkeys-lru", "volatile-lru", "allkeys-random", "volatile-random"

	// 持久化配置
	PersistenceEnabled bool   `yaml:"persistence_enabled" json:"persistence_enabled"`
	PersistenceType    string `yaml:"persistence_type" json:"persistence_type"` // "aof", "lsm", "hybrid"
	DataDir            string `yaml:"data_dir" json:"data_dir"`
	ColdStartStrategy  string `yaml:"cold_start_strategy" json:"cold_start_strategy"` // "no_load", "load_all", "lazy_load"
	AOFEnabled         bool   `yaml:"aof_enabled" json:"aof_enabled"`                 // 保留向后兼容
	AOFPath            string `yaml:"aof_path" json:"aof_path"`
	RDBPath            string `yaml:"rdb_path" json:"rdb_path"`

	// LSM 配置
	BlockSize       int   `yaml:"block_size" json:"block_size"`       // SSTable block 大小
	MemTableSize    int   `yaml:"memtable_size" json:"memtable_size"` // MemTable 最大大小
	WriteBufferSize int64 `yaml:"write_buffer_size" json:"write_buffer_size"`
	MaxOpenFiles    int   `yaml:"max_open_files" json:"max_open_files"`
	BloomFilterBits int   `yaml:"bloom_filter_bits" json:"bloom_filter_bits"`

	// 日志配置
	LogLevel string `yaml:"log_level" json:"log_level"`
	LogPath  string `yaml:"log_path" json:"log_path"`

	// 存算分离配置
	OffloadEnabled    bool   `yaml:"offload_enabled" json:"offload_enabled"`
	OffloadBackend    string `yaml:"offload_backend" json:"offload_backend"` // "fs" or "minio"
	OffloadEndpoint   string `yaml:"offload_endpoint" json:"offload_endpoint"`
	OffloadAccessKey  string `yaml:"offload_access_key" json:"offload_access_key"`
	OffloadSecretKey  string `yaml:"offload_secret_key" json:"offload_secret_key"`
	OffloadBucket     string `yaml:"offload_bucket" json:"offload_bucket"`
	OffloadUseSSL     bool   `yaml:"offload_use_ssl" json:"offload_use_ssl"`
	OffloadRegion     string `yaml:"offload_region" json:"offload_region"`
	OffloadBasePrefix string `yaml:"offload_base_prefix" json:"offload_base_prefix"`

	OffloadMinLevel  int    `yaml:"offload_min_level" json:"offload_min_level"`
	OffloadKeepLocal bool   `yaml:"offload_keep_local" json:"offload_keep_local"`
	OffloadFSRoot    string `yaml:"offload_fs_root" json:"offload_fs_root"`
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	cfg := defaultConfigNoEnv()
	cfg.applyEnvOverrides()
	return cfg
}

func (c *Config) applyEnvOverrides() {
	if v, ok := os.LookupEnv("REDIGO_PORT"); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.Port = n
		}
	}
	if v, ok := os.LookupEnv("REDIGO_NETWORK_TYPE"); ok && v != "" {
		c.NetworkType = v
	}
	if v, ok := os.LookupEnv("REDIGO_PERSISTENCE_ENABLED"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			c.PersistenceEnabled = b
		}
	}
	if v, ok := os.LookupEnv("REDIGO_PERSISTENCE_TYPE"); ok && v != "" {
		c.PersistenceType = v
	}
	if v, ok := os.LookupEnv("REDIGO_DATA_DIR"); ok && v != "" {
		c.DataDir = v
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_ENABLED"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			c.OffloadEnabled = b
		}
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_BACKEND"); ok && v != "" {
		c.OffloadBackend = v
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_ENDPOINT"); ok && v != "" {
		c.OffloadEndpoint = v
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_ACCESS_KEY"); ok && v != "" {
		c.OffloadAccessKey = v
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_SECRET_KEY"); ok && v != "" {
		c.OffloadSecretKey = v
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_BUCKET"); ok && v != "" {
		c.OffloadBucket = v
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_USE_SSL"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			c.OffloadUseSSL = b
		}
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_REGION"); ok && v != "" {
		c.OffloadRegion = v
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_BASE_PREFIX"); ok {
		c.OffloadBasePrefix = v
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_MIN_LEVEL"); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			c.OffloadMinLevel = n
		}
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_KEEP_LOCAL"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			c.OffloadKeepLocal = b
		}
	}
	if v, ok := os.LookupEnv("REDIGO_OFFLOAD_FS_ROOT"); ok && v != "" {
		c.OffloadFSRoot = v
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
