package persistence

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

// LSMEnergy LSM 引擎
type LSMEnergy struct {
	options          *Options            // 配置选项
	mu               sync.RWMutex        // 并发控制
	
	// MemTable
	mutableMem       *MemTable           // 可变 MemTable
	immutableMem     *ImmutableMemTable  // 不可变 MemTable
	
	// WAL
	wal              *WALWriter          // WAL 写入器
	
	// SSTable
	sstables         []*SSTableReader    // SSTable 列表（Level 0）
	nextSSTableNum   uint64              // 下一个 SSTable 编号
	
	// Version Set 和 Compaction
	versionSet       *VersionSet         // 版本集合
	compactor        *Compactor          // Compaction 执行器
	
	// 序列号
	seqNum           uint64              // 当前序列号
	
	// 目录
	dbDir            string              // 数据库目录
	walDir           string              // WAL 目录
	sstableDir       string              // SSTable 目录
	
	closed           bool                // 是否已关闭
	
	// 后台刷写同步
	flushing       atomic.Bool         // 是否正在刷写
	flushDone      chan struct{}       // 刷写完成信号
}

// OpenLSMEnergy 打开 LSM 引擎
func OpenLSMEnergy(dbDir string, options *Options) (*LSMEnergy, error) {
	engine := &LSMEnergy{
		options:        options,
		mutableMem:     NewMemTable(3), // 默认 maxLevel=3
		nextSSTableNum: 0,
		dbDir:          dbDir,
		walDir:         filepath.Join(dbDir, "wal"),
		sstableDir:     filepath.Join(dbDir, "sstable"),
		closed:         false,
	}
	
	// 创建目录
	err := os.MkdirAll(dbDir, 0755)
	if err != nil {
		return nil, err
	}
	
	err = os.MkdirAll(engine.walDir, 0755)
	if err != nil {
		return nil, err
	}
	
	err = os.MkdirAll(engine.sstableDir, 0755)
	if err != nil {
		return nil, err
	}
	
	// 初始化刷写同步通道
	engine.flushDone = make(chan struct{}, 1)
	
	// 打开版本集合
	engine.versionSet, err = OpenVersionSet(dbDir, MaxLevels)
	if err != nil {
		return nil, fmt.Errorf("failed to open version set: %v", err)
	}
	
	// 从 VersionSet 恢复 nextSSTableNum
	engine.nextSSTableNum = engine.versionSet.nextFileNum
	logger.Info("Recovered nextSSTableNum: %d", engine.nextSSTableNum)
	
	// 创建 Compactor
	engine.compactor = NewCompactor(dbDir, engine.versionSet, options)
	
	// 启动后台 Compaction
	engine.compactor.Start()
	
	// 从 VersionSet 恢复 SSTable 信息（优先使用 VersionSet 的元数据）
	logger.Info("Recovering SSTables from VersionSet...")
	engine.sstables = make([]*SSTableReader, 0)
	
	version := engine.versionSet.currentVersion
	totalFiles := 0
	for level, files := range version.Files {
		if len(files) == 0 {
			continue
		}
		
		logger.Info("Recovering %d SSTables from Level %d", len(files), level)
		for _, fm := range files {
			// 构建 SSTable 文件路径
			sstablePath := filepath.Join(engine.sstableDir, fmt.Sprintf("%06d.sstable", fm.FileNum))
			
			// 检查文件是否存在
			if _, err := os.Stat(sstablePath); os.IsNotExist(err) {
				logger.Warn("SSTable file missing: %s (referenced in MANIFEST)", sstablePath)
				continue
			}
			
			// 打开 SSTable 文件
			reader, err := OpenSSTableForRead(sstablePath, options)
			if err != nil {
				logger.Warn("Failed to open SSTable %s: %v", sstablePath, err)
				continue
			}
			
			logger.Info("Recovered SSTable: %s (level=%d, size=%d bytes)", 
				filepath.Base(sstablePath), level, fm.Size)
			engine.sstables = append(engine.sstables, reader)
			totalFiles++
		}
	}
	
	if totalFiles > 0 {
		logger.Info("Loaded %d SSTables from VersionSet", totalFiles)
	} else {
		logger.Info("No SSTables found in VersionSet")
	}
	
	logger.Info("LSM Engine SSTable count: %d", len(engine.sstables))
	
	// 恢复 WAL
	err = engine.recoverFromWAL()
	if err != nil {
		return nil, fmt.Errorf("failed to recover from WAL: %v", err)
	}
	
	return engine, nil
}

