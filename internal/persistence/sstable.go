package persistence

import (
	"encoding/binary"
	"os"
)

// SSTable 磁盘上的有序字符串表格
type SSTable struct {
	file     *os.File
	fileSize int64
	footer   *Footer
	options  *Options
}

// Footer SSTable 文件尾部信息
type Footer struct {
	metaIndexHandle BlockHandle // Meta Index Block 句柄
	indexHandle     BlockHandle // Index Block 句柄
	padding         [8]byte
	magicNumber     uint64 // 魔数，用于标识文件格式
}

// BlockHandle Block 句柄，指向文件中的某个 Block
type BlockHandle struct {
	offset uint64 // Block 在文件中的偏移
	size   uint64 // Block 大小
}

// Encode 编码 BlockHandle
func (bh *BlockHandle) Encode() []byte {
	buf := make([]byte, 20) // varint 最大 10 字节 * 2
	n := binary.PutUvarint(buf, bh.offset)
	m := binary.PutUvarint(buf[n:], bh.size)
	return buf[:n+m]
}

// Decode 解码 BlockHandle
func (bh *BlockHandle) Decode(data []byte) (int, error) {
	offset, n := binary.Uvarint(data)
	if n <= 0 {
		return 0, ErrInvalidFormat
	}
	bh.offset = offset
	
	size, m := binary.Uvarint(data[n:])
	if m <= 0 {
		return 0, ErrInvalidFormat
	}
	bh.size = size
	
	return n + m, nil
}

// SSTable 魔数 (LevelDB 兼容)
const TableMagicNumber = uint64(0xdb4775248b80fb57)

// Footer 大小固定为 48 字节
const FooterSize = 48

// NewSSTable 创建新的 SSTable
func NewSSTable(file *os.File, options *Options) (*SSTable, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	
	if options == nil {
		options = DefaultOptions()
	}
	
	return &SSTable{
		file:     file,
		fileSize: info.Size(),
		options:  options,
	}, nil
}

// OpenSSTable 打开已有的 SSTable 文件
func OpenSSTable(filename string, options *Options) (*SSTable, error) {
	file, err := os.OpenFile(filename, os.O_RDONLY, 0644)
	if err != nil {
		return nil, err
	}
	
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	
	if options == nil {
		options = DefaultOptions()
	}
	
	sst := &SSTable{
		file:     file,
		fileSize: info.Size(),
		options:  options,
	}
	
	// 读取 Footer
	if err := sst.readFooter(); err != nil {
		file.Close()
		return nil, err
	}
	
	return sst, nil
}

// readFooter 读取 Footer 信息
func (s *SSTable) readFooter() error {
	if s.fileSize < FooterSize {
		return ErrInvalidFormat
	}
	
	// 从文件末尾读取 Footer
	footerData := make([]byte, FooterSize)
	_, err := s.file.ReadAt(footerData, s.fileSize-FooterSize)
	if err != nil {
		return err
	}
	
	// 解析 Footer
	// [meta_index_handle (20 bytes)][index_handle (20 bytes)][padding (8 bytes)][magic_number (8 bytes)]
	
	// 读取 magic number
	magic := binary.LittleEndian.Uint64(footerData[40:])
	if magic != TableMagicNumber {
		return ErrInvalidFormat
	}
	
	footer := &Footer{}
	
	// 解码 meta index handle
	n, err := footer.metaIndexHandle.Decode(footerData[0:20])
	if err != nil {
		return err
	}
	
	// 解码 index handle
	_, err = footer.indexHandle.Decode(footerData[n : n+20])
	if err != nil {
		return err
	}
	
	s.footer = footer
	return nil
}

// WriteFooter 写入 Footer
func (s *SSTable) WriteFooter(metaHandle, indexHandle BlockHandle) error {
	footer := &Footer{
		metaIndexHandle: metaHandle,
		indexHandle:     indexHandle,
		magicNumber:     TableMagicNumber,
	}
	
	// 编码 Footer
	footerData := make([]byte, FooterSize)
	
	// 编码 meta index handle
	encoded := metaHandle.Encode()
	copy(footerData[0:], encoded)
	
	// 编码 index handle
	encoded = indexHandle.Encode()
	copy(footerData[20:], encoded)
	
	// 写入 magic number
	binary.LittleEndian.PutUint64(footerData[40:], TableMagicNumber)
	
	// 写入文件末尾
	_, err := s.file.WriteAt(footerData, s.fileSize)
	if err != nil {
		return err
	}
	
	s.footer = footer
	return nil
}

// File 获取底层文件
func (s *SSTable) File() *os.File {
	return s.file
}

// Size 返回文件大小
func (s *SSTable) Size() int64 {
	return s.fileSize
}

// Close 关闭 SSTable
func (s *SSTable) Close() error {
	if s.file != nil {
		return s.file.Close()
	}
	return nil
}

// 错误定义
var (
	ErrInvalidFormat = &os.PathError{Op: "read", Path: "sstable", Err: os.ErrInvalid}
)
