package persistence

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TZJ-BYTE/RediGo/internal/persistence/vlog"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

var errLegacyLSMDisabled = fmt.Errorf("legacy LSM engine is disabled; use engine_v2 instead")

// LSMEnergy LSM 引擎
type LSMEnergy struct {
	options *Options     // 配置选项
	mu      sync.RWMutex // 并发控制

	// Value Log (WiscKey)
	vLogWriter *vlog.ValueLogWriter // Value Log 写入器
	vLogReader *vlog.ValueLogReader // Value Log 读取器
	vLogGC     *vlog.ValueLogGC     // Value Log GC

	// MemTable
	mutableMem   *MemTable          // 可变 MemTable
	immutableMem *ImmutableMemTable // 不可变 MemTable

	// WAL
	wal *WALWriter // WAL 写入器

	// SSTable
	tableCache *TableCache
	blockCache *BlockCache

	nextSSTableNum uint64 // 下一个 SSTable 编号

	// Version Set 和 Compaction
	versionSet *VersionSet // 版本集合
	compactor  *Compactor  // Compaction 执行器

	// 序列号
	seqNum uint64 // 当前序列号

	// 目录
	dbDir      string // 数据库目录
	walDir     string // WAL 目录
	sstableDir string // SSTable 目录
	vlogDir    string // Value Log 目录

	closed    atomic.Bool
	closeOnce sync.Once
	bgWG      sync.WaitGroup
	flushWG   sync.WaitGroup

	// 后台刷写同步
	flushing  atomic.Bool   // 是否正在刷写
	flushDone chan struct{} // 刷写完成信号

	gcStop chan struct{}
	bgStop chan struct{}

	heat      *HeatTracker
	offloader *SSTableOffloader

	writesThisSec    atomic.Uint64
	writeOpsPerSec   atomic.Uint64
	cacheHitPermille atomic.Uint32

	flushCount         atomic.Uint64
	flushBytesWritten  atomic.Uint64
	flushDurationNanos atomic.Uint64
	flushErrors        atomic.Uint64
}

func (e *LSMEnergy) Reset() error {
	if e == nil {
		return fmt.Errorf("nil engine")
	}
	if e.options == nil {
		e.options = DefaultOptions()
	}

	if e.closed.Load() {
		return fmt.Errorf("engine is closed")
	}

	e.mu.Lock()
	e.closed.Store(true)
	e.mu.Unlock()

	safeClose := func(ch chan struct{}) {
		defer func() { _ = recover() }()
		close(ch)
	}

	safeClose(e.gcStop)
	safeClose(e.bgStop)

	if e.compactor != nil {
		e.compactor.Stop()
		e.compactor = nil
	}

	e.bgWG.Wait()
	e.flushWG.Wait()

	e.mu.Lock()
	unlocked := false
	defer func() {
		if !unlocked {
			e.mu.Unlock()
		}
	}()

	if e.offloader != nil && e.versionSet != nil {
		v := e.versionSet.GetCurrentVersion()
		if v != nil {
			for level := 0; level < len(v.Files); level++ {
				for _, fm := range v.Files[level] {
					if fm != nil {
						_ = e.offloader.DeleteRemote(fm.FileNum)
					}
				}
			}
		}
	}

	if e.wal != nil {
		_ = e.wal.Close()
		e.wal = nil
	}
	if e.tableCache != nil {
		_ = e.tableCache.Close()
	}
	if e.versionSet != nil {
		_ = e.versionSet.Close()
		e.versionSet = nil
	}
	if e.vLogReader != nil {
		_ = e.vLogReader.Close()
		e.vLogReader = nil
	}
	if e.vLogWriter != nil {
		_ = e.vLogWriter.Close()
		e.vLogWriter = nil
	}

	versionDir := filepath.Join(e.dbDir, "version")
	dirs := []string{e.walDir, e.sstableDir, e.vlogDir, versionDir}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
	}

	_ = e.options.Validate()
	e.mutableMem = NewMemTable(e.options.MemTableSize)
	e.immutableMem = nil
	atomic.StoreUint64(&e.seqNum, 0)
	e.nextSSTableNum = 0

	e.flushDone = make(chan struct{}, 1)

	var err error
	e.vLogWriter, err = vlog.NewValueLogWriter(e.vlogDir, 64*1024*1024)
	if err != nil {
		return fmt.Errorf("failed to open value log writer: %v", err)
	}
	e.vLogReader = vlog.NewValueLogReader(e.vlogDir)

	checkKeyFunc := func(key []byte, vp *vlog.ValuePointer) (bool, error) {
		currentValPtrBytes, found := e.getRaw(key)
		if !found {
			return false, nil
		}
		if IsDeleted(currentValPtrBytes) {
			return false, nil
		}
		currentVP := vlog.DecodeValuePointer(currentValPtrBytes)
		if currentVP == nil {
			return false, nil
		}
		if currentVP.Fid == vp.Fid && currentVP.Offset == vp.Offset {
			return true, nil
		}
		return false, nil
	}

	rewriteFunc := func(key, value []byte, oldVP *vlog.ValuePointer) error {
		newVP, err := e.vLogWriter.Write(key, value)
		if err != nil {
			return err
		}

		e.mu.Lock()
		defer e.mu.Unlock()

		if e.closed.Load() {
			return fmt.Errorf("engine closed")
		}

		currentValPtrBytes, found := e.getRawNoLock(key)
		if !found || IsDeleted(currentValPtrBytes) {
			return nil
		}
		currentVP := vlog.DecodeValuePointer(currentValPtrBytes)
		if currentVP == nil {
			return nil
		}

		if oldVP == nil {
			return nil
		}
		if currentVP.Fid != oldVP.Fid || currentVP.Offset != oldVP.Offset || currentVP.Len != oldVP.Len {
			return nil
		}

		e.mutableMem.Put(key, newVP.Encode())
		return nil
	}

	e.vLogGC = vlog.NewValueLogGC(e.vlogDir, 0.5, checkKeyFunc, rewriteFunc)

	if e.tableCache == nil {
		e.tableCache = NewTableCache(e.options.MaxOpenFiles)
	} else {
		e.tableCache = NewTableCache(e.options.MaxOpenFiles)
	}
	if e.options.UseCache {
		if e.blockCache == nil {
			e.blockCache = NewBlockCache(int64(e.options.CacheSize))
		} else {
			e.blockCache.Resize(int64(e.options.CacheSize))
			e.blockCache.Clear()
		}
	} else {
		e.blockCache = nil
	}

	e.versionSet, err = OpenVersionSet(e.dbDir, MaxLevels)
	if err != nil {
		return fmt.Errorf("failed to open version set: %v", err)
	}
	e.nextSSTableNum = e.versionSet.nextFileNum

	e.heat = NewHeatTracker(time.Duration(e.options.HotColdDecayIntervalSeconds)*time.Second, e.options.HotColdDecayFactor)
	e.offloader, err = NewSSTableOffloader(e.options, e.sstableDir)
	if err != nil {
		return fmt.Errorf("failed to init offloader: %v", err)
	}

	e.compactor = NewCompactor(e.dbDir, e.versionSet, e.options, e.heat.Get, func(fm *FileMetadata) error {
		if e.offloader == nil {
			return nil
		}
		if !e.allowOffload() {
			return nil
		}
		return e.offloader.OffloadIfNeeded(fm)
	})
	e.compactor.SetThrottle(func(level int) (bool, time.Duration) {
		return e.allowCompaction(level)
	})

	e.gcStop = make(chan struct{})
	e.bgStop = make(chan struct{})
	e.bgWG = sync.WaitGroup{}

	err = e.recoverFromWAL()
	if err != nil {
		return fmt.Errorf("failed to recover from WAL: %v", err)
	}

	e.closed.Store(false)
	unlocked = true
	e.mu.Unlock()

	if e.compactor != nil {
		e.compactor.Start()
	}

	e.bgWG.Add(2)
	go func() {
		defer e.bgWG.Done()
		e.runGC()
	}()
	go func() {
		defer e.bgWG.Done()
		e.runBackgroundScheduler()
	}()
	return nil
}

