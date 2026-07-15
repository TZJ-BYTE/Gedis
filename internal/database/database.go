package database

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/internal/persistence"
	"github.com/TZJ-BYTE/RediGo/internal/rustengine"
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// DatabaseType 数据库类型
type DatabaseType int

const (
	// MemoryOnly 纯内存模式（默认）
	MemoryOnly DatabaseType = iota
	// LSMPersistent LSM 持久化模式
	LSMPersistent
)

const (
	// ShardCount 分段锁数量
	ShardCount = 256
)

// DatabaseConfig 数据库配置
type DatabaseConfig struct {
	Type              DatabaseType         // 数据库类型
	DataDir           string               // 数据目录（仅 LSM 模式需要）
	Options           *persistence.Options // LSM 选项（仅 LSM 模式需要）
	ColdStartStrategy string
	WriteMode         string
	Durability        string
}

// shard 分段结构
type shard struct {
	data map[string]*datastruct.DataValue
	lock sync.RWMutex
}

// Database 数据库结构
type Database struct {
	id     int
	shards [ShardCount]shard

	// LSM 引擎（可选）
	lsmEngine       *persistence.LSMEnergy
	rustEngine      *rustengine.Engine
	config          *DatabaseConfig
	keyHeat         *KeyTopK
	lsmPutErrors    atomic.Uint64
	lsmDeleteErrors atomic.Uint64
	lsmLastError    atomic.Value
	lsmDeleteStop   atomic.Value
	lsmDeleteWG     sync.WaitGroup
	lsmDeleteCh     chan string
	lsmDeleteEnq    atomic.Uint64
	lsmDeleteDrop   atomic.Uint64

	// 内存管理
	usedMemory     int64  // 当前使用内存（字节），原子操作
	maxMemory      int64  // 最大内存限制（字节）
	evictionPolicy string // 淘汰策略

	expireStop atomic.Value
	expireWG   sync.WaitGroup

	expireRuns          atomic.Uint64
	expireScanned       atomic.Uint64
	expireExpired       atomic.Uint64
	expireDurationNanos atomic.Uint64
}

// DefaultDatabaseConfig 返回默认配置
func DefaultDatabaseConfig() *DatabaseConfig {
	return &DatabaseConfig{
		Type:              MemoryOnly,
		DataDir:           "",
		ColdStartStrategy: "no_load",
	}
}

// NewDatabase 创建新数据库（使用默认配置，纯内存）
func NewDatabase(id int) *Database {
	cfg := config.DefaultConfig()
	db := &Database{
		id:             id,
		config:         DefaultDatabaseConfig(),
		maxMemory:      cfg.MaxMemory,
		evictionPolicy: cfg.MaxMemoryPolicy,
	}
	db.expireStop.Store((chan struct{})(nil))
	db.lsmDeleteStop.Store((chan struct{})(nil))
	if strings.Contains(db.evictionPolicy, "lru") {
		db.keyHeat = NewKeyTopK(1024)
	}
	return db
}