// recoverFromWAL 从 WAL 恢复数据
func (e *LSMEnergy) recoverFromWAL() error {
	walFile := filepath.Join(e.walDir, "current.wal")
	
	// 检查 WAL 文件是否存在
	if !WALExists(walFile) {
		// 没有 WAL 文件，创建新的
		var err error
		e.wal, err = NewWALWriter(walFile, int64(64*1024*1024)) // 默认 64MB
		if err != nil {
			return err
		}
		return nil
	}
	
	// 重放 WAL
	lastSeq, err := ReplayWAL(walFile, func(record *WALRecord) error {
		switch record.Type {
		case WALRecordTypePut:
			e.mutableMem.Put(record.Key, record.Value)
		case WALRecordTypeDelete:
			e.mutableMem.Delete(record.Key)
		}
		return nil
	})
	
	if err != nil {
		return err
	}
	
	// 更新序列号
	atomic.StoreUint64(&e.seqNum, lastSeq)
	
	// 创建新的 WAL 写入器
	e.wal, err = NewWALWriter(walFile, int64(64*1024*1024))
	if err != nil {
		return err
	}
	
	// 检查是否需要刷写 MemTable
	if int64(e.mutableMem.Size()) >= int64(4*1024*1024) { // 默认 4MB
		err = e.flushMemTable()
		if err != nil {
			return fmt.Errorf("failed to flush memtable during recovery: %v", err)
		}
	}
	
	return nil
}

// Put 写入键值对
func (e *LSMEnergy) Put(key, value []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.closed {
		return fmt.Errorf("engine is closed")
	}
	
	// 1. 写入 WAL
	err := e.wal.Put(key, value)
	if err != nil {
		return fmt.Errorf("failed to write WAL: %v", err)
	}
	
	// 2. 写入 MemTable
	e.mutableMem.Put(key, value)
	
	// 3. 更新序列号
	seq := atomic.AddUint64(&e.seqNum, 1)
	_ = seq // 暂时不使用
	
	// 4. 检查是否需要刷写
	if int64(e.mutableMem.Size()) >= int64(4*1024*1024) { // 默认 4MB
		err = e.flushMemTable()
		if err != nil {
			return fmt.Errorf("failed to flush memtable: %v", err)
		}
	}
	
	return nil
}

// Get 获取值
func (e *LSMEnergy) Get(key []byte) ([]byte, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	if e.closed {
		return nil, false
	}
	
	// 1. 先在 Mutable MemTable 中查找
	val, found := e.mutableMem.Get(key)
	if found {
		return val, true
	}
	
	// 2. 在 Immutable MemTable 中查找
	if e.immutableMem != nil {
		val, found := e.immutableMem.Get(key)
		if found {
			return val, true
		}
	}
	
	// 3. 在 SSTable 中查找（从新到旧）
	for i := len(e.sstables) - 1; i >= 0; i-- {
		val, found := e.sstables[i].Get(key)
		if found {
			return val, true
		}
	}
	
	return nil, false
}

// Delete 删除键
func (e *LSMEnergy) Delete(key []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.closed {
		return fmt.Errorf("engine is closed")
	}
	
	// 1. 写入 WAL
	err := e.wal.Delete(key)
	if err != nil {
		return fmt.Errorf("failed to write WAL: %v", err)
	}
	
	// 2. 写入 MemTable（删除标记）
	e.mutableMem.Delete(key)
	
	// 3. 更新序列号
	seq := atomic.AddUint64(&e.seqNum, 1)
	_ = seq
	
	// 4. 检查是否需要刷写
	if int64(e.mutableMem.Size()) >= int64(4*1024*1024) { // 默认 4MB
		err = e.flushMemTable()
		if err != nil {
			return fmt.Errorf("failed to flush memtable: %v", err)
		}
	}
	
	return nil
}

