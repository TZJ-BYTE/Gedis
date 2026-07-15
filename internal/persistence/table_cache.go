package persistence

import (
	"container/list"
	"strconv"
	"sync"

	"golang.org/x/sync/singleflight"
)

// TableCache LRU Table Cache (File Descriptor Cache)
type TableCache struct {
	mu       sync.RWMutex
	cache    map[uint64]*list.Element // fileNum -> list element
	lruList  *list.List               // LRU 链表，头部是最最近使用的
	capacity int                      // 最大缓存数量（MaxOpenFiles）
	sf       singleflight.Group
}

type tableCacheItem struct {
	fileNum uint64
	reader  *SSTableReader
}

// NewTableCache 创建一个新的 Table Cache
func NewTableCache(capacity int) *TableCache {
	if capacity <= 0 {
		capacity = 100 // 默认值
	}
	return &TableCache{
		cache:    make(map[uint64]*list.Element),
		lruList:  list.New(),
		capacity: capacity,
	}
}

// GetOrOpen 获取或打开 SSTable Reader
func (c *TableCache) GetOrOpen(fileNum uint64, openFunc func() (*SSTableReader, error)) (*SSTableReader, error) {
	c.mu.Lock()
	if elem, ok := c.cache[fileNum]; ok {
		c.lruList.MoveToFront(elem)
		r := elem.Value.(*tableCacheItem).reader
		c.mu.Unlock()
		return r, nil
	}
	c.mu.Unlock()

	key := strconv.FormatUint(fileNum, 10)
	v, err, _ := c.sf.Do(key, func() (interface{}, error) {
		reader, err := openFunc()
		if err != nil {
			return nil, err
		}

		c.mu.Lock()
		defer c.mu.Unlock()

		if elem, ok := c.cache[fileNum]; ok {
			c.lruList.MoveToFront(elem)
			_ = reader.Close()
			return elem.Value.(*tableCacheItem).reader, nil
		}

		if c.lruList.Len() >= c.capacity {
			c.evictOldest()
		}

		item := &tableCacheItem{
			fileNum: fileNum,
			reader:  reader,
		}
		elem := c.lruList.PushFront(item)
		c.cache[fileNum] = elem

		return reader, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*SSTableReader), nil
}

// Add 添加 reader 到缓存
func (c *TableCache) Add(fileNum uint64, reader *SSTableReader) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果已存在，先关闭旧的
	if elem, ok := c.cache[fileNum]; ok {
		item := elem.Value.(*tableCacheItem)
		item.reader.Close()
		c.removeElement(elem)
	}

	// 如果已满，淘汰最久未使用的
	if c.lruList.Len() >= c.capacity {
		c.evictOldest()
	}

	item := &tableCacheItem{
		fileNum: fileNum,
		reader:  reader,
	}
	elem := c.lruList.PushFront(item)
	c.cache[fileNum] = elem
}

// Get 仅尝试获取，不打开
func (c *TableCache) Get(fileNum uint64) *SSTableReader {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.cache[fileNum]; ok {
		c.lruList.MoveToFront(elem)
		return elem.Value.(*tableCacheItem).reader
	}
	return nil
}

// Evict 驱逐指定文件
func (c *TableCache) Evict(fileNum uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.cache[fileNum]; ok {
		item := elem.Value.(*tableCacheItem)
		_ = item.reader.Close()
		c.removeElement(elem)
	}
}

// Close 关闭缓存，关闭所有 Reader
func (c *TableCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstErr error
	for c.lruList.Len() > 0 {
		elem := c.lruList.Back()
		item := elem.Value.(*tableCacheItem)
		err := item.reader.Close()
		if err != nil && firstErr == nil {
			firstErr = err
		}
		c.removeElement(elem)
	}
	return firstErr
}

// evictOldest 淘汰最久未使用的项
func (c *TableCache) evictOldest() {
	elem := c.lruList.Back()
	if elem != nil {
		item := elem.Value.(*tableCacheItem)
		// 关闭 Reader
		item.reader.Close()
		c.removeElement(elem)
	}
}

// removeElement 移除元素
func (c *TableCache) removeElement(elem *list.Element) {
	c.lruList.Remove(elem)
	item := elem.Value.(*tableCacheItem)
	delete(c.cache, item.fileNum)
}

// Len 返回当前缓存数量
func (c *TableCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lruList.Len()
}

func (c *TableCache) Capacity() int {
	return c.capacity
}

func (c *TableCache) BloomStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var checks uint64
	var negatives uint64
	readers := 0
	for e := c.lruList.Front(); e != nil; e = e.Next() {
		item := e.Value.(*tableCacheItem)
		if item.reader == nil {
			continue
		}
		ch, neg := item.reader.BloomStats()
		checks += ch
		negatives += neg
		readers++
	}

	return map[string]interface{}{
		"readers":        readers,
		"checks":         checks,
		"negatives":      negatives,
		"negative_ratio": ratioUint64(negatives, checks),
		"positive_ratio": ratioUint64(checks-negatives, checks),
	}
}

func ratioUint64(n uint64, d uint64) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