// NewDatabaseWithConfig 使用配置创建数据库
func NewDatabaseWithConfig(id int, dbConfig *DatabaseConfig) (*Database, error) {
	// 获取全局配置
	globalConfig := config.DefaultConfig()

	db := &Database{
		id:             id,
		config:         dbConfig,
		maxMemory:      globalConfig.MaxMemory,
		evictionPolicy: globalConfig.MaxMemoryPolicy,
	}
	db.expireStop.Store((chan struct{})(nil))
	db.lsmDeleteStop.Store((chan struct{})(nil))
	if strings.Contains(db.evictionPolicy, "lru") {
		db.keyHeat = NewKeyTopK(1024)
	}

	// 如果是 LSM 持久化模式，初始化 LSM 引擎
	if dbConfig.Type == LSMPersistent {
		if dbConfig.DataDir == "" {
			return nil, fmt.Errorf("data directory is required for LSM mode")
		}

		options := dbConfig.Options
		if options == nil {
			options = persistence.DefaultOptions()
		}

		var err error
		db.rustEngine, err = rustengine.Open(rustengine.Options{
			DataDir:            dbConfig.DataDir,
			SegmentSizeBytes:   uint64(options.MemTableSize),
			CheckpointAfterOps: 1024,
			SyncPolicy:         rustSyncPolicy(dbConfig.Durability),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to open Rust engine: %v", err)
		}
		db.lsmDeleteCh = make(chan string, 4096)
		db.StartLSMDeleteWorker()

		strategy := strings.ToLower(strings.TrimSpace(dbConfig.ColdStartStrategy))
		switch strategy {
		case "load_all":
			// 全量加载到内存
			if err := db.loadAllFromPersistence(); err != nil {
				logger.Warn("Failed to load all data from LSM: %v", err)
			}
		case "lazy_load":
			logger.Info("LSM lazy load enabled, will fallback on read")
		case "no_load", "":
		default:
			// 不加载（默认）
			logger.Info("LSM cold start: no data loading")
		}
	}

	return db, nil
}

func (db *Database) StartExpireCleaner() {
	ch := make(chan struct{})
	if !db.expireStop.CompareAndSwap((chan struct{})(nil), ch) {
		close(ch)
		return
	}

	db.expireWG.Add(1)
	go func() {
		defer db.expireWG.Done()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				start := time.Now()
				db.expireRuns.Add(1)
				scanned, expired := db.expireSample(time.Now().UnixMilli(), 64)
				db.expireScanned.Add(uint64(scanned))
				db.expireExpired.Add(uint64(expired))
				db.expireDurationNanos.Add(uint64(time.Since(start).Nanoseconds()))
			case <-ch:
				return
			}
		}
	}()
}

func (db *Database) StopExpireCleaner() {
	cur, _ := db.expireStop.Load().(chan struct{})
	if cur == nil {
		return
	}
	if db.expireStop.CompareAndSwap(cur, (chan struct{})(nil)) {
		close(cur)
		db.expireWG.Wait()
	}
}

func (db *Database) StartLSMDeleteWorker() {
	if db == nil || !db.hasPersistence() || db.lsmDeleteCh == nil {
		return
	}
	ch := make(chan struct{})
	if !db.lsmDeleteStop.CompareAndSwap((chan struct{})(nil), ch) {
		close(ch)
		return
	}
	db.lsmDeleteWG.Add(1)
	go func() {
		defer db.lsmDeleteWG.Done()
		for {
			select {
			case <-ch:
				return
			case k := <-db.lsmDeleteCh:
				_ = db.lsmDelete(k)
			}
		}
	}()
}

func (db *Database) StopLSMDeleteWorker() {
	cur, _ := db.lsmDeleteStop.Load().(chan struct{})
	if cur == nil {
		return
	}
	if db.lsmDeleteStop.CompareAndSwap(cur, (chan struct{})(nil)) {
		close(cur)
		db.lsmDeleteWG.Wait()
	}
}

func (db *Database) enqueueExpiredLSMDelete(key string) {
	if db == nil || !db.hasPersistence() || db.lsmDeleteCh == nil {
		return
	}
	db.lsmDeleteEnq.Add(1)
	select {
	case db.lsmDeleteCh <- key:
	default:
		db.lsmDeleteDrop.Add(1)
	}
}

func (db *Database) expireSample(nowMs int64, samples int) (scanned int, expired int) {
	for i := 0; i < samples; i++ {
		shard := &db.shards[rand.Intn(ShardCount)]
		shard.lock.RLock()
		var k string
		var v *datastruct.DataValue
		for kk, vv := range shard.data {
			k = kk
			v = vv
			break
		}
		shard.lock.RUnlock()
		if k == "" || v == nil {
			continue
		}
		scanned++
		if v.ExpireTime > 0 && nowMs > v.ExpireTime {
			if db.Delete(k) {
				expired++
			}
		}
	}
	return scanned, expired
}

// getShardIndex 获取分段索引
func getShardIndex(key string) int {
	h := fnv.New32a()
	h.Write(stringToBytesRO(key))
	return int(h.Sum32()) % ShardCount
}

// getShard 获取 key 对应的 shard
func (db *Database) getShard(key string) *shard {
	return &db.shards[getShardIndex(key)]
}

