package persistence

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// WALRecordType WAL 记录类型
type WALRecordType uint8

const (
	// WALRecordHeaderSize WAL 记录头大小（4 字节类型 + 4 字节长度）
	WALRecordHeaderSize = 8

	// WALMagicNumber WAL 文件魔数
	WALMagicNumber uint32 = 0x57414C47 // "WALG"

	// WALVersion WAL 文件版本
	WALVersion uint32 = 1
)

const (
	WALRecordTypePut WALRecordType = iota + 1
	WALRecordTypeDelete
	WALRecordTypeSequence
)

// WALRecord WAL 记录
type WALRecord struct {
	Type  WALRecordType // 操作类型
	Key   []byte        // Key
	Value []byte        // Value（Delete 操作为空）
}

// WALEntry WAL 文件条目（包含校验和）
type WALEntry struct {
	Record   WALRecord
	Checksum uint32 // CRC32 校验和
}

// WALWriter WAL 写入器
type WALWriter struct {
	file                *os.File      // WAL 文件
	bufWriter           *bufio.Writer // 缓冲写入器
	mu                  sync.Mutex    // 并发控制
	seqNum              uint64        // 序列号
	totalSize           int64         // 总大小
	maxSize             int64         // 最大文件大小（用于轮转）
	syncOnWrite         bool
	closed              bool
	reqCh               chan walRequest
	wg                  sync.WaitGroup
	flushBytes          int
	flushEvery          time.Duration
	enqueueTimeoutNanos atomic.Int64
	enqueueCount        atomic.Uint64
	enqueueTimeouts     atomic.Uint64
	enqueueWaitNanos    atomic.Uint64
	enqueueMaxWaitNanos atomic.Uint64
	queueMaxLen         atomic.Uint64
}

type walRequest struct {
	recordType WALRecordType
	key        []byte
	value      []byte
	done       chan error
}

// WALReader WAL 读取器
type WALReader struct {
	file       *os.File      // WAL 文件
	bufReader  *bufio.Reader // 缓冲读取器
	currentSeq uint64        // 当前序列号
}

// NewWALWriter 创建 WAL 写入器
func NewWALWriter(filename string, maxSize int64, syncOnWrite bool) (*WALWriter, error) {
	return NewWALWriterWithConfig(filename, maxSize, syncOnWrite, 4096, 0)
}

func NewWALWriterWithConfig(filename string, maxSize int64, syncOnWrite bool, queueSize int, enqueueTimeout time.Duration) (*WALWriter, error) {
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}

	// 获取文件大小
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	if queueSize <= 0 {
		queueSize = 4096
	}
	if enqueueTimeout < 0 {
		enqueueTimeout = 0
	}

	w := &WALWriter{
		file:        file,
		bufWriter:   bufio.NewWriterSize(file, 1<<20),
		seqNum:      0,
		totalSize:   info.Size(),
		maxSize:     maxSize,
		syncOnWrite: syncOnWrite,
		reqCh:       make(chan walRequest, queueSize),
		flushBytes:  1 << 20,
		flushEvery:  2 * time.Millisecond,
	}
	w.enqueueTimeoutNanos.Store(int64(enqueueTimeout))
	w.wg.Add(1)
	go w.loop()
	return w, nil
}

// Write 写入 WAL 记录
func (w *WALWriter) Write(recordType WALRecordType, key, value []byte) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return fmt.Errorf("wal is closed")
	}
	ch := w.reqCh
	timeout := time.Duration(w.enqueueTimeoutNanos.Load())
	w.mu.Unlock()

	done := make(chan error, 1)
	req := walRequest{recordType: recordType, key: key, value: value, done: done}

	start := time.Now()
	sent := false
	var sendPanic any

	func() {
		defer func() {
			if r := recover(); r != nil {
				sendPanic = r
			}
		}()
		if timeout <= 0 {
			ch <- req
			sent = true
			return
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case ch <- req:
			sent = true
		case <-timer.C:
		}
	}()

	if sendPanic != nil {
		return fmt.Errorf("wal is closed")
	}
	if !sent {
		w.enqueueTimeouts.Add(1)
		return fmt.Errorf("wal queue full")
	}

	wait := uint64(time.Since(start).Nanoseconds())
	w.enqueueCount.Add(1)
	w.enqueueWaitNanos.Add(wait)
	for {
		cur := w.enqueueMaxWaitNanos.Load()
		if wait <= cur || w.enqueueMaxWaitNanos.CompareAndSwap(cur, wait) {
			break
		}
	}
	if ch != nil {
		ql := uint64(len(ch))
		for {
			cur := w.queueMaxLen.Load()
			if ql <= cur || w.queueMaxLen.CompareAndSwap(cur, ql) {
				break
			}
		}
	}

	return <-done
}

