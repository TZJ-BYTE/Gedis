package vlog

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ValueLogGC 垃圾回收器
type ValueLogGC struct {
	dirPath      string
	gcThreshold  float64                                            // 垃圾回收阈值 (例如 0.5 表示 50% 空间无效时回收)
	checkKeyFunc func(key []byte, vp *ValuePointer) (bool, error)   // 回调函数：检查 Key 指向的 VP 是否依然有效
	rewriteFunc  func(key, value []byte, oldVP *ValuePointer) error // 回调函数：重写有效数据

	mu       sync.Mutex
	progress gcProgress
}

type gcProgress struct {
	fid       uint32
	phase     uint8
	offset    int64
	validSize int64
	fileSize  int64
}

const (
	gcPhaseScan uint8 = iota
	gcPhaseRewrite
)

// NewValueLogGC 创建 GC
func NewValueLogGC(dirPath string, threshold float64, checkFunc func([]byte, *ValuePointer) (bool, error), rewriteFunc func([]byte, []byte, *ValuePointer) error) *ValueLogGC {
	return &ValueLogGC{
		dirPath:      dirPath,
		gcThreshold:  threshold,
		checkKeyFunc: checkFunc,
		rewriteFunc:  rewriteFunc,
	}
}

// RunGC 执行一次 GC
// 策略：
// 1. 遍历所有 vLog 文件（除了当前活跃的）
// 2. 对每个文件，随机采样或全量扫描，计算有效数据比例
// 3. 如果有效比例低于阈值，则重写该文件：
//   - 读取有效数据
//   - 写入当前活跃 vLog
//   - 更新 LSM Tree 中的索引
//   - 删除旧文件
func (gc *ValueLogGC) RunGC() error {
	return gc.RunGCWithBudget(0, 0, 0)
}

func (gc *ValueLogGC) RunGCWithBudget(scanMaxBytes int64, rewriteMaxBytes int64, maxDuration time.Duration) error {
	start := time.Now()

	gc.mu.Lock()
	p := gc.progress
	gc.mu.Unlock()

	files, err := gc.listFiles()
	if err != nil {
		return err
	}

	if len(files) <= 1 {
		return nil // 只有一个文件（或者是空的），无需 GC
	}

	active := files[len(files)-1]

	var fid uint32
	if p.fid != 0 {
		fid = p.fid
	} else {
		fid = files[0]
		if fid == active {
			return nil
		}
		p = gcProgress{fid: fid, phase: gcPhaseScan}
	}

	if fid == active {
		gc.mu.Lock()
		gc.progress = gcProgress{}
		gc.mu.Unlock()
		return nil
	}

	switch p.phase {
	case gcPhaseScan:
		err = gc.scanFileWithBudget(&p, scanMaxBytes, maxDuration, start)
	case gcPhaseRewrite:
		err = gc.rewriteFileWithBudget(&p, rewriteMaxBytes, maxDuration, start)
	default:
		p = gcProgress{fid: fid, phase: gcPhaseScan}
		err = gc.scanFileWithBudget(&p, scanMaxBytes, maxDuration, start)
	}

	gc.mu.Lock()
	gc.progress = p
	gc.mu.Unlock()
	return err
}

// listFiles 获取所有 vLog 文件 ID，按从小到大排序
func (gc *ValueLogGC) listFiles() ([]uint32, error) {
	entries, err := os.ReadDir(gc.dirPath)
	if err != nil {
		return nil, err
	}

	var fids []uint32
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".vlog") {
			var fid uint32
			_, err := fmt.Sscanf(entry.Name(), "%06d.vlog", &fid)
			if err == nil {
				fids = append(fids, fid)
			}
		}
	}
	sort.Slice(fids, func(i, j int) bool { return fids[i] < fids[j] })
	return fids, nil
}

