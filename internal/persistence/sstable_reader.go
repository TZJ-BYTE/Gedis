package persistence

import (
	"bytes"
	"encoding/binary"
	"os"
	"sync"
	"sync/atomic"
)

// SSTableReader SSTable 读取器
type SSTableReader struct {
	file           *os.File // 文件句柄
	filename       string   // 文件名
	fileNum        uint64
	options        *Options          // 配置选项
	footer         *Footer           // Footer 信息
	indexBlock     *Block            // Index Block（常驻内存）
	dataCache      map[uint64]*Block // Data Block 缓存（简化版）
	bloomFilter    *BloomFilter      // Bloom Filter（可选）
	blockCache     *BlockCache       // Block Cache（LRU）
	bloomChecks    atomic.Uint64
	bloomNegatives atomic.Uint64
	refCount       atomic.Int64
	closed         atomic.Bool
	closeOnce      sync.Once
	closeMu        sync.Mutex
	closeErr       error
}

// OpenSSTableForRead 打开 SSTable 用于读取
func OpenSSTableForRead(filename string, options *Options) (*SSTableReader, error) {
	return OpenSSTableForReadWithCache(0, filename, options, nil)
}

func OpenSSTableForReadWithCache(fileNum uint64, filename string, options *Options, sharedCache *BlockCache) (*SSTableReader, error) {
	if options == nil {
		options = DefaultOptions()
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var cache *BlockCache
	if options.UseCache {
		if sharedCache != nil {
			cache = sharedCache
		} else {
			cache = NewBlockCache(int64(options.CacheSize))
		}
	}

	reader := &SSTableReader{
		file:       file,
		filename:   filename,
		fileNum:    fileNum,
		options:    options,
		dataCache:  make(map[uint64]*Block),
		blockCache: cache,
	}
	reader.refCount.Store(1)

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
		blockCache: nil,
	}
	r.refCount.Store(1)

	return r, nil
}

