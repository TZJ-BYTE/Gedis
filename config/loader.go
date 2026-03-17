package config

import (
	"fmt"
	"os"

	"go.yaml.in/yaml/v3"
)

func defaultConfigNoEnv() *Config {
	return &Config{
		Host:            "0.0.0.0",
		Port:            16379,
		NetworkType:     "std",
		DBCount:         16,
		MaxMemory:       256 * 1024 * 1024,
		MaxMemoryPolicy: "noeviction",

		PersistenceEnabled: true,
		PersistenceType:    "lsm",
		DataDir:            "./data",
		ColdStartStrategy:  "load_all",

		AOFEnabled: false,
		AOFPath:    "./data/appendonly.aof",
		RDBPath:    "./data/dump.rdb",

		BlockSize:       4096,
		MemTableSize:    4 << 20,
		WriteBufferSize: 64 << 20,
		MaxOpenFiles:    1000,
		BloomFilterBits: 10,

		LogLevel: "info",
		LogPath:  "./logs/redigo.log",

		OffloadEnabled:    false,
		OffloadBackend:    "fs",
		OffloadEndpoint:   "127.0.0.1:9000",
		OffloadAccessKey:  "minioadmin",
		OffloadSecretKey:  "minioadmin",
		OffloadBucket:     "redigo-data",
		OffloadUseSSL:     false,
		OffloadRegion:     "us-east-1",
		OffloadBasePrefix: "",
		OffloadMinLevel:   2,
		OffloadKeepLocal:  true,
		OffloadFSRoot:     "./offload",
	}
}

func LoadFromFile(path string) (*Config, error) {
	cfg := defaultConfigNoEnv()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}
	cfg.applyEnvOverrides()
	return cfg, nil
}