// loadAllFromLSM 从 LSM 全量加载所有数据到内存
func (db *Database) loadAllFromLSM() error {
	if db.lsmEngine == nil {
		return fmt.Errorf("LSM engine not initialized")
	}

	logger.Info("Loading all data from LSM into memory...")
	logger.Info("Loading all keys from LSM... SSTable count: %d", db.lsmEngine.GetSSTableCount())

	// 使用 LSM Engine 提供的公开方法加载所有键值对
	allData, err := db.lsmEngine.LoadAllKeys()
	if err != nil {
		return fmt.Errorf("failed to load keys from LSM: %v", err)
	}

	logger.Info("LoadAllKeys returned %d keys", len(allData))

	keysLoaded := 0
	deserializeErrors := 0

	for key, valueBytes := range allData {
		// 反序列化 DataValue
		dataValue, err := datastruct.DeserializeDataValue(valueBytes)
		if err != nil {
			logger.Warn("Failed to deserialize key %s: %v", key, err)
			deserializeErrors++
			continue
		}

		// 检查过期
		if dataValue.IsExpired() {
			continue
		}

		// 分段锁
		shard := db.getShard(key)
		shard.lock.Lock()
		if shard.data == nil {
			shard.data = make(map[string]*datastruct.DataValue)
		}
		shard.data[key] = dataValue
		shard.lock.Unlock()

		// 更新内存统计
		db.updateMemoryUsage(int64(len(key)) + dataValue.ApproximateSize())
		keysLoaded++
	}

	logger.Info("Successfully loaded %d keys into memory map. Used memory: %d bytes", keysLoaded, db.usedMemory)
	return nil
}

// deserializeDataValue 反序列化 DataValue
func deserializeDataValue(data []byte) (*datastruct.DataValue, error) {
	return datastruct.DeserializeDataValue(data)
}

// updateMemoryUsage 更新内存使用量
func (db *Database) updateMemoryUsage(delta int64) {
	atomic.AddInt64(&db.usedMemory, delta)
}

func (db *Database) persistenceWriteMode() string {
	if db == nil || db.config == nil || db.config.WriteMode == "" {
		return "strong"
	}
	return strings.ToLower(db.config.WriteMode)
}

func (db *Database) persistenceDurability() string {
	if db == nil || db.config == nil || db.config.Durability == "" {
		return "wal_fsync"
	}
	return strings.ToLower(db.config.Durability)
}

func (db *Database) recordLSMError(err error) {
	if err == nil {
		return
	}
	db.lsmLastError.Store(err.Error())
}

func (db *Database) lsmPut(key string, value []byte) error {
	if db == nil || !db.hasPersistence() {
		return nil
	}
	var err error
	switch {
	case db.rustEngine != nil:
		err = db.rustEngine.Put(stringToBytesRO(key), value)
	case db.lsmEngine != nil:
		err = db.lsmEngine.Put(stringToBytesRO(key), value)
	}
	if err != nil {
		db.lsmPutErrors.Add(1)
		db.recordLSMError(err)
		if db.persistenceWriteMode() == "weak" {
			logger.Error("Failed to write to LSM: %v", err)
			return nil
		}
		return err
	}
	return nil
}

func (db *Database) lsmDelete(key string) error {
	if db == nil || !db.hasPersistence() {
		return nil
	}
	var err error
	switch {
	case db.rustEngine != nil:
		err = db.rustEngine.Delete(stringToBytesRO(key))
	case db.lsmEngine != nil:
		err = db.lsmEngine.Delete(stringToBytesRO(key))
	}
	if err != nil {
		db.lsmDeleteErrors.Add(1)
		db.recordLSMError(err)
		if db.persistenceWriteMode() == "weak" {
			logger.Error("Failed to delete from LSM: %v", err)
			return nil
		}
		return err
	}
	return nil
}

// evictIfNeeded 检查内存是否超限并尝试淘汰
func (db *Database) evictIfNeeded() bool {
	if db.evictionPolicy == "noeviction" {
		// 如果策略是不淘汰，检查是否超限
		return atomic.LoadInt64(&db.usedMemory) <= db.maxMemory
	}

	for atomic.LoadInt64(&db.usedMemory) > db.maxMemory {
		// 尝试淘汰一个 key
		if !db.evictOneKey() {
			// 如果无法淘汰任何 key（例如数据库为空，或者 volatile 策略下没有过期 key）
			return false
		}
	}
	return true
}