// OpenLSMEnergy 打开 LSM 引擎
func OpenLSMEnergy(dbDir string, options *Options) (*LSMEnergy, error) {
        return nil, errLegacyLSMDisabled

	if options == nil {
		options = DefaultOptions()
	}
	_ = options.Validate()

	engine := &LSMEnergy{
		options:        options,
		mutableMem:     NewMemTable(options.MemTableSize),
		nextSSTableNum: 0,
		dbDir:          dbDir,
		walDir:         filepath.Join(dbDir, "wal"),
		sstableDir:     filepath.Join(dbDir, "sstable"),
		vlogDir:        filepath.Join(dbDir, "vlog"),
		tableCache:     NewTableCache(options.MaxOpenFiles),
		gcStop:         make(chan struct{}),
		bgStop:         make(chan struct{}),
	}
	if options.UseCache {
		engine.blockCache = NewBlockCache(int64(options.CacheSize))
	}

	cleanup := func() {
		engine.closed.Store(true)
		engine.closeOnce.Do(func() {
			close(engine.gcStop)
			close(engine.bgStop)
		})
		if engine.compactor != nil {
			engine.compactor.Stop()
			engine.compactor = nil
		}
		if engine.wal != nil {
			_ = engine.wal.Close()
			engine.wal = nil
		}
		if engine.tableCache != nil {
			_ = engine.tableCache.Close()
		}
		if engine.versionSet != nil {
			_ = engine.versionSet.Close()
			engine.versionSet = nil
		}
		if engine.vLogReader != nil {
			_ = engine.vLogReader.Close()
			engine.vLogReader = nil
		}
		if engine.vLogWriter != nil {
			_ = engine.vLogWriter.Close()
			engine.vLogWriter = nil
		}
	}

	// 创建目录
	err := os.MkdirAll(dbDir, 0750)
	if err != nil {
		cleanup()
		return nil, err
	}

	err = os.MkdirAll(engine.walDir, 0750)
	if err != nil {
		cleanup()
		return nil, err
	}

	err = os.MkdirAll(engine.sstableDir, 0750)
	if err != nil {
		cleanup()
		return nil, err
	}

	err = os.MkdirAll(engine.vlogDir, 0750)
	if err != nil {
		cleanup()
		return nil, err
	}

	// 初始化 Value Log 读写器
	engine.vLogWriter, err = vlog.NewValueLogWriter(engine.vlogDir, 64*1024*1024) // 默认 64MB
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open value log writer: %v", err)
	}
	engine.vLogReader = vlog.NewValueLogReader(engine.vlogDir)

	// 初始化 GC
	// 1. 检查 Key 有效性的回调
	checkKeyFunc := func(key []byte, vp *vlog.ValuePointer) (bool, error) {
		// 查询 LSM Tree 获取最新的 VP
		// 注意：这里需要获取最新版本，所以直接查 MemTable + SSTable
		// 但是 Get 方法返回的是 Value，不是 VP。我们需要修改 Get 逻辑或者新增 GetVP 方法
		// 为了复用现有逻辑，我们可以在内部实现一个 getVP

		// 暂时我们假设 Get 方法返回的就是 VP 编码后的值（对于 KV 分离模式）
		// 但 Get 方法会自动去 vLog 查 Value。
		// 所以我们需要一个 rawGet 方法，只查 LSM Tree，不查 vLog

		currentValPtrBytes, found := engine.getRaw(key)
		if !found {
			return false, nil // Key 已被删除
		}

		if IsDeleted(currentValPtrBytes) {
			return false, nil // Key 是 Tombstone
		}

		currentVP := vlog.DecodeValuePointer(currentValPtrBytes)
		if currentVP == nil {
			return false, nil // 不是 VP，可能是旧数据
		}

		// 比较 VP 是否一致
		if currentVP.Fid == vp.Fid && currentVP.Offset == vp.Offset {
			return true, nil
		}

		return false, nil
	}

	// 2. 重写数据的回调
	rewriteFunc := func(key, value []byte, oldVP *vlog.ValuePointer) error {
		// 写入新的 vLog
		// 必须使用一个新的内部方法，不能调用 engine.Put，因为 engine.Put 会尝试获取 engine.mu 锁
		// 而 runGC 是在后台运行的，虽然 runGC 本身不持有锁，但如果在高并发下
		// GC 内部可能需要长时间运行。
		// 更重要的是，Put 会写 WAL。GC 重写不需要写 WAL。

		// 我们的策略是：GC 重写的数据直接写入 active vLog，然后生成新的 ValuePointer
		// 然后我们需要更新 LSM Tree 中的索引。
		// 这涉及到更新 MemTable。更新 MemTable 需要锁。

		// 正确的流程：
		// 1. 写 vLog (engine.vLogWriter.Write 是并发安全的，内部有锁)
		// 2. 获取 engine.mu 锁
		// 3. 更新 MemTable (engine.mutableMem.Put)
		// 4. 释放 engine.mu 锁

		// 这样避免了调用 engine.Put 带来的 WAL 开销和死锁风险（如果 Put 内部有复杂逻辑）
		// 不过目前 engine.Put 也是先拿锁。
		// 如果我们直接调用 Put，它会写 WAL。
		// 为了避免写 WAL，我们手动实现逻辑。

		// 1. 写 vLog
		newVP, err := engine.vLogWriter.Write(key, value)
		if err != nil {
			return err
		}

		// 2. 更新 LSM Tree
		engine.mu.Lock()
		defer engine.mu.Unlock()

		if engine.closed.Load() {
			return fmt.Errorf("engine closed")
		}

		// 再次检查 Key 是否依然有效（防止在重写过程中被删除或更新）
		// 虽然 checkKeyFunc 已经检查过，但那是在无锁状态下。
		// 现在我们持有锁，是绝对安全的。

		// 检查当前 MemTable/SSTable 中的 VP 是否指向被 GC 的旧文件
		// 这比较复杂，因为我们需要知道“旧 VP”是什么。
		// rewriteFunc 的签名只给了 key/value。
		// 实际上，只要我们写入新的 VP，就相当于一次 Update。
		// 如果用户在此期间更新了 Key，我们的写入会覆盖用户的更新吗？
		// 会！这是一个严重的数据竞争问题。

		// 解决方案：
		// GC 必须是“比较并交换”(CAS) 的语义。
		// 即：只有当当前 LSM Tree 中的 VP 等于我们准备回收的 VP 时，才更新为新的 VP。
		// 但是 rewriteFunc 无法知道“准备回收的 VP”。
		// 我们需要修改 rewriteFunc 的签名，或者在 checkKeyFunc 中不做检查，留到这里做。

		// 鉴于 ValueLogGC 的实现限制（它只传递 key/value），我们无法实现 CAS。
		// 我们必须修改 ValueLogGC 的设计，或者暂时接受写放大（即即使用户更新了，我们也再次更新）。
		// 但如果用户删除了 Key，我们再次写入，会导致已删除的 Key 复活！这是不可接受的。

		// 因此，我们必须在持有锁的情况下，再次检查 Key 的状态。
		// 只有当 Key 存在且指向的 VP 仍然位于“旧文件”中时，才更新。
		// 但我们怎么知道哪些是旧文件？
		// vLogGC 知道。但它没告诉我们。

		// 这是一个架构缺陷。
		// 临时修复：我们先通过 GetRaw 获取当前 VP。
		// 如果当前 VP 的 Fid 小于 activeFid（或者属于被 GC 的文件集合），则更新。
		// 但我们不知道被 GC 的文件 ID。

		// 为了稳定性，我们先简单地检查 Key 是否存在且未被删除。
		// 如果 Key 存在，我们就覆盖它。这会导致“复活”风险（如果用户刚删除了，而我们刚读到旧值）。
		// 所以必须检查 VP。

		currentValPtrBytes, found := engine.getRawNoLock(key)
		if !found || IsDeleted(currentValPtrBytes) {
			// Key 已被删除或不存在，放弃重写（这是安全的，丢弃垃圾）
			return nil
		}

		currentVP := vlog.DecodeValuePointer(currentValPtrBytes)
		if currentVP == nil {
			// 不是 VP，可能是新写入的小 Value，不应该被 vLog GC 覆盖
			return nil
		}

		if oldVP == nil {
			return nil
		}
		if currentVP.Fid != oldVP.Fid || currentVP.Offset != oldVP.Offset || currentVP.Len != oldVP.Len {
			return nil
		}

		engine.mutableMem.Put(key, newVP.Encode())
		return nil
	}

	engine.vLogGC = vlog.NewValueLogGC(engine.vlogDir, 0.5, checkKeyFunc, rewriteFunc)

	// 初始化刷写同步通道
	engine.flushDone = make(chan struct{}, 1)

	// 打开版本集合
	engine.versionSet, err = OpenVersionSet(dbDir, MaxLevels)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open version set: %v", err)
	}

	// 从 VersionSet 恢复 nextSSTableNum
	engine.nextSSTableNum = engine.versionSet.nextFileNum
	logger.Info("Recovered nextSSTableNum: %d", engine.nextSSTableNum)

	// 创建 Compactor
	engine.heat = NewHeatTracker(time.Duration(options.HotColdDecayIntervalSeconds)*time.Second, options.HotColdDecayFactor)
	engine.offloader, err = NewSSTableOffloader(options, engine.sstableDir)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to init offloader: %v", err)
	}
	engine.compactor = NewCompactor(dbDir, engine.versionSet, options, engine.heat.Get, func(fm *FileMetadata) error {
		if engine.offloader == nil {
			return nil
		}
		if !engine.allowOffload() {
			return nil
		}
		return engine.offloader.OffloadIfNeeded(fm)
	})
	engine.compactor.SetThrottle(func(level int) (bool, time.Duration) {
		return engine.allowCompaction(level)
	})

	// 启动后台 Compaction
	engine.compactor.Start()

	// SSTable 恢复不再需要加载到 engine.sstables
	// 我们将在 Get/LoadAllKeys 时按需从 VersionSet 加载

	logger.Info("LSM Engine initialized")

	// 恢复 WAL
	err = engine.recoverFromWAL()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to recover from WAL: %v", err)
	}

	engine.bgWG.Add(2)
	go func() {
		defer engine.bgWG.Done()
		engine.runGC()
	}()
	go func() {
		defer engine.bgWG.Done()
		engine.runBackgroundScheduler()
	}()

	return engine, nil
}

