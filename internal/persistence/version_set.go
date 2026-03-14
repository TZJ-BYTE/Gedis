package persistence

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	
	"github.com/TZJ-BYTE/RediGo/pkg/logger"
)

const (
	// ManifestFileName MANIFEST 文件名
	ManifestFileName = "MANIFEST"
	
	// CurrentFileName CURRENT 文件名
	CurrentFileName = "CURRENT"
	
	// VersionSetMagicNumber 魔数
	VersionSetMagicNumber uint64 = 0x56455253494F4E // "VERSION"
)

// FileMetadata SSTable 文件元数据
type FileMetadata struct {
	FileNum   uint64        // 文件编号
	Size      int64         // 文件大小
	SmallestKey []byte      // 最小 key
	LargestKey  []byte      // 最大 key
	Level     int           // 所在层级
}

// Encode 编码文件元数据
func (fm *FileMetadata) Encode() []byte {
	// 处理 nil key
	smallestKey := fm.SmallestKey
	if smallestKey == nil {
		smallestKey = []byte{}
	}
	largestKey := fm.LargestKey
	if largestKey == nil {
		largestKey = []byte{}
	}
	
	// 计算总大小：FileNum(8) + Size(8) + SmallestKeyLen(4) + SmallestKey + LargestKeyLen(4) + LargestKey + Level(4)
	size := 8 + 8 + 4 + len(smallestKey) + 4 + len(largestKey) + 4
	data := make([]byte, size)
	pos := 0
	
	// FileNum
	binary.LittleEndian.PutUint64(data[pos:], fm.FileNum)
	pos += 8
	
	// Size
	binary.LittleEndian.PutUint64(data[pos:], uint64(fm.Size))
	pos += 8
	
	// SmallestKey
	binary.LittleEndian.PutUint32(data[pos:], uint32(len(smallestKey)))
	pos += 4
	copy(data[pos:], smallestKey)
	pos += len(smallestKey)
	
	// LargestKey
	binary.LittleEndian.PutUint32(data[pos:], uint32(len(largestKey)))
	pos += 4
	copy(data[pos:], largestKey)
	pos += len(largestKey)
	
	// Level
	binary.LittleEndian.PutUint32(data[pos:], uint32(fm.Level))
	
	return data
}

// DecodeFileMetadata 解码文件元数据
func DecodeFileMetadata(data []byte) (*FileMetadata, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("data too short")
	}
	
	pos := 0
	fm := &FileMetadata{}
	
	// FileNum
	fm.FileNum = binary.LittleEndian.Uint64(data[pos:])
	pos += 8
	
	// Size
	fm.Size = int64(binary.LittleEndian.Uint64(data[pos:]))
	pos += 8
	
	// SmallestKey
	keyLen := int(binary.LittleEndian.Uint32(data[pos:]))
	pos += 4
	fm.SmallestKey = make([]byte, keyLen)
	copy(fm.SmallestKey, data[pos:pos+keyLen])
	pos += keyLen
	
	// LargestKey
	keyLen = int(binary.LittleEndian.Uint32(data[pos:]))
	pos += 4
	fm.LargestKey = make([]byte, keyLen)
	copy(fm.LargestKey, data[pos:pos+keyLen])
	pos += keyLen
	
	// Level
	fm.Level = int(binary.LittleEndian.Uint32(data[pos:]))
	
	return fm, nil
}

// Version 版本快照
type Version struct {
	Files [][]*FileMetadata // 每个 level 的文件列表
}

// NewVersion 创建新版本
func NewVersion(maxLevels int) *Version {
	return &Version{
		Files: make([][]*FileMetadata, maxLevels),
	}
}

// AddFile 添加文件到指定 level
func (v *Version) AddFile(level int, fm *FileMetadata) {
	if level >= len(v.Files) {
		return
	}
	v.Files[level] = append(v.Files[level], fm)
}

// RemoveFile 从指定 level 移除文件
func (v *Version) RemoveFile(level int, fileNum uint64) {
	if level >= len(v.Files) {
		return
	}
	
	files := v.Files[level]
	for i, fm := range files {
		if fm.FileNum == fileNum {
			v.Files[level] = append(files[:i], files[i+1:]...)
			break
		}
	}
}

// GetTotalSize 获取总大小
func (v *Version) GetTotalSize() int64 {
	var total int64
	for _, files := range v.Files {
		for _, fm := range files {
			total += fm.Size
		}
	}
	return total
}

