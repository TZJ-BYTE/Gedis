package persistence

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCompactor_CheckNeedCompaction(t *testing.T) {
	tmpDir := "/tmp/test_compact_need"
	defer os.RemoveAll(tmpDir)
	
	// 创建版本集合
	vs, err := OpenVersionSet(tmpDir, MaxLevels)
	if err != nil {
		t.Fatalf("Failed to open version set: %v", err)
	}
	defer vs.Close()
	
	// 创建 Compactor
	options := DefaultOptions()
	compactor := NewCompactor(tmpDir, vs, options)
	
	// 在 Level 0 添加多个文件
	for i := 0; i < Level0FileThreshold+1; i++ {
		fm := &FileMetadata{
			FileNum:     vs.GetNextFileNum(),
			Size:        1024,
			SmallestKey: []byte("key1"),
			LargestKey:  []byte("key10"),
			Level:       0,
		}
		err := vs.LogAddFile(fm)
		if err != nil {
			t.Fatalf("Failed to log add file: %v", err)
		}
	}
	
	// 检查是否需要 Compaction
	need, level := compactor.checkNeedCompaction()
	if !need {
		t.Error("Expected compaction needed")
	}
	if level != 0 {
		t.Errorf("Expected level 0, got %d", level)
	}
}

func TestMergeIterator_Basic(t *testing.T) {
	// 创建模拟迭代器
	iter1 := &mockIterator{
		data: []kv{{[]byte("a"), []byte("1")}, {[]byte("c"), []byte("3")}},
	}
	iter2 := &mockIterator{
		data: []kv{{[]byte("b"), []byte("2")}, {[]byte("d"), []byte("4")}},
	}
	
	// 创建归并迭代器
	mi := newMergeIterator([]Iterator{iter1, iter2})
	defer mi.Release()
	
	// 验证顺序
	expectedKeys := [][]byte{[]byte("a"), []byte("b"), []byte("c"), []byte("d")}
	expectedValues := [][]byte{[]byte("1"), []byte("2"), []byte("3"), []byte("4")}
	
	i := 0
	for mi.First(); mi.Valid(); mi.Next() {
		if i >= len(expectedKeys) {
			t.Fatal("Too many keys")
		}
		
		key := mi.Key()
		value := mi.Value()
		
		if string(key) != string(expectedKeys[i]) {
			t.Errorf("Expected key %s, got %s", expectedKeys[i], key)
		}
		if string(value) != string(expectedValues[i]) {
			t.Errorf("Expected value %s, got %s", expectedValues[i], value)
		}
		
		i++
	}
	
	if i != 4 {
		t.Errorf("Expected 4 keys, got %d", i)
	}
}

func TestCompaction_Integration(t *testing.T) {
	tmpDir := "/tmp/test_compact_integration"
	defer os.RemoveAll(tmpDir)
	
	// 创建 LSM Engine
	options := DefaultOptions()
	engine, err := OpenLSMEnergy(tmpDir, options)
	if err != nil {
		t.Fatalf("Failed to open engine: %v", err)
	}
	defer engine.Close()
	
	// 写入足够多的数据以触发 Compaction
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		err := engine.Put([]byte(key), []byte(value))
		if err != nil {
			t.Fatalf("Failed to put: %v", err)
		}
	}
	
	// 等待一小段时间让后台 Compaction 运行
	time.Sleep(100 * time.Millisecond)
	
	// 验证数据仍然可以读取
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)
		
		val, found := engine.Get([]byte(key))
		if !found {
			t.Errorf("Expected to find key %s", key)
			continue
		}
		if string(val) != expectedValue {
			t.Errorf("Expected value %s, got %s", expectedValue, val)
		}
	}
	
	// 检查 SSTable 目录
	sstableDir := filepath.Join(tmpDir, "sstable")
	files, err := os.ReadDir(sstableDir)
	if err != nil {
		t.Fatalf("Failed to read sstable dir: %v", err)
	}
	
	t.Logf("Created %d SSTable files", len(files))
	
	// 验证 Version Set 统计信息
	stats := engine.versionSet.GetStats()
	t.Logf("Version stats: %+v", stats)
}

// mockIterator 模拟迭代器用于测试
type kv struct {
	key   []byte
	value []byte
}

type mockIterator struct {
	data  []kv
	index int
}

func (mi *mockIterator) SeekToFirst() {
	mi.index = 0
}

func (mi *mockIterator) Valid() bool {
	return mi.index < len(mi.data)
}

func (mi *mockIterator) Key() []byte {
	if mi.index >= len(mi.data) {
		return nil
	}
	return mi.data[mi.index].key
}

func (mi *mockIterator) Value() []byte {
	if mi.index >= len(mi.data) {
		return nil
	}
	return mi.data[mi.index].value
}

func (mi *mockIterator) Next() bool {
	mi.index++
	return mi.index < len(mi.data)
}

func (mi *mockIterator) Prev() bool {
	if mi.index > 0 {
		mi.index--
		return true
	}
	return false
}

func (mi *mockIterator) Error() error {
	return nil
}

func (mi *mockIterator) Seek(key []byte) bool {
	for i, kv := range mi.data {
		if bytes.Compare(kv.key, key) >= 0 {
			mi.index = i
			return true
		}
	}
	mi.index = len(mi.data)
	return false
}

func (mi *mockIterator) First() bool {
	mi.index = 0
	return len(mi.data) > 0
}

func (mi *mockIterator) Last() bool {
	if len(mi.data) > 0 {
		mi.index = len(mi.data) - 1
		return true
	}
	return false
}

func (mi *mockIterator) Release() {
}