// recoverFromWAL 从 WAL 恢复数据
func (e *LSMEnergy) recoverFromWAL() error {
	walFile := filepath.Join(e.walDir, "current.wal")

	// 检查 WAL 文件是否存在
	if !WALExists(walFile) {
		// 没有 WAL 文件，创建新的
		var err error
		e.wal, err = NewWALWriterWithConfig(walFile, int64(64*1024*1024), e.options.SyncWAL, e.options.WALQueueSize, time.Duration(e.options.WALWriteTimeoutMs)*time.Millisecond)
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
	e.wal, err = NewWALWriterWithConfig(walFile, int64(64*1024*1024), e.options.SyncWAL, e.options.WALQueueSize, time.Duration(e.options.WALWriteTimeoutMs)*time.Millisecond)
	if err != nil {
		return err
	}

	// 检查是否需要刷写 MemTable
	if e.mutableMem.IsFull() {
		err = e.flushMemTable()
		if err != nil {
			return fmt.Errorf("failed to flush memtable during recovery: %v", err)
		}
	}

	return nil
}

// runGC 后台运行 GC
func (e *LSMEnergy) runGC() {
	ticker := time.NewTicker(5 * time.Minute) // 每 5 分钟尝试一次 GC
	defer ticker.Stop()

	for {
		select {
		case <-e.gcStop:
			return
		case <-ticker.C:
			runMs := 0
			scanBytes := int64(0)
			rewriteBytes := int64(0)
			if e.options != nil {
				runMs = e.options.ValueLogGCMaxRunMs
				scanBytes = e.options.ValueLogGCMaxScanBytes
				rewriteBytes = e.options.ValueLogGCMaxRewriteBytes
			}
			var maxDur time.Duration
			if runMs > 0 {
				maxDur = time.Duration(runMs) * time.Millisecond
			}
			err := e.vLogGC.RunGCWithBudget(scanBytes, rewriteBytes, maxDur)
			if err != nil {
				logger.Warn("Value Log GC failed: %v", err)
			} else {
				// logger.Info("Value Log GC completed successfully")
			}
		}
	}
}

func (e *LSMEnergy) runBackgroundScheduler() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-e.bgStop:
			return
		case <-ticker.C:
			writes := e.writesThisSec.Swap(0)
			e.writeOpsPerSec.Store(writes)
			if e.blockCache != nil {
				hit := e.blockCache.HitRate()
				if hit < 0 {
					hit = 0
				}
				if hit > 1 {
					hit = 1
				}
				e.cacheHitPermille.Store(uint32(hit * 1000))
			} else {
				e.cacheHitPermille.Store(1000)
			}
		}
	}
}

