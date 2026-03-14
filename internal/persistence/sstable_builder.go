package persistence

import (
	"encoding/binary"
	"os"
)

// SSTableBuilder SSTable 构建器
type SSTableBuilder struct {
	file         *os.File        // 文件句柄
	filename     string          // 文件名
	options      *Options        // 配置选项
	blockBuilder *BlockBuilder   // Data Block Builder
	dataBlocks   []BlockHandle   // 数据 Block 句柄
	indexBlock   *BlockBuilder   // Index Block Builder
	firstKey     []byte          // 当前 Block 的第一个 key
	lastKey      []byte          // 上一个 key（用于判断是否需要添加到 Index）
	entryCount   int             // 条目计数
	fileSize     int64           // 当前文件大小
	closed       bool            // 是否已关闭
	bloomFilter  *BloomFilter    // Bloom Filter
}

// NewSSTableBuilder 创建新的 SSTable Builder
func NewSSTableBuilder(filename string, options *Options) (*SSTableBuilder, error) {
	if options == nil {
		options = DefaultOptions()
	}

	// 创建文件
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, err
	}

	// 创建 Bloom Filter
	bloomFilter := NewBloomFilter(10000, 0.001) // 默认 1 万条目，0.1% 假阳性率

	return &SSTableBuilder{
		file:         file,
		filename:     filename,
		options:      options,
		blockBuilder: NewBlockBuilder(options),
		dataBlocks:   make([]BlockHandle, 0),
		indexBlock:   NewBlockBuilder(options),
		firstKey:     nil,
		lastKey:      nil,
		entryCount:   0,
		fileSize:     0,
		closed:       false,
		bloomFilter:  bloomFilter,
	}, nil
}

// Add 添加键值对到 SSTable
func (b *SSTableBuilder) Add(key, value []byte) error {
	if b.closed {
		return ErrBuilderClosed
	}

	// 如果 Block 满了，先刷写
	if b.blockBuilder.CurrentSizeEstimate() >= b.options.BlockSize {
		if err := b.flushBlock(); err != nil {
			return err
		}
	}

	// 如果是当前 Block 的第一个条目，记录 firstKey
	if b.blockBuilder.Empty() {
		b.firstKey = append(b.firstKey[:0], key...)
	}

	// 添加到 Data Block
	b.blockBuilder.Add(key, value)

	// 添加到 Bloom Filter
	b.bloomFilter.Add(key)

	b.entryCount++
	b.lastKey = append(b.lastKey[:0], key...)

	return nil
}

// flushBlock 刷写当前 Data Block 到磁盘
func (b *SSTableBuilder) flushBlock() error {
	if b.blockBuilder.Empty() {
		return nil
	}

	// 完成 Block 构建
	blockData := b.blockBuilder.Finish()

	// 写入文件
	offset := uint64(b.fileSize)
	_, err := b.file.Write(blockData)
	if err != nil {
		return err
	}

	// 记录 Block 句柄
	handle := BlockHandle{
		offset: offset,
		size:   uint64(len(blockData)),
	}
	b.dataBlocks = append(b.dataBlocks, handle)
	b.fileSize += int64(len(blockData))

	// 将这个 Block 的第一个 key 添加到 Index Block
	if b.firstKey != nil && len(b.firstKey) > 0 {
		b.indexBlock.Add(b.firstKey, handle.Encode())
	}

	// 重置 Block Builder（但不重置 firstKey，它会在下次 Add 时被覆盖）
	b.blockBuilder.Reset()

	return nil
}

// NumEntries 返回条目数量
func (b *SSTableBuilder) NumEntries() int {
	return b.entryCount
}

// Finish 完成 SSTable 构建
func (b *SSTableBuilder) Finish() error {
	if b.closed {
		return ErrBuilderClosed
	}

	// 先刷写最后一个 Data Block（这会将 firstKey 添加到 Index Block）
	if err := b.flushBlock(); err != nil {
		return err
	}

	// 如果没有数据，也要创建一个空的 Data Block
	if len(b.dataBlocks) == 0 {
		if err := b.flushBlock(); err != nil {
			return err
		}
	}

	// 完成 Index Block
	if !b.indexBlock.Empty() {
		indexData := b.indexBlock.Finish()
		
		// 写入 Index Block
		indexOffset := uint64(b.fileSize)
		_, err := b.file.Write(indexData)
		if err != nil {
			return err
		}
		b.fileSize += int64(len(indexData)) // 更新 fileSize
		
		indexHandle := BlockHandle{
			offset: indexOffset,
			size:   uint64(len(indexData)),
		}
		
		// 写入 Meta Block（包含 Bloom Filter）
		metaData := b.bloomFilter.Encode()
		metaOffset := uint64(b.fileSize) // 使用更新后的 fileSize
		_, err = b.file.Write(metaData)
		if err != nil {
			return err
		}
		b.fileSize += int64(len(metaData)) // 更新 fileSize
		
		metaHandle := BlockHandle{
			offset: metaOffset,
			size:   uint64(len(metaData)),
		}
		
		// 写入 Footer
		err = b.writeFooter(metaHandle, indexHandle)
		if err != nil {
			return err
		}
	} else {
		// 空 Index Block
		emptyBlock := make([]byte, 8) // 最小区块大小
		indexOffset := uint64(b.fileSize)
		_, err := b.file.Write(emptyBlock)
		if err != nil {
			return err
		}
		b.fileSize += int64(len(emptyBlock))

		indexHandle := BlockHandle{
			offset: indexOffset,
			size:   uint64(len(emptyBlock)),
		}

		metaHandle := BlockHandle{
			offset: uint64(b.fileSize),
			size:   0,
		}
		
		b.fileSize += 0 // Meta Block 为空

		err = b.writeFooter(metaHandle, indexHandle)
		if err != nil {
			return err
		}
	}

	b.closed = true
	return b.file.Close()
}

// writeFooter 写入 Footer
func (b *SSTableBuilder) writeFooter(metaHandle, indexHandle BlockHandle) error {
	footerData := make([]byte, FooterSize)
	pos := 0

	// 编码 meta index handle
	encoded := metaHandle.Encode()
	n := copy(footerData[pos:], encoded)
	pos += n

	// 编码 index handle
	encoded = indexHandle.Encode()
	n = copy(footerData[pos:], encoded)
	pos += n

	// padding (如果需要)
	// Footer 格式：[meta_handle][index_handle][padding][magic_number]
	// magic_number 在最后 8 字节，所以 padding 自动填充

	// 写入 magic number（最后 8 字节）
	binary.LittleEndian.PutUint64(footerData[40:], TableMagicNumber)

	// 写入文件末尾
	_, err := b.file.Write(footerData)
	if err != nil {
		return err
	}

	return nil
}

// Abort 中止构建，删除文件
func (b *SSTableBuilder) Abort() {
	b.closed = true
	if b.file != nil {
		b.file.Close()
		os.Remove(b.filename)
	}
}

// FileSize 返回当前文件大小
func (b *SSTableBuilder) FileSize() int64 {
	return b.fileSize
}

// 错误定义
var (
	ErrBuilderClosed = &os.PathError{Op: "write", Path: "builder", Err: os.ErrInvalid}
)
