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
	
	// ========== WAL 配置 ==========
	
	// WriteAheadLog 是否启用 WAL
	WriteAheadLog bool
	
	// SyncWAL 每次写入后是否同步到磁盘（更安全但更慢）
	SyncWAL bool
	
	// ========== Compaction 配置 ==========
	
	// L0_CompactionTrigger Level 0 文件数达到多少时触发 compaction
	L0_CompactionTrigger int
	
	// L0_SlowdownWritesTrigger Level 0 文件数达到多少时减缓写入
	L0_SlowdownWritesTrigger int
	
	// L0_StopWritesTrigger Level 0 文件数达到多少时停止写入
	L0_StopWritesTrigger int
	
	// MaxLevels 最大层级数
	MaxLevels int
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
		CacheSize: 8 * 1024 * 1024,
		UseCache:  true,
		
		// WAL 配置
		WriteAheadLog: true,
		SyncWAL:       false, // 性能优先
		
		// Compaction 配置
		L0_CompactionTrigger:       4,
		L0_SlowdownWritesTrigger:   8,
		L0_StopWritesTrigger:       12,
		MaxLevels:                  7,
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
	
	if o.BloomFPRate <= 0 || o.BloomFPRate > 0.1 {
		o.BloomFPRate = 0.01
	}
	
	if o.MaxLevels < 3 {
		o.MaxLevels = 7
	}
	
	return nil
}
