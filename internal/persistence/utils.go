package persistence

import (
	"encoding/binary"
	"hash/crc32"
	"os"
)

// ========== 字节序转换 ==========

// PutUint64 将 uint64 编码为字节切片（小端序）
func PutUint64(b []byte, v uint64) {
	binary.LittleEndian.PutUint64(b, v)
}

// GetUint64 从字节切片解码 uint64（小端序）
func GetUint64(b []byte) uint64 {
	return binary.LittleEndian.Uint64(b)
}

// PutUint32 将 uint32 编码为字节切片（小端序）
func PutUint32(b []byte, v uint32) {
	binary.LittleEndian.PutUint32(b, v)
}

// GetUint32 从字节切片解码 uint32（小端序）
func GetUint32(b []byte) uint32 {
	return binary.LittleEndian.Uint32(b)
}

// PutVarint 将 int 编码为变长字节
// 返回写入的字节数
func PutVarint(b []byte, v int) int {
	uv := uint64(v)
	i := 0
	for uv >= 0x80 {
		b[i] = byte(uv) | 0x80
		uv >>= 7
		i++
	}
	b[i] = byte(uv)
	return i + 1
}

// GetVarint 从字节切片解码变长整数
// 返回解码的值和读取的字节数
func GetVarint(b []byte) (int, int) {
	var result uint64
	var shift uint
	for i := 0; i < len(b); i++ {
		result |= uint64(b[i]&0x7F) << shift
		if b[i]&0x80 == 0 {
			return int(result), i + 1
		}
		shift += 7
	}
	return int(result), len(b)
}

// ========== CRC 校验 ==========

var crc32Table = crc32.MakeTable(crc32.Castagnoli)

// CRC32 计算数据的 CRC32 校验和
func CRC32(data []byte) uint32 {
	return crc32.Checksum(data, crc32Table)
}

// VerifyCRC32 验证 CRC32 校验和
func VerifyCRC32(data []byte, expected uint32) bool {
	return CRC32(data) == expected
}

// ========== 文件操作 ==========

// RenameFile 重命名文件（原子操作）
func RenameFile(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

// DeleteFile 删除文件
func DeleteFile(path string) error {
	return os.Remove(path)
}

// FileExists 检查文件是否存在
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// CreateDir 创建目录（如果不存在）
func CreateDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// GetFileSize 获取文件大小
func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// ========== Key 编码 ==========

// InternalKeySuffix 内部键后缀类型
type InternalKeySuffix byte

const (
	// TypeValue 普通值
	TypeValue InternalKeySuffix = 1
	// TypeDeletion 删除标记
	TypeDeletion InternalKeySuffix = 2
)

// EncodeInternalKey 编码内部键（包含序列号和类型）
func EncodeInternalKey(key string, seqNum uint64, suffix InternalKeySuffix) []byte {
	// 格式：[key_len:varint][key:key_len bytes][seq_num:8][suffix:1]
	keyBytes := []byte(key)
	buf := make([]byte, 0, len(keyBytes)+16)
	
	// 写入 key 长度
	lenBuf := make([]byte, 8)
	n := PutVarint(lenBuf, len(keyBytes))
	buf = append(buf, lenBuf[:n]...)
	
	// 写入 key
	buf = append(buf, keyBytes...)
	
	// 写入序列号
	seqBuf := make([]byte, 8)
	PutUint64(seqBuf, seqNum)
	buf = append(buf, seqBuf...)
	
	// 写入类型
	buf = append(buf, byte(suffix))
	
	return buf
}

// DecodeInternalKey 解码内部键
func DecodeInternalKey(data []byte) (key string, seqNum uint64, suffix InternalKeySuffix, consumed int) {
	if len(data) == 0 {
		return "", 0, 0, 0
	}
	
	// 读取 key 长度
	keyLen, n := GetVarint(data)
	consumed = n
	
	// 读取 key
	key = string(data[consumed : consumed+keyLen])
	consumed += keyLen
	
	// 读取序列号
	seqNum = GetUint64(data[consumed:])
	consumed += 8
	
	// 读取类型
	suffix = InternalKeySuffix(data[consumed])
	consumed++
	
	return
}

// CompareKeys 比较两个内部键
// 返回 -1: a < b, 0: a == b, 1: a > b
func CompareKeys(a, b []byte) int {
	keyA, _, _, _ := DecodeInternalKey(a)
	keyB, _, _, _ := DecodeInternalKey(b)
	
	if keyA < keyB {
		return -1
	} else if keyA > keyB {
		return 1
	}
	return 0
}