// GetFileCount 获取文件总数
func (v *Version) GetFileCount() int {
	count := 0
	for _, files := range v.Files {
		count += len(files)
	}
	return count
}

// VersionSet 版本集合
type VersionSet struct {
	mu              sync.Mutex        // 并发控制
	dbDir           string            // 数据库目录
	maxLevels       int               // 最大层级数
	currentVersion  *Version          // 当前版本
	nextFileNum     uint64            // 下一个文件编号
	manifestFile    *os.File          // MANIFEST 文件
	bufWriter       *bufio.Writer     // 缓冲写入器
	
	closed          bool              // 是否已关闭
}

// OpenVersionSet 打开版本集合
func OpenVersionSet(dbDir string, maxLevels int) (*VersionSet, error) {
	vs := &VersionSet{
		dbDir:      dbDir,
		maxLevels:  maxLevels,
		nextFileNum: 1,
		closed:     false,
	}
	
	// 创建版本目录
	versionDir := filepath.Join(dbDir, "version")
	err := os.MkdirAll(versionDir, 0755)
	if err != nil {
		return nil, err
	}
	
	// 检查是否有现有的 MANIFEST 文件
	manifestPath := filepath.Join(versionDir, ManifestFileName)
	currentPath := filepath.Join(versionDir, CurrentFileName)
	
	if _, err := os.Stat(currentPath); err == nil {
		// 读取 CURRENT 文件获取 MANIFEST 文件名
		content, err := os.ReadFile(currentPath)
		if err != nil {
			return nil, err
		}
		
		manifestName := string(content)
		manifestPath = filepath.Join(versionDir, manifestName)
		
		// 恢复 MANIFEST
		err = vs.recoverFromManifest(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("failed to recover from manifest: %v", err)
		}
	} else {
		// 创建新的版本集合
		vs.currentVersion = NewVersion(maxLevels)
		
		// 创建 MANIFEST 文件
		err = vs.createManifest()
		if err != nil {
			return nil, err
		}
	}
	
	return vs, nil
}

// createManifest 创建 MANIFEST 文件
func (vs *VersionSet) createManifest() error {
	versionDir := filepath.Join(vs.dbDir, "version")
	manifestPath := filepath.Join(versionDir, ManifestFileName)
	currentPath := filepath.Join(versionDir, CurrentFileName)
	
	var err error
	vs.manifestFile, err = os.OpenFile(manifestPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	
	vs.bufWriter = bufio.NewWriter(vs.manifestFile)
	
	// 写入魔数
	magicData := make([]byte, 8)
	binary.LittleEndian.PutUint64(magicData, VersionSetMagicNumber)
	_, err = vs.manifestFile.Write(magicData)
	if err != nil {
		return err
	}
	
	// 写入 CURRENT 文件
	err = os.WriteFile(currentPath, []byte(ManifestFileName), 0644)
	if err != nil {
		return err
	}
	
	return nil
}

// recoverFromManifest 从 MANIFEST 恢复
func (vs *VersionSet) recoverFromManifest(manifestPath string) error {
	file, err := os.Open(manifestPath)
	if err != nil {
		return err
	}
	defer file.Close()
	
	// 读取魔数
	magicData := make([]byte, 8)
	_, err = file.Read(magicData)
	if err != nil {
		return err
	}
	
	magic := binary.LittleEndian.Uint64(magicData)
	if magic != VersionSetMagicNumber {
		return fmt.Errorf("invalid manifest magic number")
	}
	
	// 读取所有记录
	reader := bufio.NewReader(file)
	vs.currentVersion = NewVersion(vs.maxLevels)
	
	logger.Info("Recovering VersionSet from MANIFEST...")
	recordCount := 0
	fileCount := 0
	
	for {
		// 读取记录长度
		lenBuf := make([]byte, 4)
		_, err := reader.Read(lenBuf)
		if err != nil {
			break // EOF
		}
		
		recordLen := binary.LittleEndian.Uint32(lenBuf)
		recordData := make([]byte, recordLen)
		_, err = reader.Read(recordData)
		if err != nil {
			return err
		}
		
		recordCount++
		
		// 解析记录
		recordType := recordData[0]
		switch recordType {
		case 1: // 新增文件
			fm, err := DecodeFileMetadata(recordData[1:])
			if err != nil {
				return err
			}
			vs.currentVersion.AddFile(fm.Level, fm)
			fileCount++
			logger.Debug("Recovered file metadata: num=%d, level=%d, size=%d", 
				fm.FileNum, fm.Level, fm.Size)
			
			// 更新 nextFileNum
			if fm.FileNum >= vs.nextFileNum {
				vs.nextFileNum = fm.FileNum + 1
			}
		case 2: // 删除文件
			if len(recordData) >= 9 {
				level := int(recordData[1])
				fileNum := binary.LittleEndian.Uint64(recordData[2:])
				vs.currentVersion.RemoveFile(level, fileNum)
				logger.Debug("Removed file: num=%d from level=%d", fileNum, level)
			}
		}
	}
	
	logger.Info("VersionSet recovery complete: %d records, %d files", recordCount, fileCount)
	
	// 重新打开 MANIFEST 用于追加
	vs.manifestFile, err = os.OpenFile(manifestPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	
	vs.bufWriter = bufio.NewWriter(vs.manifestFile)
	
	return nil
}

// LogAddFile 记录文件添加
func (vs *VersionSet) LogAddFile(fm *FileMetadata) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	
	if vs.closed {
		return fmt.Errorf("version set is closed")
	}
	
	// 先更新内存中的版本
	vs.currentVersion.AddFile(fm.Level, fm)
	
	// 编码记录
	record := make([]byte, 1+len(fm.Encode()))
	record[0] = 1 // 新增类型
	copy(record[1:], fm.Encode())
	
	// 写入记录长度
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(record)))
	
	_, err := vs.bufWriter.Write(lenBuf)
	if err != nil {
		return err
	}
	
	_, err = vs.bufWriter.Write(record)
	if err != nil {
		return err
	}
	
	err = vs.bufWriter.Flush()
	if err != nil {
		return err
	}
	
	return vs.manifestFile.Sync()
}

