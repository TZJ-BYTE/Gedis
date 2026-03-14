package persistence

import (
	"container/list"
	"sync"
)

// CacheItem 缓存项
type CacheItem struct {
	key   uint64      // Block offset 作为 key
	value interface{} // Block 数据
	size  int         // 数据大小
}

// BlockCache LRU Block Cache
type BlockCache struct {
	mu        sync.RWMutex
	cache     map[uint64]*list.Element // offset -> list element
	lruList   *list.List               // LRU 链表，头部是最最近使用的
	maxSize   int64                    // 最大缓存大小（字节）
	curSize   int64                    // 当前缓存大小（字节）
	hits      int64                    // 命中次数
	misses    int64                    // 未命中次数
	evictions int64                    // 淘汰次数
}

// NewBlockCache 创建一个新的 Block Cache
func NewBlockCache(maxSize int64) *BlockCache {
	return &BlockCache{
		cache:   make(map[uint64]*list.Element),
		lruList: list.New(),
		maxSize: maxSize,
	}
}

// Get 从缓存中获取数据
func (c *BlockCache) Get(key uint64) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.cache[key]
	if !ok {
		c.misses++
		return nil, false
	}

	// 移动到链表头部（标记为最近使用）
	c.lruList.MoveToFront(elem)
	c.hits++

	item := elem.Value.(*CacheItem)
	return item.value, true
}

// Put 将数据放入缓存
func (c *BlockCache) Put(key uint64, value interface{}, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查是否已存在
	if elem, ok := c.cache[key]; ok {
		// 更新现有项
		item := elem.Value.(*CacheItem)
		oldSize := item.size
		item.value = value
		item.size = size

		// 更新大小
		c.curSize -= int64(oldSize)
		c.curSize += int64(size)

		// 移动到链表头部
		c.lruList.MoveToFront(elem)
		return
	}

	// 如果新项太大，直接拒绝
	if int64(size) > c.maxSize {
		return
	}

	// 如果需要淘汰，释放空间
	for c.curSize+int64(size) > c.maxSize && c.lruList.Len() > 0 {
		c.evictOldest()
	}

	// 添加新项到链表头部
	item := &CacheItem{
		key:   key,
		value: value,
		size:  size,
	}
	elem := c.lruList.PushFront(item)
	c.cache[key] = elem
	c.curSize += int64(size)
}

// Delete 从缓存中删除数据
func (c *BlockCache) Delete(key uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.cache[key]
	if !ok {
		return
	}

	item := elem.Value.(*CacheItem)
	c.lruList.Remove(elem)
	delete(c.cache, key)
	c.curSize -= int64(item.size)
}

// Clear 清空缓存
func (c *BlockCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[uint64]*list.Element)
	c.lruList = list.New()
	c.curSize = 0
}

// Size 获取当前缓存大小（字节）
func (c *BlockCache) Size() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.curSize
}

// Len 获取缓存项数量
func (c *BlockCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lruList.Len()
}

// MaxSize 获取最大缓存大小
func (c *BlockCache) MaxSize() int64 {
	return c.maxSize
}

// HitRate 获取命中率
func (c *BlockCache) HitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	if total == 0 {
		return 0.0
	}
	return float64(c.hits) / float64(total)
}

// Stats 获取缓存统计信息
func (c *BlockCache) Stats() (hits, misses, evictions int64, hitRate float64) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	if total == 0 {
		hitRate = 0.0
	} else {
		hitRate = float64(c.hits) / float64(total)
	}

	return c.hits, c.misses, c.evictions, hitRate
}

// evictOldest 淘汰最久未使用的项（必须在持有锁的情况下调用）
func (c *BlockCache) evictOldest() {
	if c.lruList.Len() == 0 {
		return
	}

	// 获取链表尾部元素（最久未使用）
	elem := c.lruList.Back()
	if elem == nil {
		return
	}

	item := elem.Value.(*CacheItem)

	// 从链表和映射中删除
	c.lruList.Remove(elem)
	delete(c.cache, item.key)
	c.curSize -= int64(item.size)
	c.evictions++
}

// Resize 调整缓存大小
func (c *BlockCache) Resize(newMaxSize int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.maxSize = newMaxSize

	// 如果新大小小于当前大小，需要淘汰一些项
	for c.curSize > c.maxSize && c.lruList.Len() > 0 {
		c.evictOldest()
	}
}

// Keys 获取所有缓存的 key（用于调试）
func (c *BlockCache) Keys() []uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]uint64, 0, len(c.cache))
	for key := range c.cache {
		keys = append(keys, key)
	}
	return keys
}
