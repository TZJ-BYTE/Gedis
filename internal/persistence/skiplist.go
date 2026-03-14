package persistence

import (
	"bytes"
	"math/rand"
	"sync"
	"time"
)

// 跳表最大层级
const MaxLevel = 12

// SkipList 跳表数据结构
// 线程安全的有序键值存储，用于 MemTable
type SkipList struct {
	header      *skipListNode
	tail        *skipListNode
	length      int
	level       int
	maxLevel    int
	probability float64
	comparator  func(a, b []byte) int
	randSource  *rand.Rand
	lock        sync.RWMutex
}

// skipListNode 跳表节点
type skipListNode struct {
	key     []byte
	value   []byte
	forward []*skipListNode // 前向指针数组
}

// NewSkipList 创建新的跳表
func NewSkipList() *SkipList {
	// 使用当前时间作为随机种子
	source := rand.NewSource(time.Now().UnixNano())
	r := rand.New(source)

	header := &skipListNode{
		key:     nil,
		value:   nil,
		forward: make([]*skipListNode, MaxLevel),
	}

	return &SkipList{
		header:      header,
		tail:        nil,
		length:      0,
		level:       1,
		maxLevel:    MaxLevel,
		probability: 0.25, // LevelDB 默认值
		comparator:  bytes.Compare,
		randSource:  r,
	}
}

// randomLevel 生成随机层级
func (sl *SkipList) randomLevel() int {
	lvl := 1
	for lvl < sl.maxLevel && sl.randSource.Float64() < sl.probability {
		lvl++
	}
	return lvl
}

// Insert 插入键值对
// 如果 key 已存在，则更新 value
func (sl *SkipList) Insert(key, value []byte) {
	sl.lock.Lock()
	defer sl.lock.Unlock()

	// 查找每个层级的最后一个小于 key 的节点
	update := make([]*skipListNode, sl.maxLevel)
	current := sl.header

	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && sl.comparator(current.forward[i].key, key) < 0 {
			current = current.forward[i]
		}
		update[i] = current
	}

	// 检查 key 是否已存在
	if current.forward[0] != nil && sl.comparator(current.forward[0].key, key) == 0 {
		// 更新已存在的 key
		current.forward[0].value = value
		return
	}

	// 生成随机层级
	newLevel := sl.randomLevel()

	// 如果需要扩展层级
	if newLevel > sl.level {
		for i := sl.level; i < newLevel; i++ {
			update[i] = sl.header
		}
		sl.level = newLevel
	}

	// 创建新节点
	newNode := &skipListNode{
		key:     key,
		value:   value,
		forward: make([]*skipListNode, newLevel),
	}

	// 插入节点
	for i := 0; i < newLevel; i++ {
		newNode.forward[i] = update[i].forward[i]
		update[i].forward[i] = newNode
	}

	sl.length++
}

// Get 获取指定 key 的值
func (sl *SkipList) Get(key []byte) ([]byte, bool) {
	sl.lock.RLock()
	defer sl.lock.RUnlock()

	current := sl.header

	// 从最高层开始查找
	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil {
			cmp := sl.comparator(current.forward[i].key, key)
			if cmp == 0 {
				// 找到了
				return current.forward[i].value, true
			} else if cmp > 0 {
				// 当前节点大于 key，停止在这一层
				break
			}
			// 继续向前
			current = current.forward[i]
		}
	}

	return nil, false
}

// Delete 删除指定 key
func (sl *SkipList) Delete(key []byte) bool {
	sl.lock.Lock()
	defer sl.lock.Unlock()

	update := make([]*skipListNode, sl.maxLevel)
	current := sl.header

	for i := sl.level - 1; i >= 0; i-- {
		for current.forward[i] != nil && sl.comparator(current.forward[i].key, key) < 0 {
			current = current.forward[i]
		}
		update[i] = current
	}

	// 检查是否找到
	if current.forward[0] == nil || sl.comparator(current.forward[0].key, key) != 0 {
		return false
	}

	// 删除节点
	nodeToDelete := current.forward[0]
	for i := 0; i < sl.level; i++ {
		if update[i].forward[i] != nodeToDelete {
			break
		}
		update[i].forward[i] = nodeToDelete.forward[i]
	}

	// 释放被删除节点的内存
	nodeToDelete.forward = nil

	// 更新层级
	for sl.level > 1 && sl.header.forward[sl.level-1] == nil {
		sl.level--
	}

	sl.length--
	return true
}

