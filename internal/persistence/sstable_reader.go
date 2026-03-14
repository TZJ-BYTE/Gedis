package persistence

import (
	"bytes"
	"encoding/binary"
	"os"
)

// SSTableReader SSTable 读取器
type SSTableReader struct {
	file       *os.File        // 文件句柄
	filename   string          // 文件名
	options    *Options        // 配置选项
	footer     *Footer         // Footer 信息
	indexBlock *Block          // Index Block（常驻内存）
	dataCache  map[uint64]*Block // Data Block 缓存（简化版）
	bloomFilter *BloomFilter   // Bloom Filter（可选）
	blockCache *BlockCache     // Block Cache（LRU）
}

// OpenSSTableForRead 打开 SSTable 用于读取
func OpenSSTableForRead(filename string, options *Options) (*SSTableReader, error) {
	if options == nil {
		options = DefaultOptions()
	}
	
	file, err := os.OpenFile(filename, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	
	reader := &SSTableReader{
		file:       file,
		filename:   filename,
		options:    options,
		dataCache:  make(map[uint64]*Block),
		blockCache: NewBlockCache(int64(options.CacheSize)),
	}
	
	// 读取 Footer
	if err := reader.readFooter(); err != nil {
		file.Close()
		return nil, err
	}
	
	// 预加载 Index Block 到内存
	if err := reader.loadIndexBlock(); err != nil {
		file.Close()
		return nil, err
	}

	// 加载 Meta Block（包含 Bloom Filter）
	if err := reader.loadMetaBlock(); err != nil {
		// Meta Block 可选，失败不影响读取
	}

	return reader, nil
}

// NewSSTableReader 创建新的 SSTable Reader（内部使用）
func NewSSTableReader(filename string, file *os.File, options *Options) (*SSTableReader, error) {
	r := &SSTableReader{
		file:       file,
		filename:   filename,
		options:    options,
		dataCache:  make(map[uint64]*Block),
		blockCache: NewBlockCache(int64(options.CacheSize)),
	}

	return r, nil
}

// readFooter 读取文件 Footer
func (r *SSTableReader) readFooter() error {
	// 获取文件大小
	fileInfo, err := r.file.Stat()
	if err != nil {
		return err
	}
	fileSize := fileInfo.Size()

	if fileSize < FooterSize {
		return ErrInvalidFormat
	}

	// 从文件末尾读取 Footer
	footerData := make([]byte, FooterSize)
	_, err = r.file.ReadAt(footerData, fileSize-FooterSize)
	if err != nil {
		return err
	}

	// 验证魔数
	magic := binary.LittleEndian.Uint64(footerData[40:])
	if magic != TableMagicNumber {
		return ErrInvalidFormat
	}

	// 解析 Footer
	footer := &Footer{}
	
	n := 0
	
	// 解码 meta index handle
	var offset, size uint64
	offset, n = binary.Uvarint(footerData[n:n+10])
	if n <= 0 {
		return ErrInvalidFormat
	}
	size, m := binary.Uvarint(footerData[n : n+10])
	if m <= 0 {
		return ErrInvalidFormat
	}
	footer.metaIndexHandle = BlockHandle{offset: offset, size: size}
	n += m
	
	// 解码 index handle
	offset, m = binary.Uvarint(footerData[n : n+10]) // 使用 m 接收新的偏移
	if m <= 0 {
		return ErrInvalidFormat
	}
	n += m
	
	size, m = binary.Uvarint(footerData[n : n+10])
	if m <= 0 {
		return ErrInvalidFormat
	}
	n += m
	
	footer.indexHandle = BlockHandle{offset: offset, size: size}
	
	r.footer = footer
	return nil
}

// loadIndexBlock 加载 Index Block 到内存
func (r *SSTableReader) loadIndexBlock() error {
	if r.footer == nil {
		return ErrInvalidFormat
	}
	
	handle := r.footer.indexHandle
	
	// 读取 Index Block 数据
	data := make([]byte, handle.size)
	_, err := r.file.ReadAt(data, int64(handle.offset))
	if err != nil {
		return err
	}
	
	r.indexBlock = NewBlock(data)
	return nil
}

// Get 获取指定 key 的值
func (r *SSTableReader) Get(key []byte) ([]byte, bool) {
	// 1. 先检查 Bloom Filter（如果有）
	if r.bloomFilter != nil && !r.bloomFilter.MayContain(key) {
		return nil, false // 肯定不存在
	}

	// 2. 在 Index Block 中找到目标 Data Block
	blockHandle, found := r.findDataBlock(key)
	if !found {
		return nil, false
	}

	// 3. 从缓存中获取 Block
	block, cached := r.getBlockFromCache(blockHandle.offset)
	if !cached {
		// 4. 缓存未命中，从磁盘读取
		data := make([]byte, blockHandle.size)
		_, err := r.file.ReadAt(data, int64(blockHandle.offset))
		if err != nil {
			return nil, false
		}

		block = NewBlock(data)

		// 5. 加入缓存
		r.putBlockToCache(blockHandle.offset, block, len(data))
	}

	// 6. 在 Block 中查找 key
	iter := NewBlockIterator(block.Data())

	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		if bytes.Equal(iter.Key(), key) {
			value := make([]byte, len(iter.Value()))
			copy(value, iter.Value())
			return value, true
		}
	}

	return nil, false
}