// flushMemTableSync 同步刷写 MemTable 到 SSTable（用于关闭时）
func (e *LSMEnergy) flushMemTableSync() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.flushMemTableSyncNoLock()
}

// flushMemTableSyncNoLock 同步刷写 MemTable 到 SSTable（内部使用，假设已持有锁）
func (e *LSMEnergy) flushMemTableSyncNoLock() error {
	fmt.Printf("[FLUSH] Starting sync flush, MemTable size: %d bytes\n", e.mutableMem.Size())
	
	if e.mutableMem.Size() == 0 {
		fmt.Println("[FLUSH] MemTable is empty, skipping")
		return nil
	}
	
	// 1. 将当前 MemTable 转为 Immutable
	oldMem := e.mutableMem
	e.immutableMem = NewImmutableMemTable(oldMem)
	fmt.Printf("[FLUSH] Converted to Immutable MemTable, size: %d bytes\n", e.immutableMem.memtable.Size())
	
	// 2. 创建新的 Mutable MemTable
	e.mutableMem = NewMemTable(3)
	
	// 3. 同步刷写（不启动 goroutine）
	imm := e.immutableMem // 保存引用用于后续释放
	defer func() {
		if imm != nil {
			imm.Unref()
			fmt.Println("[FLUSH] Unref immutable memtable")
		}
	}()
	
	fmt.Println("[FLUSH] Calling flushImmutableToSSTable...")
	err := e.flushImmutableToSSTable(imm)
	if err != nil {
		fmt.Printf("[FLUSH] ERROR: Failed to flush immutable memtable: %v\n", err)
		return fmt.Errorf("failed to flush immutable memtable: %v", err)
	}
	
	fmt.Println("[FLUSH] Flush completed successfully")
	e.immutableMem = nil
	return nil
}

// flushMemTable 异步刷写 MemTable 到 SSTable
func (e *LSMEnergy) flushMemTable() error {
	if e.mutableMem.Size() == 0 {
		return nil
	}
	
	// 1. 将当前 MemTable 转为 Immutable
	oldMem := e.mutableMem
	e.immutableMem = NewImmutableMemTable(oldMem)
	
	// 2. 创建新的 Mutable MemTable
	e.mutableMem = NewMemTable(3) // 默认 maxLevel=3
	
	// 3. 异步刷写 Immutable MemTable 到 SSTable
	go func(imm *ImmutableMemTable) {
		defer func() {
			imm.Unref() // 减少引用计数
			// 发送刷写完成信号
			select {
			case e.flushDone <- struct{}{}:
				// 信号发送成功
			default:
				// 通道已满，不阻塞
			}
			e.flushing.Store(false)
		}()
		
		err := e.flushImmutableToSSTable(imm)
		if err != nil {
			fmt.Printf("Error flushing immutable memtable: %v\n", err)
		}
	}(e.immutableMem)
	
	e.immutableMem = nil
	e.flushing.Store(true)
	
	return nil
}