// evictOneKey 尝试淘汰一个 key
func (db *Database) evictOneKey() bool {
	// 尝试多次随机选择 shard，以应对数据稀疏的情况
	maxAttempts := 1000
	for i := 0; i < maxAttempts; i++ {
		shardIdx := rand.Intn(ShardCount)
		shard := &db.shards[shardIdx]

		shard.lock.RLock()
		// 如果 shard 为空，尝试下一个
		if len(shard.data) == 0 {
			shard.lock.RUnlock()
			continue
		}
		// fmt.Printf("Sampling shard %d with %d keys\n", shardIdx, len(shard.data))

		// 采样
		var bestKey string
		bestScore := int64(math.MinInt64)

		samples := 5
		count := 0

		for key, val := range shard.data {
			score := int64(math.MinInt64)

			switch db.evictionPolicy {
			case "allkeys-lru":
				// LRU: 越久未访问 (LastAccessedAt 越小)，分数越高
				// Score = MaxInt64 - LastAccessedAt
				// 为了方便比较，我们直接找 LastAccessedAt 最小的
				score = ^val.LastAccessedAt // 取反，越小的值变成越大的值

			case "volatile-lru":
				if val.ExpireTime > 0 {
					score = ^val.LastAccessedAt
				}

			case "allkeys-random":
				score = 1 // 只要找到就可以

			case "volatile-random":
				if val.ExpireTime > 0 {
					score = 1
				}
			}

			if score > bestScore {
				bestKey = key
				bestScore = score
			}

			count++
			if count >= samples {
				break
			}
		}
		shard.lock.RUnlock()

		if bestKey != "" {
			// 执行删除
			db.Delete(bestKey)
			return true
		}
	}

	return false
}

// Get 获取键值
func (db *Database) Get(key string) (*datastruct.DataValue, bool) {
	shard := db.getShard(key)
	// 先尝试从内存读取
	shard.lock.RLock()
	value, exists := shard.data[key]
	shard.lock.RUnlock()

	if exists && value != nil {
		// 检查过期
		if value.IsExpired() {
			shard.lock.Lock()
			cur := shard.data[key]
			if cur != nil && cur.IsExpired() {
				delete(shard.data, key)
				memDelta := int64(len(key)) + cur.ApproximateSize()
				db.updateMemoryUsage(-memDelta)
			}
			shard.lock.Unlock()
			db.enqueueExpiredLSMDelete(strings.Clone(key))
			return nil, false
		}
		if db.keyHeat != nil {
			db.keyHeat.Add(key)
		}
		return value, true
	}

	// 内存中没有，尝试从 LSM 读取（懒加载）
	if db.hasPersistence() {
		valBytes, found, err := db.persistenceGet(key)
		if err != nil {
			db.recordLSMError(err)
			logger.Warn("Failed to read key %s from persistence: %v", key, err)
			return nil, false
		}
		if found {
			// 反序列化
			dataValue, err := datastruct.DeserializeDataValue(valBytes)
			if err != nil {
				logger.Warn("Failed to deserialize key %s from LSM: %v", key, err)
				// 反序列化失败，视为不存在
				return nil, false
			}

			// 检查过期
			if dataValue.IsExpired() {
				db.enqueueExpiredLSMDelete(strings.Clone(key))
				return nil, false
			}

			stableKey := strings.Clone(key)

			// 加载到内存（热点数据）
			shard.lock.Lock()
			if shard.data == nil {
				shard.data = make(map[string]*datastruct.DataValue)
			}
			// 双重检查
			if existingValue, ok := shard.data[stableKey]; ok {
				shard.lock.Unlock()
				if existingValue.IsExpired() {
					return nil, false
				}
				if db.keyHeat != nil {
					db.keyHeat.Add(stableKey)
				}
				return existingValue, true
			}

			shard.data[stableKey] = dataValue
			shard.lock.Unlock()

			if db.keyHeat != nil {
				db.keyHeat.Add(stableKey)
			}
			return dataValue, true
		}
	}

	return nil, false
}

func (db *Database) GetBytes(key []byte) (*datastruct.DataValue, bool) {
	return db.Get(bytesToString(key))
}

