package persistence

// CompressionType 压缩类型
type CompressionType int

const (
	// NoCompression 不压缩
	NoCompression CompressionType = 0
	// SnappyCompression Snappy 压缩（快速）
	SnappyCompression CompressionType = 1
	// ZlibCompression Zlib 压缩（高压缩比）
	ZlibCompression CompressionType = 2
)

// Options LSM 存储引擎配置
type Options struct {
	// ========== 基础配置 ==========

	// DataDir 数据目录路径
	DataDir string

	// ========== MemTable 配置 ==========

	// MemTableSize MemTable 大小限制（字节），默认 4MB
	MemTableSize int

	// ========== SSTable 配置 ==========

	// MaxFileSize SSTable 最大文件大小（字节），默认 2MB
	MaxFileSize int

	// BlockSize Data Block 大小（字节），默认 4KB
	BlockSize int

	// Compression 压缩类型
	Compression CompressionType

	// ========== Bloom Filter 配置 ==========

	// UseBloomFilter 是否使用 Bloom Filter
	UseBloomFilter bool

	// BloomFPRate Bloom Filter 假阳性率，默认 0.01 (1%)
	BloomFPRate float64

	// ========== Cache 配置 ==========

	// CacheSize Block Cache 大小（字节），默认 8MB
	CacheSize int

	// UseCache 是否使用 Block Cache
	UseCache bool

	// MaxOpenFiles 最大打开文件数（SSTable Cache 容量），默认 500
	MaxOpenFiles int

	// ========== WAL 配置 ==========

	// WriteAheadLog 是否启用 WAL
	WriteAheadLog bool

	// SyncWAL 每次写入后是否同步到磁盘（更安全但更慢）
	SyncWAL bool

	WALQueueSize      int
	WALWriteTimeoutMs int

	// ========== WiscKey (Value Log) 配置 ==========

	// ValueThreshold 写入 Value Log 的阈值（字节）
	// 小于此值的 Value 将直接存储在 LSM Tree 中，大于等于此值则存入 vLog
	// 默认 64 字节。设置为 0 则所有数据都写入 vLog。设置为 -1 则所有数据都存 LSM Tree（关闭 KV 分离）。
	ValueThreshold int

	EnableHotColdTiering          bool
	HotColdMinFileReads           uint64
	HotColdDecayIntervalSeconds   int
	HotColdDecayFactor            float64
	HotColdMaxLevelSizeOverFactor float64

	EnableOffloading bool
	OffloadBackend   string
	OffloadMinLevel  int
	OffloadKeepLocal bool

	OffloadFSRoot string

	OffloadEndpoint  string
	OffloadAccessKey string
	OffloadSecretKey string
	OffloadBucket    string
	OffloadRegion    string
	OffloadPrefix    string
	OffloadUseSSL    bool

	// ========== Compaction 配置 ==========

	// L0_CompactionTrigger Level 0 文件数达到多少时触发 compaction
	L0_CompactionTrigger int

	// L0_SlowdownWritesTrigger Level 0 文件数达到多少时减缓写入
	L0_SlowdownWritesTrigger int

	// L0_StopWritesTrigger Level 0 文件数达到多少时停止写入
	L0_StopWritesTrigger int

	// MaxLevels 最大层级数
	MaxLevels int

	CompactionMaxRunMs      int
	CompactionCooldownMs    int
	CompactionMaxInputBytes int64

	ValueLogGCMaxRunMs        int
	ValueLogGCMaxScanBytes    int64
	ValueLogGCMaxRewriteBytes int64

	BackgroundBusyWriteOpsPerSec  int
	BackgroundLowCacheHitPermille int
}

