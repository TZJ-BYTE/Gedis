package persistence

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
)

func TestWALWriter_Basic(t *testing.T) {
	tmpFile := "/tmp/test_wal_basic.wal"
	defer os.Remove(tmpFile)
	
	// 创建 WAL 写入器
	writer, err := NewWALWriter(tmpFile, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to create WAL writer: %v", err)
	}
	defer writer.Close()
	
	// 测试 Put 操作
	err = writer.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Failed to write put record: %v", err)
	}
	
	err = writer.Put([]byte("key2"), []byte("value2"))
	if err != nil {
		t.Fatalf("Failed to write put record: %v", err)
	}
	
	// 测试 Delete 操作
	err = writer.Delete([]byte("key3"))
	if err != nil {
		t.Fatalf("Failed to write delete record: %v", err)
	}
	
	// 验证序列号
	if writer.GetSeqNum() != 3 {
		t.Errorf("Expected seqNum=3, got %d", writer.GetSeqNum())
	}
}

func TestWALReader_Basic(t *testing.T) {
	tmpFile := "/tmp/test_wal_read.wal"
	defer os.Remove(tmpFile)
	
	// 先写入数据
	writer, err := NewWALWriter(tmpFile, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to create WAL writer: %v", err)
	}
	
	err = writer.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	
	err = writer.Put([]byte("key2"), []byte("value2"))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	
	err = writer.Delete([]byte("key3"))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	
	writer.Close()
	
	// 读取并验证
	reader, err := NewWALReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create WAL reader: %v", err)
	}
	defer reader.Close()
	
	// 读取第 1 条记录
	record, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	if record.Type != WALRecordTypePut {
		t.Errorf("Expected PUT record, got %d", record.Type)
	}
	if !bytes.Equal(record.Key, []byte("key1")) {
		t.Errorf("Expected key1, got %s", record.Key)
	}
	if !bytes.Equal(record.Value, []byte("value1")) {
		t.Errorf("Expected value1, got %s", record.Value)
	}
	
	// 读取第 2 条记录
	record, err = reader.ReadNext()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	if record.Type != WALRecordTypePut {
		t.Errorf("Expected PUT record, got %d", record.Type)
	}
	if !bytes.Equal(record.Key, []byte("key2")) {
		t.Errorf("Expected key2, got %s", record.Key)
	}
	if !bytes.Equal(record.Value, []byte("value2")) {
		t.Errorf("Expected value2, got %s", record.Value)
	}
	
	// 读取第 3 条记录
	record, err = reader.ReadNext()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	if record.Type != WALRecordTypeDelete {
		t.Errorf("Expected DELETE record, got %d", record.Type)
	}
	if !bytes.Equal(record.Key, []byte("key3")) {
		t.Errorf("Expected key3, got %s", record.Key)
	}
	if len(record.Value) != 0 {
		t.Errorf("Expected empty value for DELETE")
	}
	
	// 验证 EOF
	_, err = reader.ReadNext()
	if err != io.EOF {
		t.Errorf("Expected EOF, got %v", err)
	}
}

func TestWAL_Replay(t *testing.T) {
	tmpFile := "/tmp/test_wal_replay.wal"
	defer os.Remove(tmpFile)
	
	// 先写入数据
	writer, err := NewWALWriter(tmpFile, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to create WAL writer: %v", err)
	}
	
	// 写入多条记录
	operations := []struct {
		Type  WALRecordType
		Key   string
		Value string
	}{
		{WALRecordTypePut, "key1", "value1"},
		{WALRecordTypePut, "key2", "value2"},
		{WALRecordTypeDelete, "key3", ""},
		{WALRecordTypePut, "key4", "value4"},
	}
	
	for _, op := range operations {
		err := writer.Write(op.Type, []byte(op.Key), []byte(op.Value))
		if err != nil {
			t.Fatalf("Failed to write: %v", err)
		}
	}
	
	writer.Close()
	
	// 重放 WAL
	var replayedOps []string
	lastSeq, err := ReplayWAL(tmpFile, func(record *WALRecord) error {
		op := fmt.Sprintf("%d:%s=%s", record.Type, record.Key, record.Value)
		replayedOps = append(replayedOps, op)
		return nil
	})
	
	if err != nil {
		t.Fatalf("Failed to replay WAL: %v", err)
	}
	
	// 验证重放结果
	if len(replayedOps) != 4 {
		t.Fatalf("Expected 4 operations, got %d", len(replayedOps))
	}
	
	if lastSeq != 3 {
		t.Errorf("Expected lastSeq=3, got %d", lastSeq)
	}
}