func (e *LSMEnergy) allowCompaction(level int) (bool, time.Duration) {
	if e.options == nil || e.versionSet == nil {
		return true, 0
	}

	v := e.versionSet.GetCurrentVersion()
	l0 := 0
	if v != nil && len(v.Files) > 0 {
		l0 = len(v.Files[0])
	}

	emergency := false
	if e.options.L0_StopWritesTrigger > 0 && l0 >= e.options.L0_StopWritesTrigger {
		emergency = true
	}
	if e.options.L0_SlowdownWritesTrigger > 0 && l0 >= e.options.L0_SlowdownWritesTrigger {
		emergency = true
	}

	if emergency {
		return true, 0
	}

	busy := e.flushing.Load()
	if !busy && e.options.BackgroundBusyWriteOpsPerSec > 0 {
		if int(e.writeOpsPerSec.Load()) >= e.options.BackgroundBusyWriteOpsPerSec {
			busy = true
		}
	}
	if !busy && e.options.BackgroundLowCacheHitPermille > 0 {
		if int(e.cacheHitPermille.Load()) <= e.options.BackgroundLowCacheHitPermille {
			busy = true
		}
	}

	if busy {
		return false, time.Duration(e.options.CompactionCooldownMs) * time.Millisecond
	}
	return true, 0
}

