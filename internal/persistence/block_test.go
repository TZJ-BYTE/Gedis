package persistence

import (
	"fmt"
	"testing"
)

func TestBlockBuilder_Basic(t *testing.T) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	
	// 添加一些数据
	builder.Add([]byte("key1"), []byte("value1"))
	builder.Add([]byte("key2"), []byte("value2"))
	builder.Add([]byte("key3"), []byte("value3"))
	
	// 完成构建
	data := builder.Finish()
	if len(data) == 0 {
		t.Fatal("Expected non-empty block data")
	}
	
	// 验证可以迭代
	iter := NewBlockIterator(data)
	
	expectedKeys := []string{"key1", "key2", "key3"}
	expectedValues := []string{"value1", "value2", "value3"}
	idx := 0
	
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		if idx >= len(expectedKeys) {
			t.Fatal("Too many entries")
		}
		
		key := string(iter.Key())
		value := string(iter.Value())
		
		if key != expectedKeys[idx] {
			t.Fatalf("Expected key %s, got %s", expectedKeys[idx], key)
		}
		
		if value != expectedValues[idx] {
			t.Fatalf("Expected value %s, got %s", expectedValues[idx], value)
		}
		
		idx++
	}
	
	if idx != len(expectedKeys) {
		t.Fatalf("Expected %d entries, got %d", len(expectedKeys), idx)
	}
}

func TestBlockBuilder_Empty(t *testing.T) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	
	if !builder.Empty() {
		t.Fatal("New builder should be empty")
	}
	
	data := builder.Finish()
	if data != nil {
		t.Fatal("Empty builder should return nil")
	}
}

func TestBlockBuilder_SizeEstimate(t *testing.T) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	
	initialSize := builder.CurrentSizeEstimate()
	
	builder.Add([]byte("key1"), []byte("value1"))
	
	if builder.CurrentSizeEstimate() <= initialSize {
		t.Fatal("Size should increase after adding entry")
	}
}

func TestBlockBuilder_PrefixCompression(t *testing.T) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	
	// 添加有共同前缀的 keys
	builder.Add([]byte("apple"), []byte("value1"))
	builder.Add([]byte("application"), []byte("value2"))
	builder.Add([]byte("apply"), []byte("value3"))
	
	data := builder.Finish()
	
	// 验证可以正确读取
	iter := NewBlockIterator(data)
	iter.SeekToFirst()
	
	expectedKeys := []string{"apple", "application", "apply"}
	idx := 0
	
	for iter.Valid() {
		key := string(iter.Key())
		if key != expectedKeys[idx] {
			t.Fatalf("Expected key %s, got %s", expectedKeys[idx], key)
		}
		idx++
		iter.Next()
	}
}

func TestBlockIterator_Seek(t *testing.T) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	
	// 添加有序数据
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%02d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		builder.Add(key, value)
	}
	
	data := builder.Finish()
	iter := NewBlockIterator(data)
	
	// Seek 到 key05
	if !iter.Seek([]byte("key05")) {
		t.Fatal("Seek should succeed")
	}
	
	key := string(iter.Key())
	if key != "key05" {
		t.Fatalf("Expected key05, got %s", key)
	}
	
	// Seek 不存在的 key
	if !iter.Seek([]byte("key07")) {
		t.Fatal("Seek should succeed")
	}
	
	key = string(iter.Key())
	if key != "key07" {
		t.Fatalf("Expected key07, got %s", key)
	}
}

func TestBlockIterator_SeekNotFound(t *testing.T) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	
	// 添加数据
	builder.Add([]byte("key1"), []byte("value1"))
	builder.Add([]byte("key3"), []byte("value3"))
	
	data := builder.Finish()
	iter := NewBlockIterator(data)
	
	// Seek 一个比所有 key 都大的值
	if iter.Seek([]byte("key99")) {
		t.Fatal("Seek should fail for non-existent key")
	}
}

func TestBlockBuilder_LargeData(t *testing.T) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	
	// 添加大量数据
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		builder.Add(key, value)
	}
	
	data := builder.Finish()
	
	// 验证条目数量
	iter := NewBlockIterator(data)
	count := 0
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		count++
	}
	
	if count != 1000 {
		t.Fatalf("Expected 1000 entries, got %d", count)
	}
}

func TestBlockBuilder_Ordering(t *testing.T) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	
	// 逆序添加
	keys := []string{"key5", "key3", "key1", "key4", "key2"}
	for _, k := range keys {
		builder.Add([]byte(k), []byte("value"))
	}
	
	data := builder.Finish()
	iter := NewBlockIterator(data)
	
	// Block 本身不保证排序，只保证按添加顺序存储
	// 验证迭代器能正确返回所有添加的条目（按添加顺序）
	expectedOrder := keys // 应该按添加顺序返回
	idx := 0
	
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		if idx >= len(expectedOrder) {
			t.Fatal("Too many entries")
		}
		key := string(iter.Key())
		if key != expectedOrder[idx] {
			t.Fatalf("Expected %s, got %s", expectedOrder[idx], key)
		}
		idx++
	}
	
	if idx != len(expectedOrder) {
		t.Fatalf("Expected %d entries, got %d", len(expectedOrder), idx)
	}
}

func BenchmarkBlockBuilder_Add(b *testing.B) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		builder.Add(key, value)
	}
}

func BenchmarkBlockIterator_Seek(b *testing.B) {
	opts := DefaultOptions()
	builder := NewBlockBuilder(opts)
	
	// 预填充数据
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		builder.Add(key, value)
	}
	
	data := builder.Finish()
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		iter := NewBlockIterator(data)
		iter.Seek([]byte(fmt.Sprintf("key%d", i%1000)))
	}
}
