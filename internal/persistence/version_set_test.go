package persistence

import (
	"os"
	"testing"
)

func TestVersionSet_Basic(t *testing.T) {
	tmpDir := "/tmp/test_version_basic"
	defer os.RemoveAll(tmpDir)
	
	// 打开版本集合
	vs, err := OpenVersionSet(tmpDir, MaxLevels)
	if err != nil {
		t.Fatalf("Failed to open version set: %v", err)
	}
	defer vs.Close()
	
	// 获取下一个文件编号
	fileNum := vs.GetNextFileNum()
	if fileNum != 1 {
		t.Errorf("Expected fileNum=1, got %d", fileNum)
	}
	
	// 创建文件元数据
	fm := &FileMetadata{
		FileNum:     fileNum,
		Size:        1024,
		SmallestKey: []byte("key1"),
		LargestKey:  []byte("key10"),
		Level:       0,
	}
	
	// 记录添加文件
	err = vs.LogAddFile(fm)
	if err != nil {
		t.Fatalf("Failed to log add file: %v", err)
	}
	
	// 验证当前版本
	version := vs.GetCurrentVersion()
	if len(version.Files[0]) != 1 {
		t.Errorf("Expected 1 file in level 0, got %d", len(version.Files[0]))
	}
	
	// 验证统计信息
	stats := vs.GetStats()
	if stats["total_files"] != 1 {
		t.Errorf("Expected total_files=1, got %v", stats["total_files"])
	}
}

func TestVersionSet_RemoveFile(t *testing.T) {
	tmpDir := "/tmp/test_version_remove"
	defer os.RemoveAll(tmpDir)
	
	vs, err := OpenVersionSet(tmpDir, MaxLevels)
	if err != nil {
		t.Fatalf("Failed to open version set: %v", err)
	}
	defer vs.Close()
	
	// 添加文件
	for i := uint64(1); i <= 3; i++ {
		fm := &FileMetadata{
			FileNum:     i,
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
	
	// 删除中间的文件
	err = vs.LogDeleteFile(0, 2)
	if err != nil {
		t.Fatalf("Failed to log delete file: %v", err)
	}
	
	// 验证剩余文件
	version := vs.GetCurrentVersion()
	if len(version.Files[0]) != 2 {
		t.Errorf("Expected 2 files in level 0, got %d", len(version.Files[0]))
	}
}

func TestVersionSet_Recovery(t *testing.T) {
	tmpDir := "/tmp/test_version_recovery"
	defer os.RemoveAll(tmpDir)
	
	// 第一次打开并添加文件
	vs1, err := OpenVersionSet(tmpDir, MaxLevels)
	if err != nil {
		t.Fatalf("Failed to open version set 1: %v", err)
	}
	
	for i := uint64(1); i <= 5; i++ {
		fm := &FileMetadata{
			FileNum:     i,
			Size:        int64(1024 * i),
			SmallestKey: []byte("key1"),
			LargestKey:  []byte("key10"),
			Level:       int(i % 3), // 分散到不同层级
		}
		err := vs1.LogAddFile(fm)
		if err != nil {
			vs1.Close()
			t.Fatalf("Failed to log add file: %v", err)
		}
	}
	
	vs1.Close()
	
	// 第二次打开（应该从 MANIFEST 恢复）
	vs2, err := OpenVersionSet(tmpDir, MaxLevels)
	if err != nil {
		t.Fatalf("Failed to open version set 2: %v", err)
	}
	defer vs2.Close()
	
	// 验证恢复的版本
	version := vs2.GetCurrentVersion()
	totalFiles := version.GetFileCount()
	if totalFiles != 5 {
		t.Errorf("Expected 5 files after recovery, got %d", totalFiles)
	}
	
	// 验证每个层级的文件数
	// i=1 -> level=1, i=2 -> level=2, i=3 -> level=0, i=4 -> level=1, i=5 -> level=2
	// 所以：level 0: 1 个，level 1: 2 个，level 2: 2 个
	expectedLevels := map[int]int{0: 1, 1: 2, 2: 2}
	for level, expected := range expectedLevels {
		actual := len(version.Files[level])
		if actual != expected {
			t.Errorf("Expected %d files in level %d, got %d", expected, level, actual)
		}
	}
}

func TestVersion_MultipleLevels(t *testing.T) {
	tmpDir := "/tmp/test_version_levels"
	defer os.RemoveAll(tmpDir)
	
	vs, err := OpenVersionSet(tmpDir, MaxLevels)
	if err != nil {
		t.Fatalf("Failed to open version set: %v", err)
	}
	defer vs.Close()
	
	// 在不同层级添加文件
	for level := 0; level < MaxLevels; level++ {
		for i := 0; i < level+1; i++ {
			fm := &FileMetadata{
				FileNum:     vs.GetNextFileNum(),
				Size:        1024,
				SmallestKey: []byte("key1"),
				LargestKey:  []byte("key10"),
				Level:       level,
			}
			err := vs.LogAddFile(fm)
			if err != nil {
				t.Fatalf("Failed to log add file: %v", err)
			}
		}
	}
	
	// 验证每个层级的文件数
	version := vs.GetCurrentVersion()
	for level := 0; level < MaxLevels; level++ {
		expected := level + 1
		actual := len(version.Files[level])
		if actual != expected {
			t.Errorf("Expected %d files in level %d, got %d", expected, level, actual)
		}
	}
	
	// 验证总大小
	totalSize := version.GetTotalSize()
	expectedSize := int64(0)
	for level := 0; level < MaxLevels; level++ {
		expectedSize += int64((level + 1) * 1024)
	}
	if totalSize != expectedSize {
		t.Errorf("Expected total size %d, got %d", expectedSize, totalSize)
	}
}