// Set 设置键值
func (db *Database) Set(key string, value *datastruct.DataValue) error {
	// 检查内存并尝试淘汰
	if !db.evictIfNeeded() {
		return fmt.Errorf("OOM command not allowed when used memory (%d) > 'maxmemory' (%d)", db.usedMemory, db.maxMemory)
	}

	newKey := false
	if value != nil {
		switch v := value.Value.(type) {
		case *datastruct.String:
			if v != nil {
				v.Data = strings.Clone(v.Data)
			}
		case *datastruct.List:
			if v != nil && len(v.Data) > 0 {
				dst := make([]string, len(v.Data))
				for i := range v.Data {
					dst[i] = strings.Clone(v.Data[i])
				}
				v.Data = dst
			}
		case *datastruct.Hash:
			if v != nil && len(v.Data) > 0 {
				dst := make(map[string]string, len(v.Data))
				for k, vv := range v.Data {
					dst[strings.Clone(k)] = strings.Clone(vv)
				}
				v.Data = dst
			}
		case *datastruct.Set:
			if v != nil && len(v.Data) > 0 {
				dst := make(map[string]struct{}, len(v.Data))
				for m := range v.Data {
					dst[strings.Clone(m)] = struct{}{}
				}
				v.Data = dst
			}
		case *datastruct.ZSet:
			if v != nil && len(v.Data) > 0 {
				dst := make(map[string]float64, len(v.Data))
				for m, score := range v.Data {
					dst[strings.Clone(m)] = score
				}
				v.Data = dst
				if len(v.Scores) > 0 {
					sdst := make([]datastruct.ZSetMember, len(v.Scores))
					for i := range v.Scores {
						sdst[i] = datastruct.ZSetMember{Member: strings.Clone(v.Scores[i].Member), Score: v.Scores[i].Score}
					}
					v.Scores = sdst
				}
			}
		}
	}

	shard := db.getShard(key)
	shard.lock.Lock()
	if shard.data == nil {
		shard.data = make(map[string]*datastruct.DataValue)
	}
	old, exists := shard.data[key]
	if exists {
		shard.data[key] = value
	} else {
		key = strings.Clone(key)
		newKey = true
		shard.data[key] = value
	}
	shard.lock.Unlock()

	if db.hasPersistence() {
		dataBytes, err := value.Serialize()
		if err != nil {
			db.recordLSMError(err)
			if db.persistenceWriteMode() == "weak" {
				logger.Error("Failed to serialize value for key %s: %v", key, err)
			} else {
				return err
			}
		} else {
			if err := db.lsmPut(key, dataBytes); err != nil {
				return err
			}
		}
	}

	// 更新内存统计
	var oldSize int64
	if old != nil {
		oldSize = old.ApproximateSize()
	}
	var newSize int64
	if value != nil {
		newSize = value.ApproximateSize()
	}
	memDelta := newSize - oldSize
	if newKey {
		memDelta += int64(len(key))
	}
	db.updateMemoryUsage(memDelta)
	return nil
}

func (db *Database) SetBytes(key []byte, value *datastruct.DataValue) error {
	return db.Set(bytesToString(key), value)
}

func (db *Database) DeleteWithError(key string) (bool, error) {
	shard := db.getShard(key)
	shard.lock.Lock()
	_, exists := shard.data[key]

	if exists {
		val := shard.data[key]
		delete(shard.data, key)
		memDelta := int64(len(key)) + val.ApproximateSize()
		db.updateMemoryUsage(-memDelta)
	}
	shard.lock.Unlock()

	if err := db.lsmDelete(key); err != nil {
		return exists, err
	}
	return exists, nil
}

func (db *Database) Delete(key string) bool {
	ok, _ := db.DeleteWithError(key)
	return ok
}

func (db *Database) DeleteBytes(key []byte) bool {
	return db.Delete(bytesToString(key))
}

// Exists 检查键是否存在
func (db *Database) Exists(key string) bool {
	shard := db.getShard(key)
	shard.lock.RLock()
	value, exists := shard.data[key]
	shard.lock.RUnlock()
	if !exists || value == nil {
		return false
	}
	if value.IsExpired() {
		shard.lock.Lock()
		cur := shard.data[key]
		if cur != nil && cur.IsExpired() {
			delete(shard.data, key)
			memDelta := int64(len(key)) + cur.ApproximateSize()
			db.updateMemoryUsage(-memDelta)
		}
		shard.lock.Unlock()
		db.enqueueExpiredLSMDelete(strings.Clone(key))
		return false
	}
	return true
}

