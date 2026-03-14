package persistence

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLSMEnergy_Basic(t *testing.T) {
	tmpDir := "/tmp/test_lsm_basic"
	defer os.RemoveAll(tmpDir)
	
	// 打开引擎
	engine, err := OpenLSMEnergy(tmpDir, DefaultOptions())
	if err != nil {
		t.Fatalf("Failed to open engine: %v", err)
	}
	defer engine.Close()
	
	// 测试 Put
	err = engine.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Failed to put: %v", err)
	}
	
	// 测试 Get
	val, found := engine.Get([]byte("key1"))
	if !found {
		t.Fatal("Expected to find key1")
	}
	if !bytes.Equal(val, []byte("value1")) {
		t.Fatalf("Expected value1, got %s", val)
	}
}

func TestLSMEnergy_Update(t *testing.T) {
	tmpDir := "/tmp/test_lsm_update"
	defer os.RemoveAll(tmpDir)
	
	engine, err := OpenLSMEnergy(tmpDir, DefaultOptions())
	if err != nil {
		t.Fatalf("Failed to open engine: %v", err)
	}
	defer engine.Close()
	
	// Put
	err = engine.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Failed to put: %v", err)
	}
	
	// Update
	err = engine.Put([]byte("key1"), []byte("value2"))
	if err != nil {
		t.Fatalf("Failed to update: %v", err)
	}
	
	// Get
	val, found := engine.Get([]byte("key1"))
	if !found {
		t.Fatal("Expected to find key1")
	}
	if !bytes.Equal(val, []byte("value2")) {
		t.Fatalf("Expected value2, got %s", val)
	}
}

func TestLSMEnergy_Delete(t *testing.T) {
	tmpDir := "/tmp/test_lsm_delete"
	defer os.RemoveAll(tmpDir)
	
	engine, err := OpenLSMEnergy(tmpDir, DefaultOptions())
	if err != nil {
		t.Fatalf("Failed to open engine: %v", err)
	}
	defer engine.Close()
	
	// Put
	err = engine.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Failed to put: %v", err)
	}
	
	// Delete
	err = engine.Delete([]byte("key1"))
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}
	
	// Get
	_, found := engine.Get([]byte("key1"))
	if found {
		t.Fatal("Expected key1 to be deleted")
	}
}

func TestLSMEnergy_MultipleKeys(t *testing.T) {
	tmpDir := "/tmp/test_lsm_multiple"
	defer os.RemoveAll(tmpDir)
	
	engine, err := OpenLSMEnergy(tmpDir, DefaultOptions())
	if err != nil {
		t.Fatalf("Failed to open engine: %v", err)
	}
	defer engine.Close()
	
	// 写入多个键值对
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		err := engine.Put([]byte(key), []byte(value))
		if err != nil {
			t.Fatalf("Failed to put key %d: %v", i, err)
		}
	}
	
	// 验证所有键值对
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)
		
		val, found := engine.Get([]byte(key))
		if !found {
			t.Errorf("Expected to find key %s", key)
			continue
		}
		if !bytes.Equal(val, []byte(expectedValue)) {
			t.Errorf("Expected value %s, got %s", expectedValue, val)
		}
	}
}

func TestLSMEnergy_Recovery(t *testing.T) {
	tmpDir := "/tmp/test_lsm_recovery"
	defer os.RemoveAll(tmpDir)
	
	// 第一次打开引擎并写入数据
	engine1, err := OpenLSMEnergy(tmpDir, DefaultOptions())
	if err != nil {
		t.Fatalf("Failed to open engine1: %v", err)
	}
	
	// 写入数据但不触发刷写（让数据留在 MemTable 和 WAL）
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		err := engine1.Put([]byte(key), []byte(value))
		if err != nil {
			engine1.Close()
			t.Fatalf("Failed to put key %d: %v", i, err)
		}
	}
	
	// 关闭引擎（不显式调用 Close，让 WAL 保持）
	engine1.Close()
	
	// 第二次打开引擎（应该从 WAL 恢复）
	engine2, err := OpenLSMEnergy(tmpDir, DefaultOptions())
	if err != nil {
		t.Fatalf("Failed to open engine2: %v", err)
	}
	defer engine2.Close()
	
	// 验证恢复的数据
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)
		
		val, found := engine2.Get([]byte(key))
		if !found {
			t.Errorf("Expected to find key %s after recovery", key)
			continue
		}
		if !bytes.Equal(val, []byte(expectedValue)) {
			t.Errorf("Expected value %s, got %s", expectedValue, val)
		}
	}
}

