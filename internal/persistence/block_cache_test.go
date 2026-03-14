package persistence

import (
	"bytes"
	"fmt"
	"testing"
)

func TestBlockCache_Basic(t *testing.T) {
	cache := NewBlockCache(1024) // 1KB max size

	// 测试 Put 和 Get
	key := uint64(1)
	value := []byte("test_value")
	size := len(value)

	cache.Put(key, value, size)

	// 验证可以获取
	val, ok := cache.Get(key)
	if !ok {
		t.Fatal("Expected to find key in cache")
	}

	if !bytes.Equal(val.([]byte), value) {
		t.Errorf("Expected %s, got %s", string(value), string(val.([]byte)))
	}

	// 验证大小
	if cache.Size() != int64(size) {
		t.Errorf("Expected size %d, got %d", size, cache.Size())
	}

	// 验证长度
	if cache.Len() != 1 {
		t.Errorf("Expected length 1, got %d", cache.Len())
	}
}

func TestBlockCache_LRU(t *testing.T) {
	cache := NewBlockCache(100) // 小缓存

	// 添加多个条目，超出缓存容量
	for i := 0; i < 10; i++ {
		key := uint64(i)
		value := []byte(fmt.Sprintf("value_%d", i))
		cache.Put(key, value, len(value))
	}

	// 验证只有最近的几个条目在缓存中
	// LRU 应该淘汰了旧的条目
	t.Logf("Cache size: %d, Len: %d", cache.Size(), cache.Len())

	// 最近添加的应该在缓存中
	for i := 5; i < 10; i++ {
		_, ok := cache.Get(uint64(i))
		if !ok {
			t.Errorf("Expected key %d to be in cache", i)
		}
	}

	// 旧的条目可能被淘汰
	hits := 0
	for i := 0; i < 5; i++ {
		_, ok := cache.Get(uint64(i))
		if ok {
			hits++
		}
	}

	t.Logf("Hits in old keys: %d/5", hits)
}

func TestBlockCache_Update(t *testing.T) {
	cache := NewBlockCache(1024)

	key := uint64(1)
	value1 := []byte("value1")
	value2 := []byte("value2_updated")

	// 第一次 Put
	cache.Put(key, value1, len(value1))

	// 更新
	cache.Put(key, value2, len(value2))

	// 验证更新后的值
	val, ok := cache.Get(key)
	if !ok {
		t.Fatal("Expected to find key in cache")
	}

	if !bytes.Equal(val.([]byte), value2) {
		t.Errorf("Expected %s, got %s", string(value2), string(val.([]byte)))
	}

	// 验证大小正确更新
	expectedSize := int64(len(value2))
	if cache.Size() != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, cache.Size())
	}
}

func TestBlockCache_Delete(t *testing.T) {
	cache := NewBlockCache(1024)

	key := uint64(1)
	value := []byte("test_value")

	cache.Put(key, value, len(value))

	// 验证存在
	if _, ok := cache.Get(key); !ok {
		t.Fatal("Expected to find key before delete")
	}

	// 删除
	cache.Delete(key)

	// 验证不存在
	if _, ok := cache.Get(key); ok {
		t.Error("Expected key to be deleted")
	}

	// 验证大小
	if cache.Size() != 0 {
		t.Errorf("Expected size 0 after delete, got %d", cache.Size())
	}
}

func TestBlockCache_Clear(t *testing.T) {
	cache := NewBlockCache(1024)

	// 添加一些数据
	for i := 0; i < 10; i++ {
		key := uint64(i)
		value := []byte(fmt.Sprintf("value%d", i))
		cache.Put(key, value, len(value))
	}

	// 验证有数据
	if cache.Len() == 0 {
		t.Fatal("Expected non-zero length before clear")
	}

	// 清空
	cache.Clear()

	// 验证清空后状态
	if cache.Len() != 0 {
		t.Errorf("Expected length 0 after clear, got %d", cache.Len())
	}

	if cache.Size() != 0 {
		t.Errorf("Expected size 0 after clear, got %d", cache.Size())
	}
}

func TestBlockCache_HitRate(t *testing.T) {
	cache := NewBlockCache(1024)

	// 初始命中率应该为 0
	if rate := cache.HitRate(); rate != 0.0 {
		t.Errorf("Expected initial hit rate 0, got %.2f", rate)
	}

	key := uint64(1)
	value := []byte("test_value")

	// Miss
	cache.Get(key)

	// Put
	cache.Put(key, value, len(value))

	// Hit
	cache.Get(key)
	cache.Get(key)

	hits, misses, _, hitRate := cache.Stats()

	t.Logf("Hits: %d, Misses: %d, Hit Rate: %.2f%%", 
		hits, misses, hitRate*100)

	if hits != 2 {
		t.Errorf("Expected 2 hits, got %d", hits)
	}

	if misses != 1 {
		t.Errorf("Expected 1 miss, got %d", misses)
	}

	expectedRate := 2.0 / 3.0
	if hitRate < expectedRate-0.01 || hitRate > expectedRate+0.01 {
		t.Errorf("Expected hit rate ~%.2f, got %.2f", expectedRate, hitRate)
	}
}

func TestBlockCache_Resize(t *testing.T) {
	cache := NewBlockCache(1024)

	// 添加一些数据
	for i := 0; i < 10; i++ {
		key := uint64(i)
		value := []byte(fmt.Sprintf("value_%d", i))
		cache.Put(key, value, len(value))
	}

	initialLen := cache.Len()
	t.Logf("Initial cache: %d items, %d bytes", cache.Len(), cache.Size())

	// 缩小缓存大小
	cache.Resize(50)

	// 验证有元素被淘汰
	finalLen := cache.Len()
	t.Logf("After resize: %d items, %d bytes", finalLen, cache.Size())

	if finalLen >= initialLen {
		t.Error("Expected some items to be evicted after resize")
	}

	// 验证不超过新的大小限制
	if cache.Size() > 50 {
		t.Errorf("Expected size <= 50, got %d", cache.Size())
	}
}

func TestBlockCache_Eviction(t *testing.T) {
	cache := NewBlockCache(100) // 很小的缓存

	// 持续添加数据，触发淘汰
	for i := 0; i < 100; i++ {
		key := uint64(i)
		value := []byte(fmt.Sprintf("value_%d_with_some_length", i))
		cache.Put(key, value, len(value))
	}

	// 验证缓存大小不超过限制
	if cache.Size() > cache.MaxSize() {
		t.Errorf("Cache size exceeded max: %d > %d", cache.Size(), cache.MaxSize())
	}

	// 获取统计信息
	hits, misses, evictions, hitRate := cache.Stats()
	t.Logf("Stats - Hits: %d, Misses: %d, Evictions: %d, Hit Rate: %.2f%%",
		hits, misses, evictions, hitRate*100)

	// 应该有淘汰发生
	if evictions == 0 {
		t.Error("Expected some evictions")
	}
}

func BenchmarkBlockCache_Put(b *testing.B) {
	cache := NewBlockCache(1024 * 1024) // 1MB
	value := []byte("test_value_for_benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := uint64(i)
		cache.Put(key, value, len(value))
	}
}

func BenchmarkBlockCache_Get(b *testing.B) {
	cache := NewBlockCache(1024 * 1024)
	key := uint64(1)
	value := []byte("test_value_for_benchmark")

	// 先放入缓存
	cache.Put(key, value, len(value))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(key)
	}
}