// Expire 设置过期时间
func (db *Database) Expire(key string, milliseconds int64) bool {
	shard := db.getShard(key)
	shard.lock.Lock()
	defer shard.lock.Unlock()

	value, exists := shard.data[key]
	if !exists {
		return false
	}

	// 设置为绝对时间戳（当前时间 + TTL 毫秒）
	value.ExpireTime = time.Now().UnixMilli() + milliseconds
	return true
}

// Keys 返回所有键
func (db *Database) Keys() []string {
	keys := make([]string, 0)

	for i := 0; i < ShardCount; i++ {
		shard := &db.shards[i]
		shard.lock.RLock()
		for key, value := range shard.data {
			if value != nil && !value.IsExpired() {
				keys = append(keys, key)
			}
		}
		shard.lock.RUnlock()
	}
	return keys
}

// Size 返回数据库大小
func (db *Database) Size() int {
	count := 0
	for i := 0; i < ShardCount; i++ {
		shard := &db.shards[i]
		shard.lock.RLock()
		for _, value := range shard.data {
			if value != nil && !value.IsExpired() {
				count++
			}
		}
		shard.lock.RUnlock()
	}
	return count
}

// Clear 清空数据库
func (db *Database) Clear() error {
	for i := 0; i < ShardCount; i++ {
		shard := &db.shards[i]
		shard.lock.Lock()
		shard.data = nil
		shard.lock.Unlock()
	}
	atomic.StoreInt64(&db.usedMemory, 0)
	if db.keyHeat != nil {
		db.keyHeat.Clear()
	}

	if db.hasPersistence() {
		return db.persistenceReset()
	}
	return nil
}

// Close 关闭数据库
func (db *Database) Close() error {
	db.StopExpireCleaner()
	db.StopLSMDeleteWorker()
	// 这里不需要对所有 shards 加锁，因为 Close 意味着系统正在关闭
	// 但为了安全起见，我们还是可以加锁，或者直接关闭 LSM
	for i := 0; i < ShardCount; i++ {
		shard := &db.shards[i]
		shard.lock.Lock()
		shard.lock.Unlock()
	}

	if db.hasPersistence() {
		return db.persistenceClose()
	}
	return nil
}

// GetStats 获取数据库统计信息
func (db *Database) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})

	stats["db_id"] = db.id
	keyCount := 0
	for i := 0; i < ShardCount; i++ {
		shard := &db.shards[i]
		shard.lock.RLock()
		keyCount += len(shard.data)
		shard.lock.RUnlock()
	}
	stats["memory_keys"] = keyCount
	stats["used_memory_bytes"] = atomic.LoadInt64(&db.usedMemory)
	stats["max_memory_bytes"] = db.maxMemory
	stats["max_memory_policy"] = db.evictionPolicy
	stats["expire_runs"] = db.expireRuns.Load()
	stats["expire_scanned"] = db.expireScanned.Load()
	stats["expire_expired"] = db.expireExpired.Load()
	stats["expire_duration_ms"] = float64(db.expireDurationNanos.Load()) / 1e6
	stats["persistence_write_mode"] = db.persistenceWriteMode()
	stats["persistence_durability"] = db.persistenceDurability()
	stats["lsm_put_errors"] = db.lsmPutErrors.Load()
	stats["lsm_delete_errors"] = db.lsmDeleteErrors.Load()
	stats["lsm_delete_queue_len"] = len(db.lsmDeleteCh)
	stats["lsm_delete_queue_enqueued"] = db.lsmDeleteEnq.Load()
	stats["lsm_delete_queue_dropped"] = db.lsmDeleteDrop.Load()
	if v := db.lsmLastError.Load(); v != nil {
		stats["lsm_last_error"] = v.(string)
	} else {
		stats["lsm_last_error"] = ""
	}

	if db.hasPersistence() {
		stats["mode"] = db.persistenceMode()
		stats["lsm_enabled"] = true
		stats["lsm"] = db.persistenceStats()
	} else {
		stats["mode"] = "Memory"
		stats["lsm_enabled"] = false
	}

	return stats
}