// DefaultOptions 返回默认配置
func DefaultOptions() *Options {
	return &Options{
		// 基础配置
		DataDir: "./data",

		// MemTable 配置：4MB
		MemTableSize: 4 * 1024 * 1024,

		// SSTable 配置
		MaxFileSize: 2 * 1024 * 1024, // 2MB
		BlockSize:   4 * 1024,        // 4KB
		Compression: SnappyCompression,

		// Bloom Filter 配置
		UseBloomFilter: true,
		BloomFPRate:    0.01, // 1%

		// Cache 配置：8MB
		CacheSize:    8 * 1024 * 1024,
		UseCache:     true,
		MaxOpenFiles: 500,

		// WAL 配置
		WriteAheadLog:     true,
		SyncWAL:           false, // 性能优先
		WALQueueSize:      4096,
		WALWriteTimeoutMs: 200,

		// WiscKey 配置
		ValueThreshold: 64,

		EnableHotColdTiering:          false,
		HotColdMinFileReads:           1024,
		HotColdDecayIntervalSeconds:   30,
		HotColdDecayFactor:            0.5,
		HotColdMaxLevelSizeOverFactor: 1.5,

		EnableOffloading: false,
		OffloadBackend:   "fs",
		OffloadMinLevel:  2,
		OffloadKeepLocal: true,
		OffloadFSRoot:    "./offload",
		OffloadRegion:    "us-east-1",
		OffloadPrefix:    "",
		OffloadUseSSL:    false,

		// Compaction 配置
		L0_CompactionTrigger:     4,
		L0_SlowdownWritesTrigger: 8,
		L0_StopWritesTrigger:     12,
		MaxLevels:                7,

		CompactionMaxRunMs:      200,
		CompactionCooldownMs:    200,
		CompactionMaxInputBytes: 0,

		ValueLogGCMaxRunMs:        50,
		ValueLogGCMaxScanBytes:    16 << 20,
		ValueLogGCMaxRewriteBytes: 16 << 20,

		BackgroundBusyWriteOpsPerSec:  20000,
		BackgroundLowCacheHitPermille: 200,
	}
}

// Validate 验证配置合法性
func (o *Options) Validate() error {
	if o.MemTableSize < 1024*1024 { // 最小 1MB
		o.MemTableSize = 1024 * 1024
	}

	if o.BlockSize < 1024 { // 最小 1KB
		o.BlockSize = 1024
	}
	if o.WALQueueSize <= 0 {
		o.WALQueueSize = 4096
	}
	if o.WALWriteTimeoutMs < 0 {
		o.WALWriteTimeoutMs = 0
	}

	if o.BloomFPRate <= 0 || o.BloomFPRate > 0.1 {
		o.BloomFPRate = 0.01
	}

	if o.MaxLevels < 3 {
		o.MaxLevels = 7
	}

	if o.HotColdDecayIntervalSeconds <= 0 {
		o.HotColdDecayIntervalSeconds = 30
	}

	if o.HotColdDecayFactor <= 0 || o.HotColdDecayFactor >= 1 {
		o.HotColdDecayFactor = 0.5
	}

	if o.HotColdMaxLevelSizeOverFactor <= 1 {
		o.HotColdMaxLevelSizeOverFactor = 1.5
	}

	if o.OffloadBackend == "" {
		o.OffloadBackend = "fs"
	}

	if o.OffloadMinLevel < 0 {
		o.OffloadMinLevel = 0
	}

	if o.OffloadRegion == "" {
		o.OffloadRegion = "us-east-1"
	}

	if o.MaxOpenFiles < 10 {
		o.MaxOpenFiles = 10
	}

	if o.CompactionMaxRunMs <= 0 {
		o.CompactionMaxRunMs = 200
	}
	if o.CompactionCooldownMs < 0 {
		o.CompactionCooldownMs = 0
	}
	if o.CompactionMaxInputBytes < 0 {
		o.CompactionMaxInputBytes = 0
	}
	if o.ValueLogGCMaxRunMs <= 0 {
		o.ValueLogGCMaxRunMs = 50
	}
	if o.ValueLogGCMaxScanBytes <= 0 {
		o.ValueLogGCMaxScanBytes = 16 << 20
	}
	if o.ValueLogGCMaxRewriteBytes <= 0 {
		o.ValueLogGCMaxRewriteBytes = 16 << 20
	}
	if o.BackgroundBusyWriteOpsPerSec <= 0 {
		o.BackgroundBusyWriteOpsPerSec = 20000
	}
	if o.BackgroundLowCacheHitPermille < 0 {
		o.BackgroundLowCacheHitPermille = 0
	}
	if o.BackgroundLowCacheHitPermille > 1000 {
		o.BackgroundLowCacheHitPermille = 1000
	}

	return nil
}
