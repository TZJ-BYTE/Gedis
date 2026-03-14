package persistence

import (
	"bytes"
	"sync"
	"sync/atomic"
)

// MemTable 内存表
// 基于跳表的有序键值存储，支持并发读写
type MemTable struct {
	skiplist   *SkipList
	size       int32 // 当前大小（字节）
	maxSize    int32 // 最大大小限制
	entryCount int64 // 条目数量
	lock       sync.RWMutex
}

// NewMemTable 创建新的 MemTable
func NewMemTable(maxSize int) *MemTable {
	return &MemTable{
		skiplist: NewSkipList(),
		maxSize:  int32(maxSize),
		size:     0,
	}
}

// Put 插入键值对
func (mt *MemTable) Put(key, value []byte) {
	mt.lock.Lock()
	defer mt.lock.Unlock()

	// 估算写入的大小
	keyLen := len(key)
	valueLen := len(value)
	estimatedSize := int32(keyLen + valueLen + 16) // 16 bytes overhead

	// 获取旧值用于更新 size
	oldValue, exists := mt.skiplist.Get(key)
	
	// 插入新值
	mt.skiplist.Insert(key, value)

	// 更新大小统计
	if exists {
		// 更新操作：减去旧值大小，加上新值大小
		oldSize := int32(len(key) + len(oldValue) + 16)
		atomic.AddInt32(&mt.size, estimatedSize-oldSize)
	} else {
		// 新增操作
		atomic.AddInt32(&mt.size, estimatedSize)
		atomic.AddInt64(&mt.entryCount, 1)
	}
}

// Get 获取指定 key 的值
func (mt *MemTable) Get(key []byte) ([]byte, bool) {
	mt.lock.RLock()
	defer mt.lock.RUnlock()
	return mt.skiplist.Get(key)
}

// Delete 删除指定 key
func (mt *MemTable) Delete(key []byte) {
	mt.lock.Lock()
	defer mt.lock.Unlock()

	value, exists := mt.skiplist.Get(key)
	if exists {
		mt.skiplist.Delete(key)
		// 更新大小统计
		removedSize := int32(len(key) + len(value) + 16)
		atomic.AddInt32(&mt.size, -removedSize)
		atomic.AddInt64(&mt.entryCount, -1)
	}
}

// Contains 检查 key 是否存在
func (mt *MemTable) Contains(key []byte) bool {
	mt.lock.RLock()
	defer mt.lock.RUnlock()
	_, exists := mt.skiplist.Get(key)
	return exists
}

// Size 返回当前大小（字节）
func (mt *MemTable) Size() int32 {
	return atomic.LoadInt32(&mt.size)
}

// EntryCount 返回条目数量
func (mt *MemTable) EntryCount() int64 {
	return atomic.LoadInt64(&mt.entryCount)
}

// IsFull 检查是否已满
func (mt *MemTable) IsFull() bool {
	return mt.Size() >= mt.maxSize
}

// ApproximateMemoryUsage 估算内存使用量
func (mt *MemTable) ApproximateMemoryUsage() int64 {
	mt.lock.RLock()
	defer mt.lock.RUnlock()
	return mt.skiplist.ApproximateMemoryUsage()
}

// Iterator 创建迭代器
func (mt *MemTable) Iterator() *SkipListIterator {
	mt.lock.RLock()
	defer mt.lock.RUnlock()
	return mt.skiplist.Iterator()
}

// Clear 清空 MemTable
func (mt *MemTable) Clear() {
	mt.lock.Lock()
	defer mt.lock.Unlock()
	
	mt.skiplist = NewSkipList()
	atomic.StoreInt32(&mt.size, 0)
	atomic.StoreInt64(&mt.entryCount, 0)
}

// NewIteratorFromKey 创建从指定 key 开始的迭代器
func (mt *MemTable) NewIteratorFromKey(key []byte) *SkipListIterator {
	mt.lock.RLock()
	defer mt.lock.RUnlock()
	
	iter := mt.skiplist.Iterator()
	iter.Seek(key)
	return iter
}

// Range 范围查询 [startKey, endKey)
func (mt *MemTable) Range(startKey, endKey []byte, limit int) [][]byte {
	mt.lock.RLock()
	defer mt.lock.RUnlock()

	var results [][]byte
	iter := mt.skiplist.Iterator()
	
	if !iter.Seek(startKey) {
		return results
	}

	for iter.Valid() && len(results) < limit {
		key := iter.Key()
		if bytes.Compare(key, endKey) >= 0 {
			break
		}
		
		// 复制数据，避免外部修改
		result := make([]byte, len(iter.Value()))
		copy(result, iter.Value())
		results = append(results, result)
		
		if !iter.Next() {
			break
		}
	}

	return results
}

// ForEach 遍历所有元素
func (mt *MemTable) ForEach(fn func(key, value []byte) error) error {
	mt.lock.RLock()
	defer mt.lock.RUnlock()

	iter := mt.skiplist.Iterator()
	for iter.Valid() {
		key := iter.Key()
		value := iter.Value()
		
		// 复制数据
		keyCopy := make([]byte, len(key))
		copy(keyCopy, key)
		valueCopy := make([]byte, len(value))
		copy(valueCopy, value)
		
		if err := fn(keyCopy, valueCopy); err != nil {
			return err
		}
		
		if !iter.Next() {
			break
		}
	}

	return nil
}

// ExportForFlush 导出用于刷写的数据
// 返回所有数据的快照，用于刷写到 SSTable
func (mt *MemTable) ExportForFlush() map[string][]byte {
	mt.lock.RLock()
	defer mt.lock.RUnlock()

	data := make(map[string][]byte, mt.EntryCount())
	iter := mt.skiplist.Iterator()
	
	for iter.Valid() {
		key := string(iter.Key())
		value := make([]byte, len(iter.Value()))
		copy(value, iter.Value())
		data[key] = value
		
		if !iter.Next() {
			break
		}
	}

	return data
}
