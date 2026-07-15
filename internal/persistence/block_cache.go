package persistence

import (
	"container/list"
	"sync"
)

type CacheItem struct {
	key    uint64
	value  *Block
	size   int
	seg    uint8
	hits   uint32
	pinned bool
}

type BlockCache struct {
	mu          sync.RWMutex
	cache       map[uint64]*list.Element // offset -> list element
	probation   *list.List
	protected   *list.List
	maxSize     int64 // 最大缓存大小（字节）
	curSize     int64
	probSize    int64
	protSize    int64
	protMax     int64
	pinMinHits  uint32
	pinMaxItems int
	pinnedCount int
	hits        int64 // 命中次数
	misses      int64 // 未命中次数
	evictions   int64 // 淘汰次数
}

func NewBlockCache(maxSize int64) *BlockCache {
	return NewBlockCacheWithPolicy(maxSize, 0.8, 32)
}

func NewBlockCacheWithPolicy(maxSize int64, protectedRatio float64, pinMinHits uint32) *BlockCache {
	if protectedRatio <= 0 || protectedRatio >= 1 {
		protectedRatio = 0.8
	}
	if pinMinHits == 0 {
		pinMinHits = 32
	}
	protMax := int64(float64(maxSize) * protectedRatio)
	if protMax < 0 {
		protMax = 0
	}
	return &BlockCache{
		cache:       make(map[uint64]*list.Element),
		probation:   list.New(),
		protected:   list.New(),
		maxSize:     maxSize,
		protMax:     protMax,
		pinMinHits:  pinMinHits,
		pinMaxItems: 4096,
	}
}

func (c *BlockCache) Get(key uint64) (*Block, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.cache[key]
	if !ok {
		c.misses++
		return nil, false
	}

	item := elem.Value.(*CacheItem)
	item.hits++

	if item.seg == 0 {
		c.probation.Remove(elem)
		c.probSize -= int64(item.size)
		item.seg = 1
		elem = c.protected.PushFront(item)
		c.cache[key] = elem
		c.protSize += int64(item.size)
	} else {
		c.protected.MoveToFront(elem)
	}

	if !item.pinned && item.hits >= c.pinMinHits && c.pinnedCount < c.pinMaxItems {
		item.pinned = true
		c.pinnedCount++
	}

	c.rebalanceLocked()
	c.hits++

	return item.value, true
}

func (c *BlockCache) Put(key uint64, value *Block, size int) {
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
		if item.seg == 0 {
			c.probSize -= int64(oldSize)
			c.probSize += int64(size)
			c.probation.MoveToFront(elem)
		} else {
			c.protSize -= int64(oldSize)
			c.protSize += int64(size)
			c.protected.MoveToFront(elem)
		}
		c.curSize -= int64(oldSize)
		c.curSize += int64(size)
		item.hits++
		if !item.pinned && item.hits >= c.pinMinHits && c.pinnedCount < c.pinMaxItems {
			item.pinned = true
			c.pinnedCount++
		}
		c.rebalanceLocked()
		return
	}

	// 如果新项太大，直接拒绝
	if int64(size) > c.maxSize {
		return
	}

	if !c.ensureSpaceLocked(int64(size)) {
		return
	}

	item := &CacheItem{
		key:   key,
		value: value,
		size:  size,
		seg:   0,
		hits:  1,
	}
	elem := c.probation.PushFront(item)
	c.cache[key] = elem
	c.curSize += int64(size)
	c.probSize += int64(size)
	c.rebalanceLocked()
}

func (c *BlockCache) Delete(key uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.cache[key]
	if !ok {
		return
	}

	item := elem.Value.(*CacheItem)
	if item.seg == 0 {
		c.probation.Remove(elem)
		c.probSize -= int64(item.size)
	} else {
		c.protected.Remove(elem)
		c.protSize -= int64(item.size)
	}
	delete(c.cache, key)
	c.curSize -= int64(item.size)
	if item.pinned {
		c.pinnedCount--
	}
}

func (c *BlockCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[uint64]*list.Element)
	c.probation = list.New()
	c.protected = list.New()
	c.curSize = 0
	c.probSize = 0
	c.protSize = 0
	c.pinnedCount = 0
}

func (c *BlockCache) Size() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.curSize
}

func (c *BlockCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

func (c *BlockCache) MaxSize() int64 {
	return c.maxSize
}

func (c *BlockCache) HitRate() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	if total == 0 {
		return 0.0
	}
	return float64(c.hits) / float64(total)
}

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

func (c *BlockCache) SegmentStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"probation_bytes": c.probSize,
		"protected_bytes": c.protSize,
		"protected_max":   c.protMax,
		"probation_items": c.probation.Len(),
		"protected_items": c.protected.Len(),
		"pinned_items":    c.pinnedCount,
		"total_items":     len(c.cache),
		"total_bytes":     c.curSize,
		"max_bytes":       c.maxSize,
		"pin_min_hits":    c.pinMinHits,
		"pin_max_items":   c.pinMaxItems,
	}
}

func (c *BlockCache) Resize(newMaxSize int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.maxSize = newMaxSize
	c.protMax = int64(float64(newMaxSize) * 0.8)

	c.rebalanceLocked()
}

func (c *BlockCache) Keys() []uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]uint64, 0, len(c.cache))
	for key := range c.cache {
		keys = append(keys, key)
	}
	return keys
}

func (c *BlockCache) ensureSpaceLocked(need int64) bool {
	if need > c.maxSize {
		return false
	}
	for c.curSize+need > c.maxSize {
		if !c.evictOneLocked() {
			return false
		}
	}
	return true
}

func (c *BlockCache) rebalanceLocked() {
	for c.protSize > c.protMax {
		elem := c.protected.Back()
		if elem == nil {
			break
		}
		item := elem.Value.(*CacheItem)
		c.protected.Remove(elem)
		c.protSize -= int64(item.size)
		item.seg = 0
		newElem := c.probation.PushFront(item)
		c.cache[item.key] = newElem
		c.probSize += int64(item.size)
	}

	for c.curSize > c.maxSize {
		if !c.evictOneLocked() {
			break
		}
	}
}

func (c *BlockCache) evictOneLocked() bool {
	if c.probation.Len() > 0 {
		if c.evictFromListLocked(c.probation, 0) {
			return true
		}
	}
	if c.protected.Len() > 0 {
		if c.evictFromListLocked(c.protected, 1) {
			return true
		}
	}
	return false
}

func (c *BlockCache) evictFromListLocked(l *list.List, seg uint8) bool {
	for elem := l.Back(); elem != nil; elem = elem.Prev() {
		item := elem.Value.(*CacheItem)
		if item.pinned {
			continue
		}
		l.Remove(elem)
		delete(c.cache, item.key)
		c.curSize -= int64(item.size)
		if seg == 0 {
			c.probSize -= int64(item.size)
		} else {
			c.protSize -= int64(item.size)
		}
		c.evictions++
		return true
	}

	elem := l.Back()
	if elem == nil {
		return false
	}
	item := elem.Value.(*CacheItem)
	l.Remove(elem)
	delete(c.cache, item.key)
	c.curSize -= int64(item.size)
	if seg == 0 {
		c.probSize -= int64(item.size)
	} else {
		c.protSize -= int64(item.size)
	}
	if item.pinned {
		c.pinnedCount--
	}
	c.evictions++
	return true
}