// LogDeleteFile 记录文件删除
func (vs *VersionSet) LogDeleteFile(level int, fileNum uint64) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	
	if vs.closed {
		return fmt.Errorf("version set is closed")
	}
	
	// 先更新内存中的版本
	vs.currentVersion.RemoveFile(level, fileNum)
	
	// 编码记录：type(1) + level(1) + fileNum(8) = 10 bytes
	record := make([]byte, 10)
	record[0] = 2 // 删除类型
	record[1] = byte(level)
	binary.LittleEndian.PutUint64(record[2:], fileNum)
	
	// 写入记录长度
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(record)))
	
	_, err := vs.bufWriter.Write(lenBuf)
	if err != nil {
		return err
	}
	
	_, err = vs.bufWriter.Write(record)
	if err != nil {
		return err
	}
	
	err = vs.bufWriter.Flush()
	if err != nil {
		return err
	}
	
	return vs.manifestFile.Sync()
}

// GetCurrentVersion 获取当前版本
func (vs *VersionSet) GetCurrentVersion() *Version {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	return vs.currentVersion
}

// GetNextFileNum 获取下一个文件编号
func (vs *VersionSet) GetNextFileNum() uint64 {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	num := vs.nextFileNum
	vs.nextFileNum++
	return num
}

// Close 关闭版本集合
func (vs *VersionSet) Close() error {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	
	if vs.closed {
		return nil
	}
	
	vs.closed = true
	
	if vs.bufWriter != nil {
		err := vs.bufWriter.Flush()
		if err != nil {
			return err
		}
	}
	
	if vs.manifestFile != nil {
		return vs.manifestFile.Close()
	}
	
	return nil
}

// GetStats 获取统计信息
func (vs *VersionSet) GetStats() map[string]interface{} {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	
	stats := make(map[string]interface{})
	stats["next_file_num"] = vs.nextFileNum
	stats["total_size"] = vs.currentVersion.GetTotalSize()
	stats["total_files"] = vs.currentVersion.GetFileCount()
	
	levelStats := make([]map[string]interface{}, vs.maxLevels)
	for level, files := range vs.currentVersion.Files {
		levelStats[level] = map[string]interface{}{
			"level": level,
			"files": len(files),
		}
		
		var size int64
		for _, fm := range files {
			size += fm.Size
		}
		levelStats[level]["size"] = size
	}
	
	stats["levels"] = levelStats
	
	return stats
}