func TestWAL_Checksum(t *testing.T) {
	tmpFile := "/tmp/test_wal_checksum.wal"
	defer os.Remove(tmpFile)
	
	// 写入数据
	writer, err := NewWALWriter(tmpFile, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to create WAL writer: %v", err)
	}
	
	err = writer.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	
	writer.Close()
	
	// 读取并验证校验和
	reader, err := NewWALReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create WAL reader: %v", err)
	}
	defer reader.Close()
	
	record, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	
	// 校验和已经在读取时验证过了
	
	if record.Type != WALRecordTypePut {
		t.Errorf("Expected PUT record")
	}
}

func TestWAL_EmptyFile(t *testing.T) {
	tmpFile := "/tmp/test_wal_empty.wal"
	defer os.Remove(tmpFile)
	
	// 创建空文件
	file, err := os.Create(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}
	file.Close()
	
	// 读取空文件
	reader, err := NewWALReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create WAL reader: %v", err)
	}
	defer reader.Close()
	
	// 应该立即返回 EOF
	_, err = reader.ReadNext()
	if err != io.EOF {
		t.Errorf("Expected EOF for empty file, got %v", err)
	}
}

func TestWAL_LargeData(t *testing.T) {
	tmpFile := "/tmp/test_wal_large.wal"
	defer os.Remove(tmpFile)
	
	// 创建 WAL 写入器
	writer, err := NewWALWriter(tmpFile, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to create WAL writer: %v", err)
	}
	defer writer.Close()
	
	// 写入大数据
	largeKey := make([]byte, 1000)
	largeValue := make([]byte, 10000)
	for i := range largeKey {
		largeKey[i] = byte(i % 256)
	}
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}
	
	err = writer.Put(largeKey, largeValue)
	if err != nil {
		t.Fatalf("Failed to write large data: %v", err)
	}
	
	// 读取并验证
	reader, err := NewWALReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create WAL reader: %v", err)
	}
	defer reader.Close()
	
	record, err := reader.ReadNext()
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}
	
	if !bytes.Equal(record.Key, largeKey) {
		t.Error("Key mismatch")
	}
	if !bytes.Equal(record.Value, largeValue) {
		t.Error("Value mismatch")
	}
}

func TestWAL_MultipleRecords(t *testing.T) {
	tmpFile := "/tmp/test_wal_multiple.wal"
	defer os.Remove(tmpFile)
	
	const numRecords = 100
	
	// 写入多条记录
	writer, err := NewWALWriter(tmpFile, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to create WAL writer: %v", err)
	}
	
	for i := 0; i < numRecords; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		err := writer.Put([]byte(key), []byte(value))
		if err != nil {
			t.Fatalf("Failed to write record %d: %v", i, err)
		}
	}
	
	writer.Close()
	
	// 读取并验证
	reader, err := NewWALReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create WAL reader: %v", err)
	}
	defer reader.Close()
	
	count := 0
	for {
		record, err := reader.ReadNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read: %v", err)
		}
		
		expectedKey := fmt.Sprintf("key%d", count)
		expectedValue := fmt.Sprintf("value%d", count)
		
		if !bytes.Equal(record.Key, []byte(expectedKey)) {
			t.Errorf("Record %d: expected key %s, got %s", count, expectedKey, record.Key)
		}
		if !bytes.Equal(record.Value, []byte(expectedValue)) {
			t.Errorf("Record %d: expected value %s, got %s", count, expectedValue, record.Value)
		}
		
		count++
	}
	
	if count != numRecords {
		t.Errorf("Expected %d records, got %d", numRecords, count)
	}
}

func TestWAL_CorruptedData(t *testing.T) {
	tmpFile := "/tmp/test_wal_corrupted.wal"
	defer os.Remove(tmpFile)
	
	// 写入正常数据
	writer, err := NewWALWriter(tmpFile, 1024*1024)
	if err != nil {
		t.Fatalf("Failed to create WAL writer: %v", err)
	}
	
	err = writer.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Failed to write: %v", err)
	}
	
	writer.Close()
	
	// 损坏文件（追加垃圾数据）
	file, err := os.OpenFile(tmpFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}
	
	_, err = file.Write([]byte("garbage data"))
	if err != nil {
		t.Fatalf("Failed to write garbage: %v", err)
	}
	file.Close()
	
	// 读取时应该能检测到错误
	reader, err := NewWALReader(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create WAL reader: %v", err)
	}
	defer reader.Close()
	
	// 读取第一条（正常的）
	_, err = reader.ReadNext()
	if err != nil {
		t.Fatalf("Failed to read first record: %v", err)
	}
	
	// 读取第二条（损坏的）应该失败
	_, err = reader.ReadNext()
	if err == nil {
		t.Error("Expected error when reading corrupted data, got nil")
	}
}
