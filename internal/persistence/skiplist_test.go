package persistence

import (
	"bytes"
	"fmt"
	"testing"
)

func TestSkipList_InsertAndGet(t *testing.T) {
	sl := NewSkipList()

	// 测试插入和获取
	key1 := []byte("key1")
	value1 := []byte("value1")
	sl.Insert(key1, value1)

	val, exists := sl.Get(key1)
	if !exists {
		t.Fatal("Expected to find key1")
	}
	if !bytes.Equal(val, value1) {
		t.Fatalf("Expected value1, got %s", val)
	}
}

func TestSkipList_Update(t *testing.T) {
	sl := NewSkipList()

	// 插入
	sl.Insert([]byte("key1"), []byte("value1"))
	
	// 更新
	sl.Insert([]byte("key1"), []byte("value2"))
	
	// 验证
	val, exists := sl.Get([]byte("key1"))
	if !exists {
		t.Fatal("Expected to find key1")
	}
	if !bytes.Equal(val, []byte("value2")) {
		t.Fatalf("Expected value2, got %s", val)
	}
	
	// 长度应该还是 1
	if sl.Length() != 1 {
		t.Fatalf("Expected length 1, got %d", sl.Length())
	}
}

func TestSkipList_Delete(t *testing.T) {
	sl := NewSkipList()

	// 插入多个 key
	for i := 0; i < 5; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		sl.Insert(key, value)
	}

	// 删除中间的 key
	deleted := sl.Delete([]byte("key2"))
	if !deleted {
		t.Fatal("Expected deletion to succeed")
	}

	// 验证删除成功
	_, exists := sl.Get([]byte("key2"))
	if exists {
		t.Fatal("Expected key2 to be deleted")
	}

	// 验证其他 key 还在
	for i := 0; i < 5; i++ {
		if i == 2 {
			continue
		}
		key := []byte(fmt.Sprintf("key%d", i))
		_, exists := sl.Get(key)
		if !exists {
			t.Fatalf("Expected key%d to exist", i)
		}
	}
}

func TestSkipList_Ordering(t *testing.T) {
	sl := NewSkipList()

	// 按逆序插入
	keys := []string{"key5", "key3", "key1", "key4", "key2"}
	for _, k := range keys {
		sl.Insert([]byte(k), []byte("value"))
	}

	// 验证迭代器返回的顺序是正确的
	iter := sl.Iterator()
	expectedOrder := []string{"key1", "key2", "key3", "key4", "key5"}
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

func TestSkipList_IteratorSeek(t *testing.T) {
	sl := NewSkipList()

	// 插入数据
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%02d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		sl.Insert(key, value)
	}

	iter := sl.Iterator()

	// 测试 Seek
	if !iter.Seek([]byte("key05")) {
		t.Fatal("Expected Seek to succeed")
	}

	key := string(iter.Key())
	if key != "key05" {
		t.Fatalf("Expected key05, got %s", key)
	}

	// Seek 不存在的 key
	if !iter.Seek([]byte("key05")) {
		t.Fatal("Expected Seek to succeed")
	}
}

func TestSkipList_Empty(t *testing.T) {
	sl := NewSkipList()

	// 空跳表的长度应为 0
	if sl.Length() != 0 {
		t.Fatalf("Expected length 0, got %d", sl.Length())
	}

	// 空跳表中获取应返回 false
	_, exists := sl.Get([]byte("nonexistent"))
	if exists {
		t.Fatal("Expected not to find key in empty skip list")
	}

	// 删除应返回 false
	deleted := sl.Delete([]byte("nonexistent"))
	if deleted {
		t.Fatal("Expected deletion to fail")
	}
}

func TestSkipList_MemoryUsage(t *testing.T) {
	sl := NewSkipList()

	// 初始内存使用应为 0 或很小
	initialUsage := sl.ApproximateMemoryUsage()

	// 插入一些数据
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		sl.Insert(key, value)
	}

	// 内存使用应该增加
	finalUsage := sl.ApproximateMemoryUsage()
	if finalUsage <= initialUsage {
		t.Fatal("Expected memory usage to increase")
	}
}

func TestSkipList_Concurrent(t *testing.T) {
	sl := NewSkipList()
	done := make(chan bool, 10)

	// 启动多个 goroutine 并发写入
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", id, j))
				value := []byte(fmt.Sprintf("value-%d-%d", id, j))
				sl.Insert(key, value)
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
	actualLength := sl.Length()
	if actualLength != expectedLength {
		t.Fatalf("Expected length %d, got %d", expectedLength, actualLength)
	}
}

func BenchmarkSkipList_Insert(b *testing.B) {
	sl := NewSkipList()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		sl.Insert(key, value)
	}
}

func BenchmarkSkipList_Get(b *testing.B) {
	sl := NewSkipList()

	// 预填充数据
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		sl.Insert(key, value)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i%10000))
		sl.Get(key)
	}
}