func (e *LSMEnergy) allowOffload() bool {
	if e.options == nil {
		return true
	}

	if e.flushing.Load() {
		return false
	}
	if e.options.BackgroundBusyWriteOpsPerSec > 0 {
		if int(e.writeOpsPerSec.Load()) >= e.options.BackgroundBusyWriteOpsPerSec {
			return false
		}
	}
	if e.options.BackgroundLowCacheHitPermille > 0 {
		if int(e.cacheHitPermille.Load()) <= e.options.BackgroundLowCacheHitPermille {
			return false
		}
	}
	return true
}

// getRawNoLock 获取 LSM Tree 中的原始值（不查 vLog，不加锁）
func (e *LSMEnergy) getRawNoLock(key []byte) ([]byte, bool) {
	if e.closed.Load() {
		return nil, false
	}

	// 1. Mutable MemTable
	val, found := e.mutableMem.Get(key)
	if found {
		return val, true
	}

	// 2. Immutable MemTable
	if e.immutableMem != nil {
		val, found := e.immutableMem.Get(key)
		if found {
			return val, true
		}
	}

	// 3. SSTable
	version := e.versionSet.GetCurrentVersion()
	if version == nil {
		return nil, false
	}

	// Level 0
	for i := len(version.Files[0]) - 1; i >= 0; i-- {
		fm := version.Files[0][i]
		var err error
		reader, err := e.getSSTableReader(fm.FileNum)
		if err != nil {
			continue
		}
		val, found := reader.Get(key)
		if found {
			return val, true
		}
	}

	// Level > 0
	for level := 1; level < MaxLevels; level++ {
		for _, fm := range version.Files[level] {
			var err error
			reader, err := e.getSSTableReader(fm.FileNum)
			if err != nil {
				continue
			}
			val, found := reader.Get(key)
			if found {
				return val, true
			}
		}
	}

	return nil, false
}

// getRaw 获取 LSM Tree 中的原始值（不查 vLog）
func (e *LSMEnergy) getRaw(key []byte) ([]byte, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.getRawNoLock(key)
}

// Put 写入键值对
func (e *LSMEnergy) Put(key, value []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed.Load() {
		return fmt.Errorf("engine is closed")
	}

	e.writesThisSec.Add(1)

	// 1. WiscKey 改造：写入 Value Log
	// 判断是否需要 KV 分离
	useVLog := false
	threshold := e.options.ValueThreshold
	if threshold >= 0 && len(value) >= threshold {
		useVLog = true
	}

	var valToStore []byte
	if useVLog {
		vp, err := e.vLogWriter.Write(key, value)
		if err != nil {
			return fmt.Errorf("failed to write value log: %v", err)
		}
		// 编码 ValuePointer 作为 LSM Tree 的 Value
		valToStore = vp.Encode()
	} else {
		// 小 Value 直接存储
		valToStore = value
	}

	// 2. 写入 WAL
	// 注意：如果是 ValuePointer，WAL 记录的是指针；如果是小 Value，WAL 记录的是真实数据
	err := e.wal.Put(key, valToStore)
	if err != nil {
		return fmt.Errorf("failed to write WAL: %v", err)
	}

	// 3. 写入 MemTable
	e.mutableMem.Put(key, valToStore)

	// 4. 更新序列号
	seq := atomic.AddUint64(&e.seqNum, 1)
	_ = seq // 暂时不使用

	// 5. 检查是否需要刷写
	if e.mutableMem.IsFull() {
		err = e.flushMemTable()
		if err != nil {
			return fmt.Errorf("failed to flush memtable: %v", err)
		}
	}

	return nil
}

// getSSTableReader 获取或打开 SSTable Reader
func (e *LSMEnergy) getSSTableReader(fileNum uint64) (*SSTableReader, error) {
	return e.tableCache.GetOrOpen(fileNum, func() (*SSTableReader, error) {
		sstablePath := filepath.Join(e.sstableDir, fmt.Sprintf("%06d.sstable", fileNum))
		reader, err := OpenSSTableForReadWithCache(fileNum, sstablePath, e.options, e.blockCache)
		if err == nil {
			return reader, nil
		}
		if os.IsNotExist(err) && e.offloader != nil {
			if derr := e.offloader.EnsureLocal(fileNum); derr == nil {
				return OpenSSTableForReadWithCache(fileNum, sstablePath, e.options, e.blockCache)
			}
		}
		return nil, err
	})
}

