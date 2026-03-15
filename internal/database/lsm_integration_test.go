package database

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	//"time"

	"github.com/TZJ-BYTE/RediGo/config"
	"github.com/TZJ-BYTE/RediGo/internal/datastruct"
	"github.com/TZJ-BYTE/RediGo/internal/persistence"
)

// TestLSMIntegration 测试 LSM 引擎集成
func TestLSMIntegration(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建配置
	config := &DatabaseConfig{
		Type:    LSMPersistent,
		DataDir: tmpDir,
		Options: persistence.DefaultOptions(),
	}

	// 创建数据库
	db, err := NewDatabaseWithConfig(0, config)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	// 测试写入
	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for key, value := range testData {
		db.Set(key, &datastruct.DataValue{Value: &datastruct.String{Data: value}})
	}

	// 测试读取
	for key, expected := range testData {
		val, found := db.Get(key)
		if !found {
			t.Errorf("Key %s not found", key)
			continue
		}

		str, ok := val.Value.(*datastruct.String)
		if !ok {
			t.Errorf("Expected *datastruct.String, got %T", val.Value)
			continue
		}

		if str.Data != expected {
			t.Errorf("Expected %s, got %s", expected, str.Data)
		}
	}

	// 测试删除
	db.Delete("key1")
	_, found := db.Get("key1")
	if found {
		t.Error("Deleted key should not exist")
	}

	// 测试统计信息
	stats := db.GetStats()
	if stats["mode"] != "LSM" {
		t.Error("Expected LSM mode")
	}

	t.Logf("Database stats: %+v", stats)
}

// TestLSMRecovery 测试 LSM 恢复
func TestLSMRecovery(t *testing.T) {
	tmpDir := t.TempDir()

	// 第一次创建并写入数据
	config := &DatabaseConfig{
		Type:    LSMPersistent,
		DataDir: tmpDir,
		Options: persistence.DefaultOptions(),
	}

	db1, err := NewDatabaseWithConfig(0, config)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}

	// 写入数据
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		// 使用 *datastruct.String，模拟真实环境
		db1.Set(key, &datastruct.DataValue{Value: &datastruct.String{Data: value}})
	}

	// 强制刷写到 SSTable（通过触发 MemTable 刷写）
	// 注意：当前实现在 Close 时会自动刷写，所以我们直接关闭
	t.Log("Closing first database instance...")
	db1.Close()

	// 删除 WAL 文件，强制从 SSTable 恢复
	walFile := filepath.Join(tmpDir, "wal", "current.wal")

	// 等待文件释放（Windows 特别需要）
	// 在测试中，由于 db1.Close() 可能没有完全释放所有文件句柄（特别是 WAL），
	// 这里可能需要一点延迟或者重试

	err = os.Remove(walFile)
	if err != nil && !os.IsNotExist(err) {
		t.Logf("Warning: Failed to remove WAL file: %v", err)
	} else {
		t.Log("WAL file removed, forcing recovery from SSTable only")
	}

	// 重新打开数据库（应该从 SSTable 恢复）
	t.Log("Reopening database for recovery test...")
	db2, err := NewDatabaseWithConfig(0, config)
	if err != nil {
		t.Fatalf("Failed to reopen database: %v", err)
	}
	defer db2.Close()

	// 等待一下，也许是某种竞态？
	// time.Sleep(100 * time.Millisecond)

	// 尝试手动全量加载 (Hack: 通过反射调用 loadAllFromLSM)
	// 或者，我们假设 lazy_load 已经生效（我们在 NewDatabaseWithConfig 中强制了）

	// 检查 keys
	keys := db2.Keys()
	t.Logf("DB2 Keys after reopen: %v", keys)

	t.Log("Verifying recovered data...")
	recoveredCount := 0
	missingKeys := 0

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)

		val, found := db2.Get(key)
		if !found {
			missingKeys++
			// t.Errorf("Missing key after recovery: %s", key) // 减少日志噪音
			continue
		}

		str, ok := val.Value.(*datastruct.String)
		if !ok {
			t.Errorf("Key %s: expected *datastruct.String, got %T", key, val.Value)
			continue
		}

		if str.Data != expectedValue {
			t.Errorf("Key %s: expected value %s, got %s", key, expectedValue, str.Data)
		} else {
			recoveredCount++
		}
	}

	t.Logf("Recovery complete: %d/%d keys recovered, %d keys missing",
		recoveredCount, 100, missingKeys)

	if missingKeys > 0 {
		t.Errorf("Data loss detected: %d keys not recovered from SSTable", missingKeys)
	} else {
		t.Logf("✅ All data successfully recovered from SSTable")
	}
}

// TestDBManagerWithLSM 测试 DBManager 集成 LSM
func TestDBManagerWithLSM(t *testing.T) {
	tmpDir := t.TempDir()

	// 创建模拟配置
	cfg := &config.Config{
		DBCount:            2,
		PersistenceEnabled: true,
		PersistenceType:    "lsm",
		DataDir:            tmpDir,
		BlockSize:          4096,
		MemTableSize:       4 << 20,
	}

	// 创建 DBManager
	manager := NewDBManager(cfg)

	// 测试基本操作
	db := manager.GetDefaultDB()
	db.Set("test_key", &datastruct.DataValue{Value: &datastruct.String{Data: "test_value"}})

	val, found := db.Get("test_key")
	if !found {
		t.Error("Should find test_key")
	}
	str, ok := val.Value.(*datastruct.String)
	if !ok || str.Data != "test_value" {
		t.Errorf("Expected test_value, got %v", val.Value)
	}

	// 测试切换数据库
	db1, err := manager.GetDBByIndex(1)
	if err != nil {
		t.Fatalf("Failed to get DB 1: %v", err)
	}
	db1.Set("db1_key", &datastruct.DataValue{Value: &datastruct.String{Data: "db1_value"}})

	// 验证两个数据库独立
	db0 := manager.GetDefaultDB()
	_, found = db0.Get("db1_key")
	if found {
		t.Error("db0 should not have db1_key")
	}

	// 关闭所有数据库
	manager.Close()
}

// BenchmarkLSMWrite 基准测试：LSM 写入性能
func BenchmarkLSMWrite(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench_lsm_write")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	config := &DatabaseConfig{
		Type:    LSMPersistent,
		DataDir: tmpDir,
		Options: persistence.DefaultOptions(),
	}

	db, _ := NewDatabaseWithConfig(0, config)
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		db.Set(key, &datastruct.DataValue{Value: &datastruct.String{Data: value}})
	}
}

// BenchmarkLSMRead 基准测试：LSM 读取性能
func BenchmarkLSMRead(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "bench_lsm_read")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	config := &DatabaseConfig{
		Type:    LSMPersistent,
		DataDir: tmpDir,
		Options: persistence.DefaultOptions(),
	}

	db, _ := NewDatabaseWithConfig(0, config)
	defer db.Close()

	// 预填充数据
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		db.Set(key, &datastruct.DataValue{Value: &datastruct.String{Data: value}})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("key%d", i%10000)
		db.Get(key)
	}
}