// flushImmutableToSSTable 将 Immutable MemTable 刷写到 SSTable
func (e *LSMEnergy) flushImmutableToSSTable(imm *ImmutableMemTable) error {
	fmt.Println("[SSTABLE] Starting flush to SSTable...")
	
	// 生成 SSTable 文件名
	sstableNum := e.versionSet.GetNextFileNum()
	filename := filepath.Join(e.sstableDir, fmt.Sprintf("%06d.sstable", sstableNum))
	fmt.Printf("[SSTABLE] Will create SSTable file: %s (num=%d)\n", filename, sstableNum)
	
	// 创建 SSTable Builder
	builder, err := NewSSTableBuilder(filename, e.options)
	if err != nil {
		return fmt.Errorf("failed to create sstable builder: %v", err)
	}
	defer builder.Abort() // 如果出错则回滚
	
	fmt.Println("[SSTABLE] Iterating immutable memtable...")
	entryCount := 0
	err = imm.ForEach(func(key, value []byte) error {
		entryCount++
		fmt.Printf("[SSTABLE] Adding entry #%d: key=%s, value_size=%d\n", entryCount, string(key), len(value))
		return builder.Add(key, value)
	})
	if err != nil {
		return fmt.Errorf("failed to iterate immutable memtable: %v", err)
	}
	
	fmt.Printf("[SSTABLE] Finished iteration, added %d entries\n", entryCount)
	
	// 完成 SSTable 构建
	fmt.Println("[SSTABLE] Finishing SSTable build...")
	err = builder.Finish()
	if err != nil {
		return fmt.Errorf("failed to finish sstable: %v", err)
	}
	fmt.Println("[SSTABLE] SSTable build completed")
	
	// 打开 SSTable Reader
	fmt.Println("[SSTABLE] Opening SSTable for read...")
	reader, err := OpenSSTableForRead(filename, e.options)
	if err != nil {
		return fmt.Errorf("failed to open sstable reader: %v", err)
	}
	fmt.Println("[SSTABLE] SSTable reader opened successfully")
	
	// 获取文件信息
	info, err := os.Stat(filename)
	if err != nil {
		reader.Close()
		return fmt.Errorf("failed to stat sstable file: %v", err)
	}
	fmt.Printf("[SSTABLE] SSTable file size: %d bytes\n", info.Size())
	
	// 创建文件元数据
	fm := &FileMetadata{
		FileNum:     sstableNum,
		Size:        info.Size(),
		SmallestKey: nil, // TODO: 从 builder 获取
		LargestKey:  nil, // TODO: 从 builder 获取
		Level:       0,   // Level 0
	}
	
	// 添加到版本集合
	fmt.Println("[SSTABLE] Adding file to version set...")
	err = e.versionSet.LogAddFile(fm)
	if err != nil {
		reader.Close()
		return fmt.Errorf("failed to log add file: %v", err)
	}
	fmt.Println("[SSTABLE] File added to version set successfully")
	
	// 添加到内存中的 SSTable 列表（假设已持有锁）
	e.sstables = append(e.sstables, reader)
	fmt.Printf("[SSTABLE] Added to in-memory SSTable list, total count: %d\n", len(e.sstables))
	
	return nil
}

// Close 关闭 LSM 引擎
func (e *LSMEnergy) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	if e.closed {
		return nil
	}
	
	e.closed = true
	
	// 创建关闭标记文件（用于调试）
	os.WriteFile("/tmp/lsm_close_called.txt", []byte("Close called at "+time.Now().String()), 0644)
	
	logger.Info("=== CLOSING LSM ENGINE ===")
	logger.Info("MemTable size before close: %d bytes", e.mutableMem.Size())
	
	// 1. 停止后台 Compaction
	if e.compactor != nil {
		e.compactor.Stop()
		logger.Info("Stopped compactor")
	}
	
	// 2. 同步刷写当前 MemTable（强制刷写，无论大小）
	// 注意：需要在持有锁的情况下调用，但 flushMemTableSync 不需要额外获取锁
	logger.Info("Forcing flush of MemTable with size %d bytes...", e.mutableMem.Size())
	err := e.flushMemTableSyncNoLock()
	if err != nil {
		logger.Error("Error flushing memtable: %v", err)
		return err
	}
	logger.Info("MemTable flushed successfully (forced)")
	
	// 3. 关闭 WAL
	if e.wal != nil {
		err := e.wal.Close()
		if err != nil {
			return err
		}
		logger.Info("WAL closed")
	}
	
	// 4. 关闭所有 SSTable
	for _, sstable := range e.sstables {
		err := sstable.Close()
		if err != nil {
			return err
		}
	}
	logger.Info("Closed %d SSTables", len(e.sstables))
	
	// 5. 关闭版本集合
	if e.versionSet != nil {
		err := e.versionSet.Close()
		if err != nil {
			return err
		}
		logger.Info("VersionSet closed")
	}
	
	fmt.Println("LSM Engine closed successfully")
	return nil
}