// Get 获取值
func (e *LSMEnergy) Get(key []byte) ([]byte, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed.Load() {
		return nil, false
	}

	// 1. 先在 Mutable MemTable 中查找
	valPtrBytes, found := e.mutableMem.Get(key)
	if found {
		if IsDeleted(valPtrBytes) {
			return nil, false
		}
		// 解码 ValuePointer 并读取真实 Value
		vp := vlog.DecodeValuePointer(valPtrBytes)
		if vp == nil {
			// 如果解码失败，可能是旧数据或小数据直接存储（未实现），这里暂时报错或返回原值
			return valPtrBytes, true
		}
		realVal, err := e.vLogReader.Read(vp)
		if err != nil {
			logger.Error("Failed to read value from log: %v", err)
			return nil, false
		}
		return realVal, true
	}

	// 2. 在 Immutable MemTable 中查找
	if e.immutableMem != nil {
		valPtrBytes, found := e.immutableMem.Get(key)
		if found {
			if IsDeleted(valPtrBytes) {
				return nil, false
			}
			vp := vlog.DecodeValuePointer(valPtrBytes)
			if vp == nil {
				return valPtrBytes, true
			}
			realVal, err := e.vLogReader.Read(vp)
			if err != nil {
				logger.Error("Failed to read value from log: %v", err)
				return nil, false
			}
			return realVal, true
		}
	}

	// 3. 在 SSTable 中查找
	// 获取当前版本
	version := e.versionSet.GetCurrentVersion()
	if version == nil {
		return nil, false
	}

	getFromFile := func(fm *FileMetadata, reader *SSTableReader) ([]byte, bool) {
		valPtrBytes, found := reader.Get(key)
		if !found {
			return nil, false
		}

		if e.heat != nil {
			e.heat.Add(fm.FileNum, 1)
		}

		if IsDeleted(valPtrBytes) {
			return nil, true // Found but deleted (tombstone)
		}

		vp := vlog.DecodeValuePointer(valPtrBytes)
		if vp == nil {
			// 兼容旧数据或直接存储的小数据
			return valPtrBytes, true
		}

		realVal, err := e.vLogReader.Read(vp)
		if err != nil {
			logger.Error("Failed to read value from log: %v", err)
			return nil, false
		}
		return realVal, true
	}

	// 遍历 Level 0 (从新到旧)
	for i := len(version.Files[0]) - 1; i >= 0; i-- {
		fm := version.Files[0][i]
		reader, err := e.getSSTableReader(fm.FileNum)
		if err != nil {
			logger.Warn("Failed to get reader for file %d: %v", fm.FileNum, err)
			continue
		}

		val, found := getFromFile(fm, reader)
		if found {
			if val == nil { // Tombstone
				return nil, false
			}
			return val, true
		}
	}

	// 遍历 Level > 0 (从低层到高层)
	for level := 1; level < MaxLevels; level++ {
		files := version.Files[level]
		if len(files) == 0 {
			continue
		}

		for _, fm := range files {
			if fm.SmallestKey != nil && bytes.Compare(key, fm.SmallestKey) < 0 {
				continue
			}
			if fm.LargestKey != nil && bytes.Compare(key, fm.LargestKey) > 0 {
				continue
			}

			reader, err := e.getSSTableReader(fm.FileNum)
			if err != nil {
				continue
			}

			val, found := getFromFile(fm, reader)
			if found {
				if val == nil { // Tombstone
					return nil, false
				}
				return val, true
			}
		}
	}

	return nil, false
}

// Delete 删除键
func (e *LSMEnergy) Delete(key []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed.Load() {
		return fmt.Errorf("engine is closed")
	}

	e.writesThisSec.Add(1)

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
	if e.mutableMem.IsFull() {
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
	if e.mutableMem.Size() == 0 {
		return nil
	}

	// 1. 将当前 MemTable 转为 Immutable
	oldMem := e.mutableMem
	e.immutableMem = NewImmutableMemTable(oldMem)

	// 2. 创建新的 Mutable MemTable
	e.mutableMem = NewMemTable(e.options.MemTableSize)

	// 3. 同步刷写（不启动 goroutine）
	imm := e.immutableMem // 保存引用用于后续释放
	defer func() {
		if imm != nil {
			imm.Unref()
		}
	}()

	err := e.flushImmutableToSSTable(imm)
	if err != nil {
		return fmt.Errorf("failed to flush immutable memtable: %v", err)
	}

	e.immutableMem = nil
	return nil
}

// flushMemTable 异步刷写 MemTable 到 SSTable
func (e *LSMEnergy) flushMemTable() error {
	if e.mutableMem.Size() == 0 {
		return nil
	}

	if !e.flushing.CompareAndSwap(false, true) {
		return nil
	}

	// 1. 将当前 MemTable 转为 Immutable
	oldMem := e.mutableMem
	e.immutableMem = NewImmutableMemTable(oldMem)

	// 2. 创建新的 Mutable MemTable
	e.mutableMem = NewMemTable(e.options.MemTableSize)

	// 3. 异步刷写 Immutable MemTable 到 SSTable
	e.flushWG.Add(1)
	go func(imm *ImmutableMemTable) {
		defer e.flushWG.Done()
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
			logger.Error("Error flushing immutable memtable: %v", err)
		}
	}(e.immutableMem)

	e.immutableMem = nil

	return nil
}

// flushImmutableToSSTable 将 Immutable MemTable 刷写到 SSTable
func (e *LSMEnergy) flushImmutableToSSTable(imm *ImmutableMemTable) error {
	start := time.Now()
	// 生成 SSTable 文件名
	sstableNum := e.versionSet.GetNextFileNum()
	filename := filepath.Join(e.sstableDir, fmt.Sprintf("%06d.sstable", sstableNum))

	// 创建 SSTable Builder
	builder, err := NewSSTableBuilder(filename, e.options)
	if err != nil {
		e.flushErrors.Add(1)
		return fmt.Errorf("failed to create sstable builder: %v", err)
	}
	defer builder.Abort() // 如果出错则回滚

	entryCount := 0
	err = imm.ForEach(func(key, value []byte) error {
		entryCount++
		return builder.Add(key, value)
	})
	if err != nil {
		e.flushErrors.Add(1)
		return fmt.Errorf("failed to iterate immutable memtable: %v", err)
	}

	// 完成 SSTable 构建
	err = builder.Finish()
	if err != nil {
		e.flushErrors.Add(1)
		return fmt.Errorf("failed to finish sstable: %v", err)
	}

	sstablePath := filepath.Join(e.sstableDir, fmt.Sprintf("%06d.sstable", sstableNum))
	reader, err := OpenSSTableForReadWithCache(sstableNum, sstablePath, e.options, e.blockCache)
	if err != nil {
		e.flushErrors.Add(1)
		return fmt.Errorf("failed to open sstable reader: %v", err)
	}

	// 获取文件信息
	info, err := os.Stat(filename)
	if err != nil {
		reader.Close()
		e.flushErrors.Add(1)
		return fmt.Errorf("failed to stat sstable file: %v", err)
	}

	// 创建文件元数据
	fm := &FileMetadata{
		FileNum:     sstableNum,
		Size:        info.Size(),
		SmallestKey: builder.SmallestKey(),
		LargestKey:  builder.LargestKey(),
		Level:       0, // Level 0
	}

	// 添加到版本集合
	err = e.versionSet.LogAddFile(fm)
	if err != nil {
		reader.Close()
		e.flushErrors.Add(1)
		return fmt.Errorf("failed to log add file: %v", err)
	}

	if e.offloader != nil {
		if err := e.offloader.OffloadIfNeeded(fm); err != nil {
			reader.Close()
			e.flushErrors.Add(1)
			return fmt.Errorf("failed to offload sstable: %v", err)
		}
	}

	// 添加到缓存
	e.tableCache.Add(sstableNum, reader)
	logger.Debug("SSTable added to table cache")

	e.flushCount.Add(1)
	e.flushBytesWritten.Add(uint64(info.Size()))
	e.flushDurationNanos.Add(uint64(time.Since(start).Nanoseconds()))

	return nil
}

