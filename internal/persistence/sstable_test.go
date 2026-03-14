package persistence

import (
	"bytes"
	"fmt"
	"os"
	"testing"
)

func TestSSTableBuilder_Basic(t *testing.T) {
	// 创建临时文件
	tmpFile := "/tmp/test_sstable_basic.sst"
	defer os.Remove(tmpFile)
	
	opts := DefaultOptions()
	builder, err := NewSSTableBuilder(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	
	// 添加数据
	err = builder.Add([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Failed to add: %v", err)
	}
	
	err = builder.Add([]byte("key2"), []byte("value2"))
	if err != nil {
		t.Fatalf("Failed to add: %v", err)
	}
	
	err = builder.Add([]byte("key3"), []byte("value3"))
	if err != nil {
		t.Fatalf("Failed to add: %v", err)
	}
	
	// 完成构建
	err = builder.Finish()
	if err != nil {
		t.Fatalf("Failed to finish: %v", err)
	}
	
	// 验证文件大小
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	
	if info.Size() == 0 {
		t.Fatal("Expected non-zero file size")
	}
	
	// 验证可以读取
	reader, err := OpenSSTableForRead(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to open for read: %v", err)
	}
	defer reader.Close()
	
	
	// 测试 Get
	
	val, exists := reader.Get([]byte("key1"))
	if !exists {
		t.Fatal("Expected to find key1")
	}
	if !bytes.Equal(val, []byte("value1")) {
		t.Fatalf("Expected value1, got %s", val)
	}
	
	val, exists = reader.Get([]byte("key2"))
	if !exists {
		t.Fatal("Expected to find key2")
	}
	if !bytes.Equal(val, []byte("value2")) {
		t.Fatalf("Expected value2, got %s", val)
	}
}

func TestSSTableBuilder_ManyEntries(t *testing.T) {
	tmpFile := "/tmp/test_sstable_many.sst"
	defer os.Remove(tmpFile)
	
	opts := DefaultOptions()
	builder, err := NewSSTableBuilder(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	
	// 添加大量数据
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		err = builder.Add(key, value)
		if err != nil {
			t.Fatalf("Failed to add key%d: %v", i, err)
		}
	}
	
	err = builder.Finish()
	if err != nil {
		t.Fatalf("Failed to finish: %v", err)
	}
	
	// 验证可以读取
	reader, err := OpenSSTableForRead(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to open for read: %v", err)
	}
	defer reader.Close()
	
	// 验证所有 key 都能读取
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%03d", i))
		expectedValue := []byte(fmt.Sprintf("value%d", i))
		
		val, exists := reader.Get(key)
		if !exists {
			t.Fatalf("Expected to find key%03d", i)
		}
		if !bytes.Equal(val, expectedValue) {
			t.Fatalf("Expected value%d for key%03d, got %s", i, i, val)
		}
	}
}

func TestSSTableReader_NotFound(t *testing.T) {
	tmpFile := "/tmp/test_sstable_notfound.sst"
	defer os.Remove(tmpFile)
	
	opts := DefaultOptions()
	builder, err := NewSSTableBuilder(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	
	// 添加一些数据
	builder.Add([]byte("key1"), []byte("value1"))
	builder.Add([]byte("key3"), []byte("value3"))
	builder.Finish()
	
	// 尝试读取不存在的 key
	reader, err := OpenSSTableForRead(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to open for read: %v", err)
	}
	defer reader.Close()
	
	_, exists := reader.Get([]byte("key99"))
	if exists {
		t.Fatal("Expected not to find key99")
	}
}

func TestSSTableIterator_SeekToFirst(t *testing.T) {
	// 创建临时文件
	tmpFile := "/tmp/test_sstable_iter_first.sst"
	defer os.Remove(tmpFile)
	
	opts := DefaultOptions()
	builder, err := NewSSTableBuilder(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	
	// 按顺序添加数据（SSTable 要求数据在写入时已经排序）
	keys := []string{"key1", "key2", "key3", "key4", "key5"}
	for _, k := range keys {
		err = builder.Add([]byte(k), []byte("value"+k))
		if err != nil {
			t.Fatalf("Failed to add: %v", err)
		}
	}
	
	// 完成构建
	err = builder.Finish()
	if err != nil {
		t.Fatalf("Failed to finish: %v", err)
	}
	
	// 验证可以读取
	reader, err := OpenSSTableForRead(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to open for read: %v", err)
	}
	defer reader.Close()
	
	iter := reader.NewIterator()
	iter.SeekToFirst()
	
	expectedOrder := keys // 应该按添加顺序返回
	idx := 0
	
	for iter.Valid() {
		if idx >= len(expectedOrder) {
			t.Fatal("Too many entries")
		}
		key := string(iter.Key())
		if key != expectedOrder[idx] {
			t.Fatalf("Expected %s, got %s", expectedOrder[idx], key)
		}
		idx++
		iter.Next()
	}
	
	if idx != len(expectedOrder) {
		t.Fatalf("Expected %d entries, got %d", len(expectedOrder), idx)
	}
}

func TestSSTableIterator_Seek(t *testing.T) {
	tmpFile := "/tmp/test_sstable_iter_seek.sst"
	defer os.Remove(tmpFile)
	
	opts := DefaultOptions()
	builder, err := NewSSTableBuilder(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	
	// 添加有序数据
	for i := 0; i < 10; i++ {
		key := []byte(fmt.Sprintf("key%02d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		builder.Add(key, value)
	}
	builder.Finish()
	
	reader, err := OpenSSTableForRead(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to open for read: %v", err)
	}
	defer reader.Close()
	
	iter := reader.NewIterator()
	
	// Seek 到 key05
	if !iter.Seek([]byte("key05")) {
		t.Fatal("Seek should succeed")
	}
	
	key := string(iter.Key())
	if key != "key05" {
		t.Fatalf("Expected key05, got %s", key)
	}
}

func TestSSTableBuilder_Abort(t *testing.T) {
	tmpFile := "/tmp/test_sstable_abort.sst"
	
	opts := DefaultOptions()
	builder, err := NewSSTableBuilder(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	
	// 添加一些数据
	builder.Add([]byte("key1"), []byte("value1"))
	
	// 中止
	builder.Abort()
	
	// 验证文件被删除
	_, err = os.Stat(tmpFile)
	if !os.IsNotExist(err) {
		t.Fatal("Expected file to be deleted")
	}
}

func TestSSTable_EmptyTable(t *testing.T) {
	tmpFile := "/tmp/test_sstable_empty.sst"
	defer os.Remove(tmpFile)
	
	opts := DefaultOptions()
	builder, err := NewSSTableBuilder(tmpFile, opts)
	if err != nil {
		t.Fatalf("Failed to create builder: %v", err)
	}
	
	// 不添加任何数据，直接完成
	err = builder.Finish()
	if err != nil {
		t.Fatalf("Failed to finish: %v", err)
	}
	
	// 验证文件存在
	info, err := os.Stat(tmpFile)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}
	
	if info.Size() == 0 {
		t.Fatal("Expected non-zero file size even for empty table")
	}
}

func TestSSTable_CorruptedFooter(t *testing.T) {
	tmpFile := "/tmp/test_sstable_corrupted.sst"
	defer os.Remove(tmpFile)
	
	// 创建一个损坏的文件
	file, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	
	// 写入一些随机数据
	file.Write([]byte("random data"))
	file.Close()
	
	// 尝试打开应该失败
	opts := DefaultOptions()
	_, err = OpenSSTableForRead(tmpFile, opts)
	if err == nil {
		t.Fatal("Expected error when opening corrupted file")
	}
}

func TestBlockHandle_EncodeDecode(t *testing.T) {
	handle := BlockHandle{
		offset: 12345,
		size:   6789,
	}
	
	encoded := handle.Encode()
	
	var decoded BlockHandle
	_, err := decoded.Decode(encoded)
	if err != nil {
		t.Fatalf("Failed to decode: %v", err)
	}
	
	if decoded.offset != handle.offset {
		t.Fatalf("Expected offset %d, got %d", handle.offset, decoded.offset)
	}
	
	if decoded.size != handle.size {
		t.Fatalf("Expected size %d, got %d", handle.size, decoded.size)
	}
}

func BenchmarkSSTableBuilder_Add(b *testing.B) {
	tmpFile := "/tmp/bench_sstable.sst"
	defer os.Remove(tmpFile)
	
	opts := DefaultOptions()
	builder, _ := NewSSTableBuilder(tmpFile, opts)
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		builder.Add(key, value)
	}
	builder.Finish()
}

func BenchmarkSSTableReader_Get(b *testing.B) {
	tmpFile := "/tmp/bench_sstable_read.sst"
	defer os.Remove(tmpFile)
	
	opts := DefaultOptions()
	builder, _ := NewSSTableBuilder(tmpFile, opts)
	
	// 预填充数据
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		value := []byte(fmt.Sprintf("value%d", i))
		builder.Add(key, value)
	}
	builder.Finish()
	
	reader, _ := OpenSSTableForRead(tmpFile, opts)
	defer reader.Close()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("key%d", i%10000))
		reader.Get(key)
	}
}
