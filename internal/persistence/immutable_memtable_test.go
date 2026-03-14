package persistence

import (
	"bytes"
	"fmt"
	"testing"
)

func TestImmutableMemTable_Basic(t *testing.T) {
	// 创建 Mutable MemTable
	mt := NewMemTable(4 * 1024 * 1024)
	mt.Put([]byte("key1"), []byte("value1"))
	mt.Put([]byte("key2"), []byte("value2"))

	// 转为 Immutable
	imt := NewImmutableMemTable(mt)

	// 验证可以读取
	val, exists := imt.Get([]byte("key1"))
	if !exists {
		t.Fatal("Expected to find key1")
	}
	if !bytes.Equal(val, []byte("value1")) {
		t.Fatalf("Expected value1, got %s", val)
	}
}

func TestImmutableMemTable_RefCount(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)
	mt.Put([]byte("key1"), []byte("value1"))

	imt := NewImmutableMemTable(mt)

	// 初始引用计数应为 1
	if imt.refCount != 1 {
		t.Fatalf("Expected refCount 1, got %d", imt.refCount)
	}

	// 增加引用
	imt.Ref()
	if imt.refCount != 2 {
		t.Fatalf("Expected refCount 2 after Ref(), got %d", imt.refCount)
	}

	// 减少引用
	imt.Unref()
	if imt.refCount != 1 {
		t.Fatalf("Expected refCount 1 after Unref(), got %d", imt.refCount)
	}
}

func TestImmutableMemTable_Close(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)
	mt.Put([]byte("key1"), []byte("value1"))

	imt := NewImmutableMemTable(mt)

	// 关闭
	imt.Close()

	// 验证已关闭
	if !imt.IsClosed() {
		t.Fatal("Expected ImmutableMemTable to be closed")
	}

	// 关闭后不应再能读取
	_, exists := imt.Get([]byte("key1"))
	if exists {
		t.Fatal("Expected not to find key after close")
	}
}

func TestImmutableMemTable_UnrefAutoClose(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)
	mt.Put([]byte("key1"), []byte("value1"))

	imt := NewImmutableMemTable(mt)

	// Unref 到 0 应该自动关闭
	imt.Unref()

	if !imt.IsClosed() {
		t.Fatal("Expected auto-close when refCount reaches 0")
	}
}

func TestImmutableMemTable_Size(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)
	
	for i := 0; i < 100; i++ {
		mt.Put([]byte(fmt.Sprintf("key%d", i)), []byte("value"))
	}

	imt := NewImmutableMemTable(mt)

	if imt.Size() == 0 {
		t.Fatal("Expected non-zero size")
	}

	if imt.EntryCount() != 100 {
		t.Fatalf("Expected EntryCount 100, got %d", imt.EntryCount())
	}
}

func TestImmutableMemTable_Iterator(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)
	
	keys := []string{"key3", "key1", "key2"}
	for _, k := range keys {
		mt.Put([]byte(k), []byte("value"))
	}

	imt := NewImmutableMemTable(mt)

	iter := imt.Iterator()
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
}

func TestImmutableMemTable_Range(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)
	
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%02d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		mt.Put(key, value)
	}

	imt := NewImmutableMemTable(mt)

	results := imt.Range([]byte("key03"), []byte("key07"), 10)
	
	if len(results) != 4 {
		t.Fatalf("Expected 4 results, got %d", len(results))
	}
}

func TestImmutableMemTable_ForEach(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)
	
	for i := 0; i < 5; i++ {
		mt.Put([]byte(fmt.Sprintf("key%d", i)), []byte("value"))
	}

	imt := NewImmutableMemTable(mt)

	count := 0
	err := imt.ForEach(func(key, value []byte) error {
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

func TestImmutableMemTable_ExportForFlush(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)

	data := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for k, v := range data {
		mt.Put([]byte(k), []byte(v))
	}

	imt := NewImmutableMemTable(mt)

	exported := imt.ExportForFlush()

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

func TestImmutableMemTable_ClosedOperations(t *testing.T) {
	mt := NewMemTable(4 * 1024 * 1024)
	mt.Put([]byte("key1"), []byte("value1"))

	imt := NewImmutableMemTable(mt)
	imt.Close()

	// 所有操作在关闭后应该安全返回
	_, exists := imt.Get([]byte("key1"))
	if exists {
		t.Fatal("Get should return false after close")
	}

	if imt.Contains([]byte("key1")) {
		t.Fatal("Contains should return false after close")
	}

	if imt.Iterator() != nil {
		t.Fatal("Iterator should return nil after close")
	}

	if imt.ExportForFlush() != nil {
		t.Fatal("ExportForFlush should return nil after close")
	}
}