// Close 关闭 LSM 引擎
func (e *LSMEnergy) Close() error {
	e.mu.Lock()
	if e.closed.Load() {
		e.mu.Unlock()
		return nil
	}
	e.closed.Store(true)
	e.mu.Unlock()

	e.closeOnce.Do(func() {
		close(e.gcStop)
		close(e.bgStop)
	})

	e.bgWG.Wait()
	e.flushWG.Wait()

	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.closed.Load() {
		return nil
	}

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
			// 在 Windows 上，如果文件仍然被占用（尽管我们已经尝试关闭了），Close 可能会失败
			// 但我们仍然应该继续关闭其他资源
			logger.Warn("Failed to close WAL: %v", err)
		} else {
			logger.Info("WAL closed")
		}
	}

	// 4. 关闭所有 SSTable
	if e.tableCache != nil {
		if err := e.tableCache.Close(); err != nil {
			logger.Warn("Failed to close table cache: %v", err)
		}
	}
	logger.Info("Closed table cache")

	// 5. 关闭版本集合
	if e.versionSet != nil {
		err := e.versionSet.Close()
		if err != nil {
			return err
		}
		logger.Info("VersionSet closed")
	}

	// 4. 关闭 Value Log
	if e.vLogReader != nil {
		if err := e.vLogReader.Close(); err != nil {
			logger.Warn("Failed to close value log reader: %v", err)
		}
	}
	if e.vLogWriter != nil {
		if err := e.vLogWriter.Close(); err != nil {
			logger.Warn("Failed to close value log writer: %v", err)
		}
	}
	logger.Info("Value Log closed")

	logger.Info("LSM Engine closed successfully")
	return nil
}

func (e *LSMEnergy) GetStats() map[string]interface{} {
	if e == nil {
		return map[string]interface{}{"enabled": false}
	}

	stats := make(map[string]interface{})
	stats["enabled"] = true
	stats["closed"] = e.closed.Load()
	stats["flushing"] = e.flushing.Load()
	stats["memtable_bytes"] = e.mutableMem.Size()
	stats["memtable_entries"] = e.mutableMem.EntryCount()
	if e.immutableMem != nil {
		stats["immutable_memtable_bytes"] = e.immutableMem.Size()
		stats["immutable_memtable_entries"] = e.immutableMem.EntryCount()
	} else {
		stats["immutable_memtable_bytes"] = int32(0)
		stats["immutable_memtable_entries"] = int64(0)
	}

	stats["writes_per_sec"] = e.writeOpsPerSec.Load()
	stats["cache_hit_permille"] = e.cacheHitPermille.Load()
	stats["flush_count"] = e.flushCount.Load()
	stats["flush_bytes_written"] = e.flushBytesWritten.Load()
	stats["flush_duration_ms"] = float64(e.flushDurationNanos.Load()) / 1e6
	stats["flush_errors"] = e.flushErrors.Load()

	if e.options != nil {
		stats["value_threshold"] = e.options.ValueThreshold
		stats["block_size"] = e.options.BlockSize
		stats["memtable_size_limit"] = e.options.MemTableSize
		stats["max_open_files"] = e.options.MaxOpenFiles
		stats["use_bloom_filter"] = e.options.UseBloomFilter
		stats["use_cache"] = e.options.UseCache
		stats["cache_size"] = e.options.CacheSize
		stats["sync_wal"] = e.options.SyncWAL
		stats["write_ahead_log"] = e.options.WriteAheadLog
	}

	if e.wal != nil {
		stats["wal"] = e.wal.Stats()
	}
	if e.vLogWriter != nil {
		stats["vlog"] = e.vLogWriter.Stats()
	}

	if e.versionSet != nil {
		stats["version_set"] = e.versionSet.GetStats()
	}
	if e.compactor != nil {
		cs := e.compactor.GetStats()
		stats["compaction"] = map[string]interface{}{
			"num_compactions":  cs.NumCompactions,
			"num_files_merged": cs.NumFilesMerged,
			"bytes_read":       cs.BytesRead,
			"bytes_written":    cs.BytesWritten,
			"duration_ms":      cs.DurationMs,
		}
	}

	tableStats := map[string]interface{}{}
	if e.tableCache != nil {
		tableStats["len"] = e.tableCache.Len()
		tableStats["capacity"] = e.tableCache.Capacity()
		tableStats["bloom"] = e.tableCache.BloomStats()
	} else {
		tableStats["len"] = 0
		tableStats["capacity"] = 0
	}
	stats["table_cache"] = tableStats

	blockStats := map[string]interface{}{}
	if e.blockCache != nil {
		hits, misses, evictions, hitRate := e.blockCache.Stats()
		blockStats["enabled"] = true
		blockStats["hits"] = hits
		blockStats["misses"] = misses
		blockStats["evictions"] = evictions
		blockStats["hit_rate"] = hitRate
		blockStats["size_bytes"] = e.blockCache.Size()
		blockStats["max_size_bytes"] = e.blockCache.MaxSize()
		blockStats["items"] = e.blockCache.Len()
		blockStats["segments"] = e.blockCache.SegmentStats()
	} else {
		blockStats["enabled"] = false
	}
	stats["block_cache"] = blockStats

	return stats
}