// Length 返回跳表长度
func (sl *SkipList) Length() int {
	sl.lock.RLock()
	defer sl.lock.RUnlock()
	return sl.length
}

// ApproximateMemoryUsage 估算内存使用量（字节）
func (sl *SkipList) ApproximateMemoryUsage() int64 {
	sl.lock.RLock()
	defer sl.lock.RUnlock()

	// 估算公式：
	// 每个节点：key + value + 指针数组
	// 指针数组大小：level * 8 bytes (64-bit pointer)
	var totalBytes int64
	
	current := sl.header.forward[0]
	for current != nil {
		// key 和 value 的大小
		totalBytes += int64(len(current.key) + len(current.value))
		// 节点结构本身和指针数组
		totalBytes += int64(48 + len(current.forward)*8)
		current = current.forward[0]
	}

	return totalBytes
}

// Iterator 创建迭代器
func (sl *SkipList) Iterator() *SkipListIterator {
	sl.lock.RLock()
	defer sl.lock.RUnlock()

	return &SkipListIterator{
		list:    sl,
		current: sl.header.forward[0],
	}
}

// SkipListIterator 跳表迭代器
type SkipListIterator struct {
	list    *SkipList
	current *skipListNode
}

// Next 移动到下一个元素
func (it *SkipListIterator) Next() bool {
	if it.current == nil {
		return false
	}
	it.current = it.current.forward[0]
	return it.current != nil
}

// Valid 检查当前元素是否有效
func (it *SkipListIterator) Valid() bool {
	return it.current != nil
}

// Key 获取当前 key
func (it *SkipListIterator) Key() []byte {
	if it.current == nil {
		return nil
	}
	return it.current.key
}

// Value 获取当前 value
func (it *SkipListIterator) Value() []byte {
	if it.current == nil {
		return nil
	}
	return it.current.value
}

// Release 释放迭代器
func (it *SkipListIterator) Release() {
	it.current = nil
}

// Seek 定位到第一个 >= key 的元素
func (it *SkipListIterator) Seek(key []byte) bool {
	it.list.lock.RLock()
	defer it.list.lock.RUnlock()

	current := it.list.header
	for i := it.list.level - 1; i >= 0; i-- {
		for current.forward[i] != nil {
			cmp := bytes.Compare(current.forward[i].key, key)
			if cmp >= 0 {
				break
			}
			current = current.forward[i]
		}
	}

	it.current = current.forward[0]
	return it.current != nil
}

// SeekToFirst 定位到第一个元素（符合 Iterator 接口契约）
func (it *SkipListIterator) SeekToFirst() bool {
	return it.First()
}

// First 定位到第一个元素
func (it *SkipListIterator) First() bool {
	it.list.lock.RLock()
	defer it.list.lock.RUnlock()

	it.current = it.list.header.forward[0]
	return it.current != nil
}

// Last 定位到最后一个元素（需要反向迭代支持）
func (it *SkipListIterator) Last() bool {
	// 简单实现：遍历到最后一个
	it.list.lock.RLock()
	defer it.list.lock.RUnlock()

	current := it.list.header.forward[0]
	if current == nil {
		it.current = nil
		return false
	}

	for current.forward[0] != nil {
		current = current.forward[0]
	}

	it.current = current
	return true
}

// Prev 移动到前一个元素（需要双向链表支持）
func (it *SkipListIterator) Prev() bool {
	// 简单实现：从头遍历到当前元素的前一个
	if it.current == nil {
		return false
	}

	it.list.lock.RLock()
	defer it.list.lock.RUnlock()

	current := it.list.header.forward[0]
	if current == nil || current == it.current {
		it.current = nil
		return false
	}

	for current.forward[0] != nil && current.forward[0] != it.current {
		current = current.forward[0]
	}

	it.current = current
	return true
}

// Error 获取错误信息（目前未使用）
func (it *SkipListIterator) Error() error {
	return nil
}
