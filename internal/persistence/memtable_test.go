package persistence

import (
	"bytes"
	"fmt"
	"testing"
)

func TestMemTable_Basic(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024) // 4MB

	// 测试 Put 和 Get
	mt.Put([]byte("key1"), []byte("value1"))
	
	val, exists := mt.Get([]byte("key1"))
	if !exists {
		t.Fatal("Expected to find key1")
	}
	if !bytes.Equal(val, []byte("value1")) {
		t.Fatalf("Expected value1, got %s", val)
	}
}

func TestMemTable_Update(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)

	// 插入
	mt.Put([]byte("key1"), []byte("value1"))
	
	// 更新
	mt.Put([]byte("key1"), []byte("value2"))
	
	// 验证
	val, exists := mt.Get([]byte("key1"))
	if !exists {
		t.Fatal("Expected to find key1")
	}
	if !bytes.Equal(val, []byte("value2")) {
		t.Fatalf("Expected value2, got %s", val)
	}
	
	// EntryCount 应该还是 1
	if mt.EntryCount() != 1 {
		t.Fatalf("Expected EntryCount 1, got %d", mt.EntryCount())
	}
}

func TestMemTable_Delete(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)

	// 插入多个 key
	for i := 0; i < 5; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		mt.Put(key, value)
	}

	// 删除
	mt.Delete([]byte("key2"))

	// 验证删除成功
	_, exists := mt.Get([]byte("key2"))
	if exists {
		t.Fatal("Expected key2 to be deleted")
	}

	// 验证 EntryCount
	if mt.EntryCount() != 4 {
		t.Fatalf("Expected EntryCount 4, got %d", mt.EntryCount())
	}
}

func TestMemTable_Size(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)

	initialSize := mt.Size()

	// 插入一些数据
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		mt.Put(key, value)
	}

	// Size 应该增加
	middleSize := mt.Size()
	if middleSize <= initialSize {
		t.Fatal("Expected size to increase")
	}

	// 删除一些数据
	for i := 0; i < 50; i++ {
		mt.Delete([]byte(fmt.Sprintf("key%d", i)))
	}

	// Size 应该减少
	finalSize := mt.Size()
	if finalSize >= middleSize {
		t.Fatalf("Expected size to decrease after deletion, before: %d, after: %d", middleSize, finalSize)
	}
}

func TestMemTable_IsFull(t *testing.T) {
	// 创建一个很小的 MemTable
	mt := NewMemTable(100)

	if mt.IsFull() {
		t.Fatal("New MemTable should not be full")
	}

	// 填满它
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("k%d", i))
		value := []byte(fmt.Sprintf("v%d", i))
		mt.Put(key, value)
		
		if mt.IsFull() {
			break
		}
	}

	if !mt.IsFull() {
		t.Fatal("Expected MemTable to be full")
	}
}

func TestMemTable_Iterator(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)

	// 插入数据
	keys := []string{"key3", "key1", "key2"}
	for _, k := range keys {
		mt.Put([]byte(k), []byte("value"))
	}

	// 验证迭代器顺序
	iter := mt.Iterator()
	expectedOrder := []string{"key1", "key2", "key3"}
	idx := 0

	for iter.First(); iter.Valid(); iter.Next() {
		if idx >= len(expectedOrder) {
			t.Fatal("Iterator returned too many elements")
		}
		key := string(iter.Key())
		if key != expectedOrder[idx] {
			t.Fatalf("Expected %s, got %s", expectedOrder[idx], key)
		}
		idx++
	}

	if idx != len(expectedOrder) {
		t.Fatalf("Expected %d elements, got %d", len(expectedOrder), idx)
	}
}

func TestMemTable_Range(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)

	// 插入有序数据
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%02d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		mt.Put(key, value)
	}

	// 范围查询
	results := mt.Range([]byte("key03"), []byte("key07"), 10)
	
	if len(results) != 4 { // key03, key04, key05, key06
		t.Fatalf("Expected 4 results, got %d", len(results))
	}
}

func TestMemTable_ForEach(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)

	// 插入数据
	for i := 0; i < 5; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		mt.Put(key, value)
	}

	count := 0
	err := mt.ForEach(func(key, value []byte) error {
		count++
		return nil
	})

	if err != nil {
		t.Fatalf("ForEach failed: %v", err)
	}

	if count != 5 {
		t.Fatalf("Expected 5 iterations, got %d", count)
	}
}

func TestMemTable_ExportForFlush(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)

	// 插入数据
	data := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for k, v := range data {
		mt.Put([]byte(k), []byte(v))
	}

	// 导出
	exported := mt.ExportForFlush()

	if len(exported) != len(data) {
		t.Fatalf("Expected %d entries, got %d", len(data), len(exported))
	}

	for k, v := range data {
		exportedVal, exists := exported[k]
		if !exists {
			t.Fatalf("Expected %s in exported data", k)
		}
		if string(exportedVal) != v {
			t.Fatalf("Expected value %s for key %s, got %s", v, k, exportedVal)
		}
	}
}

func TestMemTable_Clear(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)

	// 插入数据
	for i := 0; i < 100; i++ {
		mt.Put([]byte(fmt.Sprintf("key%d", i)), []byte("value"))
	}

	if mt.EntryCount() == 0 {
		t.Fatal("Expected non-zero entry count")
	}

	// 清空
	mt.Clear()

	if mt.EntryCount() != 0 {
		t.Fatalf("Expected 0 entries after clear, got %d", mt.EntryCount())
	}

	if mt.Size() != 0 {
		t.Fatalf("Expected 0 size after clear, got %d", mt.Size())
	}
}

func TestMemTable_Concurrent(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)
	done := make(chan bool, 10)

	// 启动多个 goroutine 并发写入
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", id, j))
				value := []byte(fmt.Sprintf("value-%d-%d", id, j))
				mt.Put(key, value)
			}
			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}

	// 验证总长度
	expectedLength := 10 * 100
	actualLength := mt.EntryCount()
	if actualLength != int64(expectedLength) {
		t.Fatalf("Expected length %d, got %d", expectedLength, actualLength)
	}
}

func BenchmarkMemTable_Put(b *testing.B) {
	mt := NewMemTable(4 * 1024 * 1024)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		mt.Put(key, value)
	}
}

func BenchmarkMemTable_Get(b *testing.B) {
	mt := NewMemTable(4 * 1024 * 1024)

	// 预填充数据
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		mt.Put(key, value)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i%10000))
		mt.Get(key)
	}
}