// findDataBlock 在 Index Block 中查找目标 Data Block
func (r *SSTableReader) findDataBlock(key []byte) (BlockHandle, bool) {
	if r.indexBlock == nil || r.indexBlock.Data() == nil {
		return BlockHandle{}, false
	}
	
	iter := NewBlockIterator(r.indexBlock.Data())
	
	var targetHandle BlockHandle
	found := false
	
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		indexKey := iter.Key()
		
		// 如果 index key > key，说明前一个 block 是目标
		if bytes.Compare(indexKey, key) > 0 {
			break
		}
		
		// 解码 BlockHandle
		handleData := iter.Value()
		if len(handleData) == 0 {
			continue
		}
		
		var handle BlockHandle
		_, err := handle.Decode(handleData)
		if err != nil {
			continue
		}
		
		targetHandle = handle
		found = true
	}
	
	return targetHandle, found
}

// getFromBlock 从 Data Block 中获取数据
func (r *SSTableReader) getFromBlock(handle BlockHandle, key []byte) ([]byte, bool) {
	// 检查缓存
	block, cached := r.dataCache[handle.offset]
	if !cached {
		// 读取 Block 数据
		data := make([]byte, handle.size)
		_, err := r.file.ReadAt(data, int64(handle.offset))
		if err != nil {
			return nil, false
		}
		
		block = NewBlock(data)
		
		// 简单的缓存策略：总是保留（实际应该用 LRU）
		if len(r.dataCache) < 100 { // 限制缓存数量
			r.dataCache[handle.offset] = block
		}
	}
	
	// 在 Block 中查找 key
	iter := NewBlockIterator(block.Data())
	
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		if bytes.Equal(iter.Key(), key) {
			value := make([]byte, len(iter.Value()))
			copy(value, iter.Value())
			return value, true
		}
	}
	
	return nil, false
}

// NewIterator 创建 SSTable 迭代器
func (r *SSTableReader) NewIterator() *SSTableIterator {
	return &SSTableIterator{
		reader: r,
		valid:  false,
	}
}

// loadMetaBlock 加载 Meta Block（包含 Bloom Filter）
func (r *SSTableReader) loadMetaBlock() error {
	if r.footer == nil || r.footer.metaIndexHandle.size == 0 {
		return nil // 没有 Meta Block
	}

	handle := r.footer.metaIndexHandle

	// 读取 Meta Block 数据
	data := make([]byte, handle.size)
	_, err := r.file.ReadAt(data, int64(handle.offset))
	if err != nil {
		return err
	}

	// 尝试解码 Bloom Filter
	bf, err := DecodeBloomFilter(data)
	if err != nil {
		return err
	}

	r.bloomFilter = bf
	return nil
}

