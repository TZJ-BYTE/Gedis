package persistence

import (
	"bytes"
	"encoding/binary"
)

// Block SSTable 中的数据块
type Block struct {
	data []byte
}

// NewBlock 从字节数据创建 Block
func NewBlock(data []byte) *Block {
	return &Block{
		data: data,
	}
}

// Data 获取原始数据
func (b *Block) Data() []byte {
	return b.data
}

// Size 返回 Block 大小
func (b *Block) Size() int {
	return len(b.data)
}

// BlockBuilder Block 构建器（简化版本）
type BlockBuilder struct {
	options    *Options
	data       []byte   // 数据缓冲区
	entryCount int      // 条目数量
	lastKey    []byte   // 上一个 key
}

// NewBlockBuilder 创建新的 Block Builder
func NewBlockBuilder(options *Options) *BlockBuilder {
	if options == nil {
		options = DefaultOptions()
	}
	
	return &BlockBuilder{
		options:    options,
		data:       make([]byte, 0, options.BlockSize),
		entryCount: 0,
		lastKey:    nil,
	}
}

// Reset 重置 Builder
func (bb *BlockBuilder) Reset() {
	bb.data = bb.data[:0]
	bb.entryCount = 0
	bb.lastKey = nil
}

// Add 添加键值对到 Block
// 格式：[key_len:varint][key:N][value_len:varint][value:M]
func (bb *BlockBuilder) Add(key, value []byte) {
	var buf bytes.Buffer
	
	// 写入 key length
	encoded := make([]byte, binary.MaxVarintLen64)
	n := binary.PutUvarint(encoded, uint64(len(key)))
	buf.Write(encoded[:n])
	
	// 写入 key
	buf.Write(key)
	
	// 写入 value length
	n = binary.PutUvarint(encoded, uint64(len(value)))
	buf.Write(encoded[:n])
	
	// 写入 value
	buf.Write(value)
	
	// 追加到数据区
	bb.data = append(bb.data, buf.Bytes()...)
	
	// 更新状态
	bb.entryCount++
	bb.lastKey = append(bb.lastKey[:0], key...)
}

// CurrentSizeEstimate 估算当前大小
func (bb *BlockBuilder) CurrentSizeEstimate() int {
	return len(bb.data)
}

// Empty 检查是否为空
func (bb *BlockBuilder) Empty() bool {
	return bb.entryCount == 0
}

// Finish 完成 Block 构建，返回完整的 Block 数据
func (bb *BlockBuilder) Finish() []byte {
	if bb.Empty() {
		return nil
	}
	
	// 简单的复制返回
	result := make([]byte, len(bb.data))
	copy(result, bb.data)
	return result
}

// NewIterator 创建 Block 迭代器
func (bb *BlockBuilder) NewIterator() *BlockIterator {
	data := bb.Finish()
	return NewBlockIterator(data)
}

// BlockIterator Block 数据迭代器
type BlockIterator struct {
	data       []byte
	pos        int
	currentKey []byte
	currentVal []byte
	valid      bool
	err        error
}

// NewBlockIterator 创建 Block 迭代器
func NewBlockIterator(data []byte) *BlockIterator {
	if data == nil || len(data) == 0 {
		return &BlockIterator{valid: false}
	}
	
	return &BlockIterator{
		data:  data,
		pos:   0,
		valid: false,
	}
}

// SeekToFirst 定位到第一个元素
func (it *BlockIterator) SeekToFirst() {
	it.pos = 0
	it.valid = true
	// 读取第一个条目
	it.readCurrentEntry()
}

// readCurrentEntry 读取当前条目
func (it *BlockIterator) readCurrentEntry() {
	if it.pos >= len(it.data) {
		it.valid = false
		return
	}
	
	data := it.data[it.pos:]
	pos := 0
	
	// 读取 key length
	keyLen, n := binary.Uvarint(data[pos:])
	if n <= 0 {
		it.valid = false
		return
	}
	pos += n
	
	// 读取 key
	if pos+int(keyLen) > len(data) {
		it.valid = false
		return
	}
	it.currentKey = append(it.currentKey[:0], data[pos:pos+int(keyLen)]...)
	pos += int(keyLen)
	
	// 读取 value length
	valLen, n := binary.Uvarint(data[pos:])
	if n <= 0 {
		it.valid = false
		return
	}
	pos += n
	
	// 读取 value
	if pos+int(valLen) > len(data) {
		it.valid = false
		return
	}
	it.currentVal = append(it.currentVal[:0], data[pos:pos+int(valLen)]...)
	
	// 移动到下一个条目的位置（相对于数据起始位置）
	it.pos += pos + int(valLen)
}

// Seek 定位到第一个 >= key 的元素
func (it *BlockIterator) Seek(key []byte) bool {
	it.SeekToFirst()
	
	for it.Valid() {
		if bytes.Compare(it.Key(), key) >= 0 {
			return true
		}
		it.Next()
	}
	
	return false
}

// Key 获取当前 key
func (it *BlockIterator) Key() []byte {
	return it.currentKey
}

// Value 获取当前 value
func (it *BlockIterator) Value() []byte {
	return it.currentVal
}

// Valid 检查是否有效
func (it *BlockIterator) Valid() bool {
	return it.valid && it.err == nil
}

// Next 移动到下一个元素
func (it *BlockIterator) Next() {
	if !it.valid || it.err != nil {
		return
	}
	
	// 读取下一个条目
	it.readCurrentEntry()
}

// Prev 移动到前一个元素（需要额外实现）
func (it *BlockIterator) Prev() bool {
	// TODO: 实现反向遍历
	return false
}

// Error 获取错误
func (it *BlockIterator) Error() error {
	return it.err
}

// Release 释放资源
func (it *BlockIterator) Release() {
	it.data = nil
	it.currentKey = nil
	it.currentVal = nil
	it.valid = false
}