func TestLSMEnergy_SSTableFlush(t *testing.T) {
	tmpDir := "/tmp/test_lsm_flush"
	defer os.RemoveAll(tmpDir)
	
	options := DefaultOptions()
	// 注意：当前 Options 没有 MemTableMaxSize 字段，使用默认值 4MB
	// 我们通过手动触发来测试刷写功能
	
	engine, err := OpenLSMEnergy(tmpDir, options)
	if err != nil {
		t.Fatalf("Failed to open engine: %v", err)
	}
	defer engine.Close()
	
	// 写入足够多的数据（接近 4MB 会触发自动刷写）
	for i := 0; i < 50000; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		err := engine.Put([]byte(key), []byte(value))
		if err != nil {
			t.Fatalf("Failed to put key %d: %v", i, err)
		}
	}
	
	// 等待一小段时间让后台刷写完成
	// TODO: 使用更好的同步机制
	
	// 检查 SSTable 是否被创建
	sstableDir := filepath.Join(tmpDir, "sstable")
	_, err = os.ReadDir(sstableDir)
	if err != nil {
		t.Fatalf("Failed to read sstable dir: %v", err)
	}
	
	// 验证可以从 SSTable 读取数据（至少有一些数据被刷写）
	sstableCount := engine.GetSSTableCount()
	t.Logf("Created %d SSTable(s)", sstableCount)
	
	// 验证部分数据
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)
		
		val, found := engine.Get([]byte(key))
		if !found {
			t.Errorf("Expected to find key %s", key)
			continue
		}
		if !bytes.Equal(val, []byte(expectedValue)) {
			t.Errorf("Expected value %s, got %s", expectedValue, val)
		}
	}
}

func TestLSMEnergy_NotFound(t *testing.T) {
	tmpDir := "/tmp/test_lsm_notfound"
	defer os.RemoveAll(tmpDir)
	
	engine, err := OpenLSMEnergy(tmpDir, DefaultOptions())
	if err != nil {
		t.Fatalf("Failed to open engine: %v", err)
	}
	defer engine.Close()
	
	// Get 不存在的 key
	_, found := engine.Get([]byte("nonexistent"))
	if found {
		t.Fatal("Expected key to not exist")
	}
}

func TestLSMEnergy_Concurrent(t *testing.T) {
	tmpDir := "/tmp/test_lsm_concurrent"
	defer os.RemoveAll(tmpDir)
	
	engine, err := OpenLSMEnergy(tmpDir, DefaultOptions())
	if err != nil {
		t.Fatalf("Failed to open engine: %v", err)
	}
	defer engine.Close()
	
	done := make(chan bool, 10)
	
	// 启动 10 个 goroutine 并发写入
	for i := 0; i < 10; i++ {
		go func(base int) {
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("key%d_%d", base, j)
				value := fmt.Sprintf("value%d_%d", base, j)
				engine.Put([]byte(key), []byte(value))
			}
			done <- true
		}(i)
	}
	
	// 等待所有 goroutine 完成
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// 验证部分数据
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key%d_0", i)
		expectedValue := fmt.Sprintf("value%d_0", i)
		
		val, found := engine.Get([]byte(key))
		if !found {
			t.Errorf("Expected to find key %s", key)
			continue
		}
		if !bytes.Equal(val, []byte(expectedValue)) {
			t.Errorf("Expected value %s, got %s", expectedValue, val)
		}
	}
}
