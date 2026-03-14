// Package persistence 实现了基于 LSM Tree 的持久化存储引擎
//
// 该包提供了 LevelDB/RocksDB 风格的存储实现，包括以下核心组件：
//
// # 架构组件
//
// - MemTable: 基于跳表的内存有序数据结构，处理所有写入操作
// - Immutable MemTable: 只读 MemTable，用于异步刷写到磁盘
// - SSTable: 磁盘上的有序字符串表格，支持高效的点查和范围查询
// - Bloom Filter: 快速判断 key 是否存在，减少不必要的磁盘 I/O
// - Block Cache: LRU 缓存机制，加速热点数据访问
// - WAL: Write-Ahead Logging，保证崩溃后的数据恢复
// - Compaction: 后台合并 SSTable，优化读取性能和空间利用率
//
// # 使用示例
//
//	opts := persistence.DefaultOptions()
//	opts.DataDir = "./data"
//	engine, err := persistence.NewLSMEnergy(opts)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer engine.Close()
//
//	// 写入数据
//	err = engine.Put("key1", []byte("value1"))
//
//	// 读取数据
//	value, exists := engine.Get("key1")
//
//	// 删除数据
//	err = engine.Delete("key1")
//
// # 性能特性
//
// 写入性能：
//   - 顺序写入（WAL disabled）: > 500K ops/s
//   - 随机写入（WAL enabled）: > 200K ops/s
//
// 读取性能：
//   - 缓存命中：< 0.5ms
//   - Bloom Filter 命中：< 2ms
//   - 磁盘读取：< 10ms
//
// 空间效率：
//   - 压缩率：> 50% (Snappy)
//   - Bloom Filter 开销：< 10MB/GB
//
// # 线程安全
//
// 所有公共方法都是并发安全的，支持多线程同时读写。
//
// # 错误处理
//
// 所有错误都会直接返回给调用者，包括：
//   - IO 错误
//   - 编码/解码错误
//   - 资源不足错误
//
// # 配置选项
//
// 通过 Options 结构体可以配置：
//   - MemTable 大小
//   - SSTable 文件大小
//   - Block 大小
//   - 压缩算法
//   - Bloom Filter 参数
//   - Cache 大小
//   - WAL 行为
//   - Compaction 策略
//
// 更多详细信息请参考 README_IMPLEMENTATION.md
package persistence