// GetSeqNum 获取当前序列号
func (e *LSMEnergy) GetSeqNum() uint64 {
	return atomic.LoadUint64(&e.seqNum)
}

// GetSSTableCount 获取 SSTable 数量
func (e *LSMEnergy) GetSSTableCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.sstables)
}

// LoadAllKeys 加载所有 key-value 到内存（用于冷启动全量加载）
// 返回一个 map[string][]byte，包含所有未删除的键值对
func (e *LSMEnergy) LoadAllKeys() (map[string][]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	result := make(map[string][]byte)
	
	fmt.Printf("[LoadAllKeys] Starting to load keys...\n")
	fmt.Printf("[LoadAllKeys] MemTable size: %d bytes, entries: %d\n", e.mutableMem.Size(), e.mutableMem.EntryCount())
	
	// 1. 从 MemTable 加载
	if e.mutableMem != nil {
		it := e.mutableMem.Iterator()
		memCount := 0
		for it.SeekToFirst(); it.Valid(); it.Next() {
			key := it.Key()
			value := it.Value()
			fmt.Printf("[LoadAllKeys] MemTable entry: key=%s, value_size=%d\n", string(key), len(value))
			if len(value) > 0 && value[0] != 0x00 { // 检查删除标记
				keyCopy := make([]byte, len(key))
				copy(keyCopy, key)
				valueCopy := make([]byte, len(value))
				copy(valueCopy, value)
				result[string(keyCopy)] = valueCopy
				memCount++
			}
		}
		fmt.Printf("[LoadAllKeys] Loaded %d keys from MemTable\n", memCount)
	}
	
	// 2. 从 Immutable MemTable 加载
	if e.immutableMem != nil && e.immutableMem.memtable != nil {
		it := e.immutableMem.memtable.Iterator()
		immCount := 0
		for it.SeekToFirst(); it.Valid(); it.Next() {
			key := it.Key()
			value := it.Value()
			if len(value) > 0 && value[0] != 0x00 { // 检查删除标记
				keyStr := string(key)
				// 如果已存在，跳过
				if _, exists := result[keyStr]; !exists {
					keyCopy := make([]byte, len(key))
					copy(keyCopy, key)
					valueCopy := make([]byte, len(value))
					copy(valueCopy, value)
					result[keyStr] = valueCopy
					immCount++
				}
			}
		}
		fmt.Printf("[LoadAllKeys] Loaded %d keys from Immutable MemTable\n", immCount)
	}
	
	// 3. 从所有 SSTable 加载（从新到旧，确保最新版本优先）
	fmt.Printf("[LoadAllKeys] Starting to scan %d SSTables\n", len(e.sstables))
	for i := len(e.sstables) - 1; i >= 0; i-- {
		sstable := e.sstables[i]
		fmt.Printf("[LoadAllKeys] Scanning SSTable #%d (index=%d)\n", i, i)
		it := sstable.NewIterator()
		sstableCount := 0
		iterCount := 0
		for it.SeekToFirst(); it.Valid(); it.Next() {
			iterCount++
			key := it.Key()
			value := it.Value()
			fmt.Printf("[LoadAllKeys] SSTable entry: key=%s, value_size=%d\n", string(key), len(value))
			if len(value) > 0 && value[0] != 0x00 { // 检查删除标记
				keyStr := string(key)
				// 如果已存在，跳过
				if _, exists := result[keyStr]; !exists {
					keyCopy := make([]byte, len(key))
					copy(keyCopy, key)
					valueCopy := make([]byte, len(value))
					copy(valueCopy, value)
					result[keyStr] = valueCopy
					sstableCount++
				}
			}
		}
		it.Release()
		fmt.Printf("[LoadAllKeys] SSTable #%d: iterated %d entries, loaded %d keys\n", i, iterCount, sstableCount)
	}
	
	fmt.Printf("[LoadAllKeys] Loaded total %d keys\n", len(result))
	return result, nil
}