func (r *SSTableReader) retain() bool {
	for {
		if r.closed.Load() {
			return false
		}
		cur := r.refCount.Load()
		if cur == 0 {
			return false
		}
		if r.refCount.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

func (r *SSTableReader) release() {
	for {
		cur := r.refCount.Load()
		if cur == 0 {
			return
		}
		if r.refCount.CompareAndSwap(cur, cur-1) {
			if cur-1 == 0 {
				r.closeOnce.Do(func() {
					var err error
					if r.file != nil {
						err = r.file.Close()
						r.file = nil
					}
					r.closed.Store(true)
					r.closeMu.Lock()
					r.closeErr = err
					r.closeMu.Unlock()
				})
			}
			return
		}
	}
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
	offset, n = binary.Uvarint(footerData[n : n+10])
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
	if !r.retain() {
		return nil, false
	}
	defer r.release()
	if r.file == nil {
		return nil, false
	}
	// 1. 先检查 Bloom Filter（如果有）
	if r.bloomFilter != nil {
		r.bloomChecks.Add(1)
		if !r.bloomFilter.MayContain(key) {
			r.bloomNegatives.Add(1)
			return nil, false
		}
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

func (r *SSTableReader) BloomStats() (checks uint64, negatives uint64) {
	return r.bloomChecks.Load(), r.bloomNegatives.Load()
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
	retained := r.retain()
	return &SSTableIterator{
		reader:   r,
		valid:    false,
		retained: retained,
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
	r.release()
	r.closeMu.Lock()
	defer r.closeMu.Unlock()
	return r.closeErr
}

// getBlockFromCache 从缓存中获取 Block
func (r *SSTableReader) getBlockFromCache(offset uint64) (*Block, bool) {
	// 先尝试 LRU Cache
	if r.blockCache != nil {
		if block, ok := r.blockCache.Get(r.blockCacheKey(offset)); ok {
			return block, true
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
		r.blockCache.Put(r.blockCacheKey(offset), block, size)
	}

	// 同时保留旧的 dataCache（向后兼容）
	if len(r.dataCache) < 100 { // 限制缓存数量
		r.dataCache[offset] = block
	}
}

func (r *SSTableReader) blockCacheKey(offset uint64) uint64 {
	if r.fileNum == 0 {
		return offset
	}
	return (r.fileNum << 32) ^ offset
}

// SSTableIterator SSTable 迭代器
type SSTableIterator struct {
	reader    *SSTableReader
	indexIter *BlockIterator
	current   *BlockIterator
	valid     bool
	err       error
	retained  bool
}

// SeekToFirst 定位到第一个元素
func (it *SSTableIterator) SeekToFirst() {
	it.First()
}

// First 定位到第一个元素
func (it *SSTableIterator) First() bool {
	if it.reader == nil || !it.retained {
		it.valid = false
		it.err = ErrInvalidFormat
		return false
	}
	if it.reader.indexBlock == nil {
		it.valid = false
		return false
	}

	// 初始化 Index Iterator
	it.indexIter = NewBlockIterator(it.reader.indexBlock.Data())
	it.indexIter.SeekToFirst()

	if !it.indexIter.Valid() {
		it.valid = false
		return false
	}

	// 加载第一个 Block
	return it.loadBlockFromIndexAndAdvance()
}

// Seek 定位到第一个 >= key 的元素
func (it *SSTableIterator) Seek(key []byte) bool {
	if it.reader == nil || !it.retained {
		it.valid = false
		it.err = ErrInvalidFormat
		return false
	}
	if it.reader.indexBlock == nil {
		it.valid = false
		return false
	}

	it.indexIter = NewBlockIterator(it.reader.indexBlock.Data())

	var targetHandle BlockHandle
	found := false

	// 在 Index Block 中查找目标 Data Block
	// 我们需要找到最后一个 key <= targetKey 的 block (或者说找到第一个 key > targetKey 的 block 的前一个)
	// 但由于 Index 中存储的是 Block 的 firstKey，如果 searchKey < firstKey，则该 block 不包含 searchKey
	// 因此我们找的是最后一个 firstKey <= searchKey 的 block

	for it.indexIter.SeekToFirst(); it.indexIter.Valid(); it.indexIter.Next() {
		indexKey := it.indexIter.Key()

		// 如果 index key > key，说明前一个 block 是目标
		// 此时 indexIter 指向的是 *下一个* block，正好符合我们在 Next() 中的预期
		if bytes.Compare(indexKey, key) > 0 {
			break
		}

		// 解码 BlockHandle
		handleData := it.indexIter.Value()
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

	if !found {
		it.valid = false
		return false
	}

	// 加载 Data Block
	if err := it.loadBlock(targetHandle); err != nil {
		it.err = err
		it.valid = false
		return false
	}

	// 在 Data Block 中 Seek
	it.valid = it.current.Seek(key)

	// 如果在当前 Block 中没找到 (到达末尾)，但 Block Iterator 是有效的
	// 这可能意味着 key 大于该 Block 的所有 key
	// 在这种情况下，我们应该尝试加载下一个 Block
	if !it.valid && it.current.Error() == nil {
		// 尝试加载下一个 Block
		if it.loadNextBlock() {
			// 在下一个 Block 中 SeekToFirst (因为 key 肯定小于下一个 Block 的所有 key，如果它存在的话)
			// 等等，如果 key > block1.last, 且 key < block2.first
			// 那么 key 不存在。
			// 但是 Seek 的语义是 >= key。
			// 所以如果是这种情况，我们应该返回 block2.first。
			// loadNextBlock 会加载 block2 并 SeekToFirst。
			it.valid = it.current.Valid()
		}
	}

	return it.valid
}

// loadBlockFromIndexAndAdvance 从当前 indexIter 加载 Block，并将 indexIter 前进一步
func (it *SSTableIterator) loadBlockFromIndexAndAdvance() bool {
	if !it.indexIter.Valid() {
		return false
	}

	handleData := it.indexIter.Value()
	var handle BlockHandle
	_, err := handle.Decode(handleData)
	if err != nil {
		it.err = err
		it.valid = false
		return false
	}

	// 移动到下一个 index
	it.indexIter.Next()

	if err := it.loadBlock(handle); err != nil {
		it.err = err
		it.valid = false
		return false
	}

	it.current.SeekToFirst()
	it.valid = it.current.Valid()
	return it.valid
}

// loadBlock 加载指定 handle 的 Block
func (it *SSTableIterator) loadBlock(handle BlockHandle) error {
	// 使用 Reader 的缓存机制获取 Block
	block, cached := it.reader.getBlockFromCache(handle.offset)
	if !cached {
		data := make([]byte, handle.size)
		_, err := it.reader.file.ReadAt(data, int64(handle.offset))
		if err != nil {
			return err
		}
		block = NewBlock(data)
		it.reader.putBlockToCache(handle.offset, block, len(data))
	}

	it.current = NewBlockIterator(block.Data())
	return nil
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
		return it.loadNextBlock()
	}

	return it.valid
}

// loadNextBlock 加载下一个 Data Block
func (it *SSTableIterator) loadNextBlock() bool {
	// 检查是否有下一个 Block
	if it.indexIter == nil || !it.indexIter.Valid() {
		it.valid = false
		return false
	}

	return it.loadBlockFromIndexAndAdvance()
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
	if it.retained && it.reader != nil {
		it.reader.release()
	}
	it.retained = false
}