// Close 关闭 SSTable Reader
func (r *SSTableReader) Close() error {
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

// getBlockFromCache 从缓存中获取 Block
func (r *SSTableReader) getBlockFromCache(offset uint64) (*Block, bool) {
	// 先尝试 LRU Cache
	if r.blockCache != nil {
		if val, ok := r.blockCache.Get(offset); ok {
			if block, ok := val.(*Block); ok {
				return block, true
			}
		}
	}

	// 回退到旧的 dataCache（向后兼容）
	block, cached := r.dataCache[offset]
	return block, cached
}

// putBlockToCache 将 Block 放入缓存
func (r *SSTableReader) putBlockToCache(offset uint64, block *Block, size int) {
	// 优先使用 LRU Cache
	if r.blockCache != nil {
		r.blockCache.Put(offset, block, size)
	}

	// 同时保留旧的 dataCache（向后兼容）
	if len(r.dataCache) < 100 { // 限制缓存数量
		r.dataCache[offset] = block
	}
}

// SSTableIterator SSTable 迭代器
type SSTableIterator struct {
	reader  *SSTableReader
	current *BlockIterator
	valid   bool
	err     error
}

// SeekToFirst 定位到第一个元素
func (it *SSTableIterator) SeekToFirst() {
	it.First()
}

// First 定位到第一个元素
func (it *SSTableIterator) First() bool {
	if it.reader.indexBlock == nil {
		it.valid = false
		return false
	}
	
	// 在 Index Block 中找到第一个 Data Block
	indexIter := NewBlockIterator(it.reader.indexBlock.Data())
	indexIter.SeekToFirst()
	
	if !indexIter.Valid() {
		it.valid = false
		return false
	}
	
	// 解码第一个 BlockHandle
	handleData := indexIter.Value()
	var handle BlockHandle
	_, err := handle.Decode(handleData)
	if err != nil {
		it.err = err
		it.valid = false
		return false
	}
	
	// 读取第一个 Data Block
	data := make([]byte, handle.size)
	_, err = it.reader.file.ReadAt(data, int64(handle.offset))
	if err != nil {
		it.err = err
		it.valid = false
		return false
	}
	
	it.current = NewBlockIterator(data)
	it.current.SeekToFirst()
	it.valid = it.current.Valid()
	return it.valid
}

// Seek 定位到第一个 >= key 的元素
func (it *SSTableIterator) Seek(key []byte) bool {
	if it.reader.indexBlock == nil {
		it.valid = false
		return false
	}
	
	// 在 Index Block 中找到目标 Data Block
	blockHandle, found := it.reader.findDataBlock(key)
	if !found {
		it.valid = false
		return false
	}
	
	// 读取 Data Block
	data := make([]byte, blockHandle.size)
	_, err := it.reader.file.ReadAt(data, int64(blockHandle.offset))
	if err != nil {
		it.err = err
		it.valid = false
		return false
	}
	
	it.current = NewBlockIterator(data)
	it.valid = it.current.Seek(key)
	return it.valid
}

// Key 获取当前 key
func (it *SSTableIterator) Key() []byte {
	if !it.valid || it.current == nil {
		return nil
	}
	return it.current.Key()
}

// Value 获取当前 value
func (it *SSTableIterator) Value() []byte {
	if !it.valid || it.current == nil {
		return nil
	}
	return it.current.Value()
}

// Valid 检查是否有效
func (it *SSTableIterator) Valid() bool {
	return it.valid && it.err == nil
}

// Next 移动到下一个元素
func (it *SSTableIterator) Next() bool {
	if !it.valid || it.current == nil {
		return false
	}
	
	it.current.Next()
	
	// 如果当前 Block 遍历完了，加载下一个 Block
	if !it.current.Valid() {
		// 需要在 Index Block 中移动到下一个 Block
		// 简化实现：直接设置为无效
		it.valid = false
		return false
	}
	
	return it.valid
}

// loadNextBlock 加载下一个 Data Block
func (it *SSTableIterator) loadNextBlock() {
	// TODO: 实现加载下一个 Block 的逻辑
	// 需要知道当前是哪个 Block，然后加载下一个
	it.valid = false
}

// Prev 移动到前一个元素（不支持）
func (it *SSTableIterator) Prev() bool {
	// SSTable 只支持顺序遍历，不支持反向
	return false
}

// Last 定位到最后一个元素（不支持）
func (it *SSTableIterator) Last() bool {
	// SSTable 只支持顺序遍历，不支持反向
	it.valid = false
	return false
}

// Error 获取错误
func (it *SSTableIterator) Error() error {
	return it.err
}

// Release 释放资源
func (it *SSTableIterator) Release() {
	if it.current != nil {
		it.current.Release()
	}
	it.current = nil
	it.valid = false
}