// Put 写入 Put 操作
func (w *WALWriter) Put(key, value []byte) error {
	return w.Write(WALRecordTypePut, key, value)
}

// Delete 写入 Delete 操作
func (w *WALWriter) Delete(key []byte) error {
	return w.Write(WALRecordTypeDelete, key, nil)
}

// Close 关闭 WAL 写入器
func (w *WALWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	close(w.reqCh)
	w.mu.Unlock()

	w.wg.Wait()

	w.mu.Lock()
	defer w.mu.Unlock()

	// 刷新缓冲区
	err := w.bufWriter.Flush()
	if err != nil {
		return err
	}

	// 同步到磁盘
	err = w.file.Sync()
	if err != nil {
		return err
	}

	// 关闭文件
	return w.file.Close()
}

func (w *WALWriter) loop() {
	defer w.wg.Done()

	var batch []walRequest
	var batchBytes int
	var batchStart time.Time

	flush := func() {
		if len(batch) == 0 {
			return
		}

		w.mu.Lock()
		err := w.writeBatchLocked(batch)
		w.mu.Unlock()

		for _, r := range batch {
			r.done <- err
		}
		batch = batch[:0]
		batchBytes = 0
		batchStart = time.Time{}
	}

	for req := range w.reqCh {
		if len(batch) == 0 {
			batchStart = time.Now()
		}
		batch = append(batch, req)
		batchBytes += 22 + len(req.key) + len(req.value)

	drain:
		for batchBytes < w.flushBytes && time.Since(batchStart) < w.flushEvery {
			select {
			case r, ok := <-w.reqCh:
				if !ok {
					flush()
					return
				}
				batch = append(batch, r)
				batchBytes += 22 + len(r.key) + len(r.value)
			default:
				break drain
			}
		}
		flush()
	}
	flush()
}

func (w *WALWriter) writeBatchLocked(batch []walRequest) error {
	for _, r := range batch {
		record := WALRecord{
			Type:  r.recordType,
			Key:   r.key,
			Value: r.value,
		}
		checksum := calculateChecksum(record)

		var header [22]byte
		header[0] = byte(record.Type)
		header[1] = 0
		binary.LittleEndian.PutUint64(header[2:10], w.seqNum)
		binary.LittleEndian.PutUint32(header[10:14], uint32(len(r.key)))
		binary.LittleEndian.PutUint32(header[14:18], uint32(len(r.value)))
		binary.LittleEndian.PutUint32(header[18:22], checksum)

		if _, err := w.bufWriter.Write(header[:]); err != nil {
			return err
		}
		if len(r.key) > 0 {
			if _, err := w.bufWriter.Write(r.key); err != nil {
				return err
			}
		}
		if len(r.value) > 0 {
			if _, err := w.bufWriter.Write(r.value); err != nil {
				return err
			}
		}

		w.seqNum++
		w.totalSize += int64(22 + len(r.key) + len(r.value))
	}

	if err := w.bufWriter.Flush(); err != nil {
		return err
	}
	if w.syncOnWrite {
		if err := w.file.Sync(); err != nil {
			return err
		}
	}
	return nil
}

func (w *WALWriter) Stats() map[string]interface{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	var qlen int
	var qcap int
	if w.reqCh != nil {
		qlen = len(w.reqCh)
		qcap = cap(w.reqCh)
	}
	return map[string]interface{}{
		"seq_num":               w.seqNum,
		"total_size":            w.totalSize,
		"sync_on_write":         w.syncOnWrite,
		"queue_len":             qlen,
		"queue_capacity":        qcap,
		"queue_max_len":         w.queueMaxLen.Load(),
		"enqueue_timeout_ms":    float64(time.Duration(w.enqueueTimeoutNanos.Load())) / float64(time.Millisecond),
		"enqueue_count":         w.enqueueCount.Load(),
		"enqueue_timeouts":      w.enqueueTimeouts.Load(),
		"enqueue_wait_ms_total": float64(w.enqueueWaitNanos.Load()) / 1e6,
		"enqueue_wait_ms_max":   float64(w.enqueueMaxWaitNanos.Load()) / 1e6,
		"enqueue_wait_ms_avg":   ratioUint64(w.enqueueWaitNanos.Load()/1e6, w.enqueueCount.Load()),
	}
}

