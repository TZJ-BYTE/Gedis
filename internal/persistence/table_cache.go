package persistence

import (
	"container/list"
	"sync"
)

// TableCache LRU Table Cache (File Descriptor Cache)
type TableCache struct {
	mu       sync.RWMutex
	cache    map[uint64]*list.Element // fileNum -> list element
	lruList  *list.List               // LRU 链表，头部是最最近使用的
	capacity int                      // 最大缓存数量（MaxOpenFiles）
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
	defer c.mu.Unlock()

	// 1. 尝试从缓存获取
	if elem, ok := c.cache[fileNum]; ok {
		c.lruList.MoveToFront(elem)
		return elem.Value.(*tableCacheItem).reader, nil
	}

	// 2. 缓存未命中，执行打开操作
	// 注意：这里我们在持有锁的情况下执行 openFunc，这会阻塞其他 goroutine。
	// 在高并发场景下，可能需要优化（例如使用 singleflight）。
	// 但考虑到 openFunc 主要是文件打开操作，且我们希望严格控制文件描述符数量，
	// 持有锁是合理的，可以避免并发打开同一个文件或超出限制。

	reader, err := openFunc()
	if err != nil {
		return nil, err
	}

	// 3. 放入缓存
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

	return reader, nil
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
