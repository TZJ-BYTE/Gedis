package persistence

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

const (
	// MaxLevels 最大层级数
	MaxLevels = 7
	
	// Level0FileThreshold Level 0 触发 Compaction 的文件数阈值
	Level0FileThreshold = 4
	
	// LevelMaxSize 每个 level 的最大大小（字节）
	LevelMaxSize = 10 << 20 // 10MB
	
	// LevelSizeFactor 每层大小增长因子
	LevelSizeFactor = 10
)

// CompactionStats Compaction 统计信息
type CompactionStats struct {
	NumCompactions int64   // Compaction 次数
	NumFilesMerged int64   // 合并的文件数
	BytesRead      int64   // 读取的字节数
	BytesWritten   int64   // 写入的字节数
	DurationMs     int64   // 耗时（毫秒）
}

// Compactor Compaction 执行器
type Compactor struct {
	mu            sync.Mutex        // 并发控制
	dbDir         string            // 数据库目录
	versionSet    *VersionSet       // 版本集合
	options       *Options          // 配置选项
	stats         CompactionStats   // 统计信息
	running       bool              // 是否正在运行
	stopChan      chan struct{}     // 停止信号
	wg            sync.WaitGroup    // 等待组
}

// NewCompactor 创建 Compactor
func NewCompactor(dbDir string, versionSet *VersionSet, options *Options) *Compactor {
	return &Compactor{
		dbDir:      dbDir,
		versionSet: versionSet,
		options:    options,
		running:    false,
		stopChan:   make(chan struct{}),
	}
}

// Start 启动后台 Compaction 线程
func (c *Compactor) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.running {
		return
	}
	
	c.running = true
	c.wg.Add(1)
	
	go func() {
		defer c.wg.Done()
		
		for {
			select {
			case <-c.stopChan:
				return
			default:
				// 检查是否需要 Compaction
				needCompaction, level := c.checkNeedCompaction()
				
				if needCompaction {
					err := c.runCompaction(level)
					if err != nil {
						fmt.Printf("Compaction error: %v\n", err)
					}
				}
			}
		}
	}()
}

// Stop 停止后台 Compaction
func (c *Compactor) Stop() {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return
	}
	
	close(c.stopChan)
	c.running = false
	c.mu.Unlock()
	
	c.wg.Wait()
}

// checkNeedCompaction 检查是否需要 Compaction
func (c *Compactor) checkNeedCompaction() (bool, int) {
	version := c.versionSet.GetCurrentVersion()
	
	// 检查 Level 0
	if len(version.Files[0]) >= Level0FileThreshold {
		return true, 0
	}
	
	// 检查其他层级
	for level := 1; level < MaxLevels; level++ {
		var levelSize int64
		for _, fm := range version.Files[level] {
			levelSize += fm.Size
		}
		
		maxSize := int64(LevelMaxSize)
		for i := 1; i < level; i++ {
			maxSize *= LevelSizeFactor
		}
		
		if levelSize > maxSize {
			return true, level
		}
	}
	
	return false, -1
}

// runCompaction 执行 Compaction
func (c *Compactor) runCompaction(level int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	version := c.versionSet.GetCurrentVersion()
	
	// 选择要合并的文件
	inputFiles := c.selectInputFiles(version, level)
	if len(inputFiles) == 0 {
		return nil
	}
	
	// 更新统计
	c.stats.NumCompactions++
	c.stats.NumFilesMerged += int64(len(inputFiles))
	
	// 计算 key 范围
	smallestKey, largestKey := c.computeKeyRange(inputFiles)
	
	// 选择目标层级的重叠文件
	outputLevel := level + 1
	if outputLevel >= MaxLevels {
		// 到达最大层级，不再继续合并
		return c.finishCompaction(inputFiles, level, nil)
	}
	
	overlapFiles := c.findOverlapFiles(version.Files[outputLevel], smallestKey, largestKey)
	
	// 执行合并
	outputFileNum, err := c.mergeFiles(inputFiles, overlapFiles, outputLevel, smallestKey, largestKey)
	if err != nil {
		return err
	}
	
	// 更新版本和 MANIFEST
	err = c.finishCompaction(inputFiles, level, []*FileMetadata{outputFileNum})
	if err != nil {
		return err
	}
	
	return nil
}

// selectInputFiles 选择要合并的文件
func (c *Compactor) selectInputFiles(version *Version, level int) []*FileMetadata {
	if level == 0 {
		// Level 0 选择所有文件（因为可能有重叠）
		return version.Files[0]
	}
	
	// 其他层级选择最小的几个文件
	files := version.Files[level]
	if len(files) == 0 {
		return nil
	}
	
	// 按文件大小排序
	sort.Slice(files, func(i, j int) bool {
		return files[i].Size < files[j].Size
	})
	
	// 选择前几个文件
	numToCompact := len(files) / 4
	if numToCompact < 1 {
		numToCompact = 1
	}
	
	return files[:numToCompact]
}

// computeKeyRange 计算 key 范围
func (c *Compactor) computeKeyRange(files []*FileMetadata) ([]byte, []byte) {
	if len(files) == 0 {
		return nil, nil
	}
	
	smallest := files[0].SmallestKey
	largest := files[0].LargestKey
	
	for _, fm := range files {
		if bytes.Compare(fm.SmallestKey, smallest) < 0 {
			smallest = fm.SmallestKey
		}
		if bytes.Compare(fm.LargestKey, largest) > 0 {
			largest = fm.LargestKey
		}
	}
	
	return smallest, largest
}