// GetSeqNum 获取当前序列号
func (e *LSMEnergy) GetSeqNum() uint64 {
	return atomic.LoadUint64(&e.seqNum)
}

// GetSSTableCount 获取 SSTable 数量
func (e *LSMEnergy) GetSSTableCount() int {
	return e.versionSet.GetCurrentVersion().GetFileCount()
}

// LoadAllKeys 加载所有 key-value 到内存（用于冷启动全量加载）
// 返回一个 map[string][]byte，包含所有未删除的键值对
func (e *LSMEnergy) LoadAllKeys() (map[string][]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := make(map[string][]byte)
	deletedKeys := make(map[string]struct{}) // 记录已删除的 key

	logger.Info("LoadAllKeys starting")

	// 辅助函数：处理单个 key-value
	processEntry := func(source string, key, valPtrBytes []byte) {
		keyStr := string(key)

		// 如果已经被标记为删除，或者已经加载了较新版本，则跳过
		if _, deleted := deletedKeys[keyStr]; deleted {
			return
		}
		if _, exists := result[keyStr]; exists {
			return
		}

		// 检查是否为 Tombstone
		if IsDeleted(valPtrBytes) {
			deletedKeys[keyStr] = struct{}{}
		} else {
			// 加载数据：需要从 Value Log 读取
			vp := vlog.DecodeValuePointer(valPtrBytes)
			var realVal []byte
			var err error

			if vp == nil {
				// 兼容旧数据
				realVal = valPtrBytes
			} else {
				realVal, err = e.vLogReader.Read(vp)
				if err != nil {
					logger.Error("Failed to read value from log for key %s: %v", keyStr, err)
					return // Skip this key
				}
			}

			// 深拷贝
			keyCopy := make([]byte, len(key))
			copy(keyCopy, key)
			valueCopy := make([]byte, len(realVal))
			copy(valueCopy, realVal)
			result[keyStr] = valueCopy
		}
	}

	// 1. 从 MemTable 加载
	if e.mutableMem != nil {
		it := e.mutableMem.Iterator()
		for it.SeekToFirst(); it.Valid(); it.Next() {
			processEntry("MemTable", it.Key(), it.Value())
		}
	}

	// 2. 从 Immutable MemTable 加载
	if e.immutableMem != nil && e.immutableMem.memtable != nil {
		it := e.immutableMem.memtable.Iterator()
		for it.SeekToFirst(); it.Valid(); it.Next() {
			processEntry("ImmutableMemTable", it.Key(), it.Value())
		}
	}

	// 3. 从所有 SSTable 加载
	// 注意：LoadAllKeys 需要按照数据的新旧顺序（从新到旧）来加载，以确保旧数据不会覆盖新数据。
	// 但 processEntry 内部已经有了 exists 检查：`if _, exists := result[keyStr]; exists { return }`
	// 所以只要我们保证“新数据先被处理”，逻辑就是正确的。

	version := e.versionSet.GetCurrentVersion()
	if version != nil {
		// Level 0: 文件是重叠的，且按 FileNum 排序（通常较新的在后面，但 Compaction 可能改变这一点）。
		// 在 Level 0 中，FileNum 越大通常意味着越新（由 Immutable MemTable 刷写产生）。
		// 所以我们需要从大到小遍历 Level 0 文件。
		for i := len(version.Files[0]) - 1; i >= 0; i-- {
			fm := version.Files[0][i]
			// 修复：确保 FileNum 正确
			if fm == nil {
				continue
			}

			reader, err := e.getSSTableReader(fm.FileNum)
			if err != nil {
				logger.Warn("Failed to get reader for file %d: %v", fm.FileNum, err)
				continue
			}

			it := reader.NewIterator()
			// 修复：检查 iterator 是否有效
			if it == nil {
				logger.Warn("Failed to create iterator for file %d", fm.FileNum)
				continue
			}

			iterCount := 0
			for it.SeekToFirst(); it.Valid(); it.Next() {
				processEntry(fmt.Sprintf("SSTable#%d(L0)", fm.FileNum), it.Key(), it.Value())
				iterCount++
			}
			it.Release()

			// DEBUG
			// fmt.Printf("[LoadAllKeys] Scanned SSTable %d (L0), found %d entries\n", fm.FileNum, iterCount)
		}

		// Level > 0: 文件不重叠，且层级越小数据越新。
		// 所以我们需要先处理 Level 1，然后 Level 2...
		// 在同一层级内，文件是不重叠的，所以顺序不影响正确性（对于不同的 Key）。
		// 但为了保险起见，我们还是顺序遍历。
		for level := 1; level < MaxLevels; level++ {
			for _, fm := range version.Files[level] {
				reader, err := e.getSSTableReader(fm.FileNum)
				if err != nil {
					continue
				}

				it := reader.NewIterator()
				for it.SeekToFirst(); it.Valid(); it.Next() {
					processEntry(fmt.Sprintf("SSTable#%d(L%d)", fm.FileNum, level), it.Key(), it.Value())
				}
				it.Release()
			}
		}
	}

	logger.Info("LoadAllKeys loaded total %d keys, ignored %d deleted keys", len(result), len(deletedKeys))
	return result, nil
}