// Rotate 轮转 WAL 文件
func (w *WALWriter) Rotate(newFilename string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 刷新并关闭当前文件
	err := w.bufWriter.Flush()
	if err != nil {
		return err
	}

	err = w.file.Sync()
	if err != nil {
		return err
	}

	err = w.file.Close()
	if err != nil {
		return err
	}

	// 重命名文件
	err = os.Rename(w.file.Name(), newFilename)
	if err != nil {
		return err
	}

	// 创建新文件
	w.file, err = os.OpenFile(w.file.Name(), os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}

	w.bufWriter = bufio.NewWriter(w.file)
	w.seqNum = 0
	w.totalSize = 0

	return nil
}

// GetSeqNum 获取当前序列号
func (w *WALWriter) GetSeqNum() uint64 {
	return w.seqNum
}

// GetTotalSize 获取总大小
func (w *WALWriter) GetTotalSize() int64 {
	return w.totalSize
}

// NewWALReader 创建 WAL 读取器
func NewWALReader(filename string) (*WALReader, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	return &WALReader{
		file:       file,
		bufReader:  bufio.NewReader(file),
		currentSeq: 0,
	}, nil
}

// ReadNext 读取下一条记录
func (r *WALReader) ReadNext() (*WALRecord, error) {
	// 读取记录头
	header := make([]byte, 22)
	_, err := io.ReadFull(r.bufReader, header)
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, err
	}

	// 解析头部
	recordType := WALRecordType(header[0])
	seqNum := binary.LittleEndian.Uint64(header[2:10])
	keyLen := binary.LittleEndian.Uint32(header[10:14])
	valueLen := binary.LittleEndian.Uint32(header[14:18])
	checksum := binary.LittleEndian.Uint32(header[18:22])

	// 更新序列号
	r.currentSeq = seqNum

	// 读取 Key
	key := make([]byte, keyLen)
	if keyLen > 0 {
		_, err := io.ReadFull(r.bufReader, key)
		if err != nil {
			return nil, err
		}
	}

	// 读取 Value
	value := make([]byte, valueLen)
	if valueLen > 0 {
		_, err := io.ReadFull(r.bufReader, value)
		if err != nil {
			return nil, err
		}
	}

	// 构建记录
	record := WALRecord{
		Type:  recordType,
		Key:   key,
		Value: value,
	}

	// 验证校验和
	calculatedChecksum := calculateChecksum(record)
	if checksum != calculatedChecksum {
		return nil, fmt.Errorf("checksum mismatch: expected %d, got %d", checksum, calculatedChecksum)
	}

	return &record, nil
}

// Close 关闭 WAL 读取器
func (r *WALReader) Close() error {
	return r.file.Close()
}

// GetCurrentSeqNum 获取当前序列号
func (r *WALReader) GetCurrentSeqNum() uint64 {
	return r.currentSeq
}

// calculateChecksum 计算校验和
func calculateChecksum(record WALRecord) uint32 {
	data := make([]byte, 1+len(record.Key)+len(record.Value))
	data[0] = byte(record.Type)
	copy(data[1:], record.Key)
	copy(data[1+len(record.Key):], record.Value)
	return crc32Checksum(data)
}

// crc32Checksum 计算 CRC32 校验和
func crc32Checksum(data []byte) uint32 {
	// 简单的 CRC32 实现（可以使用标准库的 hash/crc32）
	checksum := uint32(0)
	for _, b := range data {
		checksum = (checksum << 8) ^ uint32(b)
	}
	return checksum
}

// ReplayWAL 重放 WAL 日志
func ReplayWAL(filename string, applyFunc func(record *WALRecord) error) (uint64, error) {
	reader, err := NewWALReader(filename)
	if err != nil {
		return 0, err
	}
	defer reader.Close()

	var lastSeqNum uint64 = 0

	for {
		record, err := reader.ReadNext()
		if err != nil {
			if err == io.EOF {
				break
			}
			return 0, err
		}

		// 应用记录
		err = applyFunc(record)
		if err != nil {
			return 0, fmt.Errorf("failed to apply record: %v", err)
		}

		lastSeqNum = reader.GetCurrentSeqNum()
	}

	return lastSeqNum, nil
}

// RemoveWAL 删除 WAL 文件
func RemoveWAL(filename string) error {
	return os.Remove(filename)
}

// WALExists 检查 WAL 文件是否存在
func WALExists(filename string) bool {
	_, err := os.Stat(filename)
	return err == nil
}