// findOverlapFiles 查找与 key 范围重叠的文件
func (c *Compactor) findOverlapFiles(files []*FileMetadata, smallest, largest []byte) []*FileMetadata {
	var overlap []*FileMetadata
	
	for _, fm := range files {
		// 检查是否重叠
		if bytes.Compare(fm.LargestKey, smallest) >= 0 && 
		   bytes.Compare(fm.SmallestKey, largest) <= 0 {
			overlap = append(overlap, fm)
		}
	}
	
	return overlap
}

// mergeFiles 合并文件
func (c *Compactor) mergeFiles(inputFiles, overlapFiles []*FileMetadata, outputLevel int, smallestKey, largestKey []byte) (*FileMetadata, error) {
	// 生成输出文件编号
	fileNum := c.versionSet.GetNextFileNum()
	sstableDir := filepath.Join(c.dbDir, "sstable")
	filename := filepath.Join(sstableDir, fmt.Sprintf("%06d.sstable", fileNum))
	
	// 创建 SSTable Builder
	builder, err := NewSSTableBuilder(filename, c.options)
	if err != nil {
		return nil, err
	}
	defer builder.Abort()
	
	// 收集所有输入文件的 Reader
	readers := make([]*SSTableReader, 0, len(inputFiles)+len(overlapFiles))
	
	// 打开输入文件
	allFiles := append(inputFiles, overlapFiles...)
	for _, fm := range allFiles {
		sstablePath := filepath.Join(sstableDir, fmt.Sprintf("%06d.sstable", fm.FileNum))
		reader, err := OpenSSTableForRead(sstablePath, c.options)
		if err != nil {
			return nil, err
		}
		readers = append(readers, reader)
	}
	
	// 使用归并排序合并所有文件
	iterators := make([]Iterator, len(readers))
	for i, reader := range readers {
		iterators[i] = reader.NewIterator()
	}
	
	mergedIter := newMergeIterator(iterators)
	defer mergedIter.Release()
	
	// 遍历并写入输出文件
	prevKey := []byte(nil)
	for mergedIter.First(); mergedIter.Valid(); mergedIter.Next() {
		key := mergedIter.Key()
		value := mergedIter.Value()
		
		// 跳过重复的 key（只保留最新的）
		if bytes.Equal(key, prevKey) {
			continue
		}
		prevKey = key
		
		err := builder.Add(key, value)
		if err != nil {
			return nil, err
		}
	}
	
	// 完成构建
	err = builder.Finish()
	if err != nil {
		return nil, err
	}
	
	// 获取输出文件信息
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}
	
	// 创建文件元数据
	outputFM := &FileMetadata{
		FileNum:     fileNum,
		Size:        info.Size(),
		SmallestKey: smallestKey,
		LargestKey:  largestKey,
		Level:       outputLevel,
	}
	
	// 关闭所有 Reader
	for _, reader := range readers {
		reader.Close()
	}
	
	return outputFM, nil
}

// finishCompaction 完成 Compaction
func (c *Compactor) finishCompaction(inputFiles []*FileMetadata, inputLevel int, outputFiles []*FileMetadata) error {
	// 从旧层级移除输入文件
	for _, fm := range inputFiles {
		c.versionSet.GetCurrentVersion().RemoveFile(inputLevel, fm.FileNum)
		err := c.versionSet.LogDeleteFile(inputLevel, fm.FileNum)
		if err != nil {
			return err
		}
		
		// 删除物理文件
		sstablePath := filepath.Join(c.dbDir, "sstable", fmt.Sprintf("%06d.sstable", fm.FileNum))
		os.Remove(sstablePath)
	}
	
	// 添加输出文件到新层级
	for _, fm := range outputFiles {
		c.versionSet.GetCurrentVersion().AddFile(fm.Level, fm)
		err := c.versionSet.LogAddFile(fm)
		if err != nil {
			return err
		}
	}
	
	return nil
}

// GetStats 获取统计信息
func (c *Compactor) GetStats() CompactionStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// MergeIterator 归并迭代器
type MergeIterator struct {
	iterators []Iterator
	current   int
	valid     bool
}

func newMergeIterator(iterators []Iterator) *MergeIterator {
	mi := &MergeIterator{
		iterators: iterators,
		current:   -1,
		valid:     false,
	}
	mi.seekMin()
	return mi
}

func (mi *MergeIterator) seekMin() {
	mi.current = -1
	minKey := []byte(nil)
	
	for i, iter := range mi.iterators {
		if iter.Valid() {
			key := iter.Key()
			if mi.current == -1 || bytes.Compare(key, minKey) < 0 {
				minKey = key
				mi.current = i
			}
		}
	}
	
	mi.valid = mi.current != -1
}

// SeekToFirst 定位到第一个元素（符合 Iterator 接口契约）
func (mi *MergeIterator) SeekToFirst() bool {
	mi.First()
	return mi.Valid()
}

func (mi *MergeIterator) First() {
	for _, iter := range mi.iterators {
		iter.First()
	}
	mi.seekMin()
}

func (mi *MergeIterator) Valid() bool {
	return mi.valid
}

func (mi *MergeIterator) Key() []byte {
	if mi.current == -1 {
		return nil
	}
	return mi.iterators[mi.current].Key()
}

func (mi *MergeIterator) Value() []byte {
	if mi.current == -1 {
		return nil
	}
	return mi.iterators[mi.current].Value()
}

func (mi *MergeIterator) Next() bool {
	if mi.current == -1 {
		return false
	}
	
	mi.iterators[mi.current].Next()
	mi.seekMin()
	return mi.valid
}

func (mi *MergeIterator) Release() {
	for _, iter := range mi.iterators {
		iter.Release()
	}
}
