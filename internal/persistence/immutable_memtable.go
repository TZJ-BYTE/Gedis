package persistence

import (
	"sync/atomic"
)

// ImmutableMemTable 不可变内存表
// 用于异步刷写到磁盘的只读 MemTable
type ImmutableMemTable struct {
	memtable *MemTable
	refCount int32 // 引用计数
	closed   int32 // 是否已关闭
}

// NewImmutableMemTable 创建不可变内存表
func NewImmutableMemTable(mt *MemTable) *ImmutableMemTable {
	return &ImmutableMemTable{
		memtable: mt,
		refCount: 1, // 初始引用为 1
		closed:   0,
	}
}

// Get 获取指定 key 的值（只读）
func (imt *ImmutableMemTable) Get(key []byte) ([]byte, bool) {
	if atomic.LoadInt32(&imt.closed) == 1 {
		return nil, false
	}
	return imt.memtable.Get(key)
}

// Contains 检查 key 是否存在
func (imt *ImmutableMemTable) Contains(key []byte) bool {
	if atomic.LoadInt32(&imt.closed) == 1 {
		return false
	}
	return imt.memtable.Contains(key)
}

// Size 返回大小（字节）
func (imt *ImmutableMemTable) Size() int32 {
	return imt.memtable.Size()
}

// EntryCount 返回条目数量
func (imt *ImmutableMemTable) EntryCount() int64 {
	return imt.memtable.EntryCount()
}

// Iterator 创建迭代器
func (imt *ImmutableMemTable) Iterator() *SkipListIterator {
	if atomic.LoadInt32(&imt.closed) == 1 {
		return nil
	}
	return imt.memtable.Iterator()
}

// NewIteratorFromKey 创建从指定 key 开始的迭代器
func (imt *ImmutableMemTable) NewIteratorFromKey(key []byte) *SkipListIterator {
	if atomic.LoadInt32(&imt.closed) == 1 {
		return nil
	}
	return imt.memtable.NewIteratorFromKey(key)
}

// ApproximateMemoryUsage 估算内存使用量
func (imt *ImmutableMemTable) ApproximateMemoryUsage() int64 {
	return imt.memtable.ApproximateMemoryUsage()
}

// Ref 增加引用计数
func (imt *ImmutableMemTable) Ref() {
	atomic.AddInt32(&imt.refCount, 1)
}

// Unref 减少引用计数，当计数为 0 时释放资源
func (imt *ImmutableMemTable) Unref() {
	if atomic.AddInt32(&imt.refCount, -1) == 0 {
		imt.Close()
	}
}

// Close 关闭并释放资源
func (imt *ImmutableMemTable) Close() {
	if atomic.CompareAndSwapInt32(&imt.closed, 0, 1) {
		// 清空底层 MemTable 以释放内存
		imt.memtable.Clear()
	}
}

// IsClosed 检查是否已关闭
func (imt *ImmutableMemTable) IsClosed() bool {
	return atomic.LoadInt32(&imt.closed) == 1
}

// ExportForFlush 导出用于刷写的数据
func (imt *ImmutableMemTable) ExportForFlush() map[string][]byte {
	if atomic.LoadInt32(&imt.closed) == 1 {
		return nil
	}
	return imt.memtable.ExportForFlush()
}

// Range 范围查询
func (imt *ImmutableMemTable) Range(startKey, endKey []byte, limit int) [][]byte {
	if atomic.LoadInt32(&imt.closed) == 1 {
		return nil
	}
	return imt.memtable.Range(startKey, endKey, limit)
}

// ForEach 遍历所有元素
func (imt *ImmutableMemTable) ForEach(fn func(key, value []byte) error) error {
	if atomic.LoadInt32(&imt.closed) == 1 {
		return nil
	}
	return imt.memtable.ForEach(fn)
}
