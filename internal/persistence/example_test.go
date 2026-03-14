package persistence_test

import (
	"fmt"
	"log"
	"os"
	
	"github.com/TZJ-BYTE/RediGo/internal/persistence"
)

// ExampleLSMEnergy_basic 基本使用示例
func ExampleLSMEnergy_basic() {
	// 配置选项
	opts := persistence.DefaultOptions()
	opts.DataDir = "./test_data"
	opts.WriteAheadLog = true
	
	// 创建临时目录
	os.MkdirAll(opts.DataDir, 0755)
	defer os.RemoveAll(opts.DataDir)
	
	// TODO: 创建 LSM Engine
	// engine, err := persistence.NewLSMEnergy(opts)
	// if err != nil {
	//     log.Fatal(err)
	// }
	// defer engine.Close()
	
	// 写入数据
	// err := engine.Put("key1", []byte("value1"))
	// if err != nil {
	//     log.Fatal(err)
	// }
	
	// 读取数据
	// value, exists := engine.Get("key1")
	// if exists {
	//     fmt.Printf("Got value: %s\n", string(value))
	// }
	
	fmt.Println("Example will be available after Phase 6 implementation")
	
	// Output: Example will be available after Phase 6 implementation
}

// ExampleLSMEnergy_batch 批量操作示例
func ExampleLSMEnergy_batch() {
	opts := persistence.DefaultOptions()
	opts.DataDir = "./test_data_batch"
	
	os.MkdirAll(opts.DataDir, 0755)
	defer os.RemoveAll(opts.DataDir)
	
	// TODO: 批量写入示例
	// engine, _ := persistence.NewLSMEnergy(opts)
	// defer engine.Close()
	
	// batch := make([]persistence.Operation, 100)
	// for i := 0; i < 100; i++ {
	//     batch[i] = persistence.Operation{
	//         Type: persistence.OpPut,
	//         Key:  fmt.Sprintf("key_%d", i),
	//         Value: []byte(fmt.Sprintf("value_%d", i)),
	//     }
	// }
	// err := engine.WriteBatch(batch)
	
	fmt.Println("Batch example will be available after Phase 6 implementation")
	
	// Output: Batch example will be available after Phase 6 implementation
}

// ExampleLSMEnergy_iterator 迭代器使用示例
func ExampleLSMEnergy_iterator() {
	opts := persistence.DefaultOptions()
	opts.DataDir = "./test_data_iter"
	
	os.MkdirAll(opts.DataDir, 0755)
	defer os.RemoveAll(opts.DataDir)
	
	// TODO: 迭代器示例
	// engine, _ := persistence.NewLSMEnergy(opts)
	// defer engine.Close()
	
	// iter := engine.NewIterator()
	// defer iter.Release()
	
	// for iter.Next() {
	//     fmt.Printf("Key: %s, Value: %s\n", iter.Key(), string(iter.Value()))
	// }
	
	fmt.Println("Iterator example will be available after Phase 8 implementation")
	
	// Output: Iterator example will be available after Phase 8 implementation
}

// ExampleOptions_custom 自定义配置示例
func ExampleOptions_custom() {
	// 自定义配置
	opts := &persistence.Options{
		DataDir: "./custom_data",
		
		// MemTable 配置：8MB
		MemTableSize: 8 * 1024 * 1024,
		
		// SSTable 配置：4MB 文件，8KB Block
		MaxFileSize: 4 * 1024 * 1024,
		BlockSize:   8 * 1024,
		
		// Bloom Filter：0.1% 假阳性率
		UseBloomFilter: true,
		BloomFPRate:    0.001,
		
		// Cache: 16MB
		CacheSize: 16 * 1024 * 1024,
		UseCache:  true,
		
		// WAL: 每次写入都同步
		WriteAheadLog: true,
		SyncWAL:       true,
		
		// Compaction: 更激进的策略
		L0_CompactionTrigger:       2,
		L0_SlowdownWritesTrigger:   4,
		L0_StopWritesTrigger:       8,
		MaxLevels:                  5,
	}
	
	// 验证配置
	if err := opts.Validate(); err != nil {
		log.Fatal(err)
	}
	
	fmt.Printf("Custom options configured for high-performance workload\n")
	fmt.Printf("Data directory: %s\n", opts.DataDir)
	fmt.Printf("MemTable size: %d MB\n", opts.MemTableSize/(1024*1024))
	fmt.Printf("Cache size: %d MB\n", opts.CacheSize/(1024*1024))
	
	// Output: Custom options configured for high-performance workload
	// Data directory: ./custom_data
	// MemTable size: 8 MB
	// Cache size: 16 MB
}

// ExampleOptions_default 默认配置示例
func ExampleOptions_default() {
	opts := persistence.DefaultOptions()
	
	fmt.Printf("Default configuration:\n")
	fmt.Printf("- DataDir: %s\n", opts.DataDir)
	fmt.Printf("- MemTableSize: %d MB\n", opts.MemTableSize/(1024*1024))
	fmt.Printf("- MaxFileSize: %d MB\n", opts.MaxFileSize/(1024*1024))
	fmt.Printf("- UseBloomFilter: %v\n", opts.UseBloomFilter)
	fmt.Printf("- BloomFPRate: %.2f%%\n", opts.BloomFPRate*100)
	fmt.Printf("- CacheSize: %d MB\n", opts.CacheSize/(1024*1024))
	fmt.Printf("- WriteAheadLog: %v\n", opts.WriteAheadLog)
	fmt.Printf("- MaxLevels: %d\n", opts.MaxLevels)
	
	// Output: 
	// Default configuration:
	// - DataDir: ./data
	// - MemTableSize: 4 MB
	// - MaxFileSize: 2 MB
	// - UseBloomFilter: true
	// - BloomFPRate: 1.00%
	// - CacheSize: 8 MB
	// - WriteAheadLog: true
	// - MaxLevels: 7
}