func (gc *ValueLogGC) scanFileWithBudget(p *gcProgress, maxBytes int64, maxDuration time.Duration, start time.Time) error {
	filename := filepath.Join(gc.dirPath, fmt.Sprintf("%06d.vlog", p.fid))
	file, err := os.Open(filename)
	if err != nil {
		*p = gcProgress{}
		return nil
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		*p = gcProgress{}
		return nil
	}
	fileSize := info.Size()
	p.fileSize = fileSize

	offset := p.offset
	header := make([]byte, 8)
	for offset < fileSize {
		if maxDuration > 0 && time.Since(start) >= maxDuration {
			p.offset = offset
			return nil
		}
		if maxBytes > 0 && offset-p.offset >= maxBytes {
			p.offset = offset
			return nil
		}

		_, err := file.ReadAt(header, offset)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		keyLen := binary.BigEndian.Uint32(header[0:4])
		valLen := binary.BigEndian.Uint32(header[4:8])
		entrySize := 8 + int64(keyLen) + int64(valLen)

		key := make([]byte, keyLen)
		_, err = file.ReadAt(key, offset+8)
		if err != nil {
			return err
		}

		vp := &ValuePointer{
			Fid:    p.fid,
			Len:    uint32(entrySize),
			Offset: offset,
		}
		isValid, err := gc.checkKeyFunc(key, vp)
		if err != nil {
			return err
		}
		if isValid {
			p.validSize += entrySize
		}

		offset += entrySize
	}

	p.offset = offset
	if p.offset < fileSize {
		return nil
	}

	if fileSize == 0 {
		*p = gcProgress{}
		return nil
	}

	ratio := float64(p.validSize) / float64(fileSize)
	if ratio < gc.gcThreshold {
		if gc.rewriteFunc == nil {
			*p = gcProgress{}
			return fmt.Errorf("GC found garbage in file %d, but rewriteFunc is not set", p.fid)
		}
		p.phase = gcPhaseRewrite
		p.offset = 0
		return nil
	}

	*p = gcProgress{}
	return nil
}

func (gc *ValueLogGC) rewriteFileWithBudget(p *gcProgress, maxBytes int64, maxDuration time.Duration, start time.Time) error {
	filename := filepath.Join(gc.dirPath, fmt.Sprintf("%06d.vlog", p.fid))
	file, err := os.Open(filename)
	if err != nil {
		*p = gcProgress{}
		return nil
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		*p = gcProgress{}
		return nil
	}
	fileSize := info.Size()

	offset := p.offset
	header := make([]byte, 8)
	for offset < fileSize {
		if maxDuration > 0 && time.Since(start) >= maxDuration {
			p.offset = offset
			return nil
		}
		if maxBytes > 0 && offset-p.offset >= maxBytes {
			p.offset = offset
			return nil
		}

		_, err := file.ReadAt(header, offset)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		keyLen := binary.BigEndian.Uint32(header[0:4])
		valLen := binary.BigEndian.Uint32(header[4:8])
		entrySize := 8 + int64(keyLen) + int64(valLen)

		key := make([]byte, keyLen)
		_, err = file.ReadAt(key, offset+8)
		if err != nil {
			return err
		}

		vp := &ValuePointer{
			Fid:    p.fid,
			Len:    uint32(entrySize),
			Offset: offset,
		}
		isValid, err := gc.checkKeyFunc(key, vp)
		if err != nil {
			return err
		}

		if isValid {
			value := make([]byte, valLen)
			_, err = file.ReadAt(value, offset+8+int64(keyLen))
			if err != nil {
				return err
			}
			if err := gc.rewriteFunc(key, value, vp); err != nil {
				return fmt.Errorf("failed to rewrite entry during GC: %v", err)
			}
		}

		offset += entrySize
	}

	p.offset = offset
	if p.offset < fileSize {
		return nil
	}

	file.Close()
	if err := os.Remove(filename); err != nil {
		return fmt.Errorf("failed to remove compacted file %s: %v", filename, err)
	}

	*p = gcProgress{}
	return nil
}
