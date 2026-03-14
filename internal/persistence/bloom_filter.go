package persistence

import (
	"math"
)

// BloomFilter Bloom Filter 数据结构
type BloomFilter struct {
	bits       []byte    // 位数组
	numHashes  int       // 哈希函数数量
	size       uint64    // 位数组大小（位数）
	itemCount  uint64    // 已添加的元素数量
}

// NewBloomFilter 创建一个新的 Bloom Filter
func NewBloomFilter(expectedItems uint64, falsePositiveRate float64) *BloomFilter {
	// 计算最优的位数组大小和哈希函数数量
	// m = -((n * ln(p)) / (ln(2)^2))
	// k = (m/n) * ln(2)
	size := optimalNumBits(expectedItems, falsePositiveRate)
	numHashes := optimalNumHashes(size, expectedItems)

	return &BloomFilter{
		bits:      make([]byte, (size+7)/8), // 转换为字节数
		numHashes: numHashes,
		size:      size,
		itemCount: 0,
	}
}

// Add 添加一个元素到 Bloom Filter
func (bf *BloomFilter) Add(key []byte) {
	for i := 0; i < bf.numHashes; i++ {
		hash := hashKey(key, uint32(i))
		bitIndex := hash % uint64(bf.size)
		byteIndex := bitIndex / 8
		bitMask := byte(1 << (bitIndex % 8))
		bf.bits[byteIndex] |= bitMask
	}
	bf.itemCount++
}

// MayContain 检查元素是否可能存在（可能误判为存在，但不会漏判）
func (bf *BloomFilter) MayContain(key []byte) bool {
	for i := 0; i < bf.numHashes; i++ {
		hash := hashKey(key, uint32(i))
		bitIndex := hash % uint64(bf.size)
		byteIndex := bitIndex / 8
		bitMask := byte(1 << (bitIndex % 8))
		if (bf.bits[byteIndex] & bitMask) == 0 {
			return false
		}
	}
	return true
}

// Clear 清空 Bloom Filter
func (bf *BloomFilter) Clear() {
	for i := range bf.bits {
		bf.bits[i] = 0
	}
	bf.itemCount = 0
}

// ItemCount 获取已添加的元素数量
func (bf *BloomFilter) ItemCount() uint64 {
	return bf.itemCount
}

// Size 获取位数组大小（位数）
func (bf *BloomFilter) Size() uint64 {
	return bf.size
}

// NumHashes 获取哈希函数数量
func (bf *BloomFilter) NumHashes() int {
	return bf.numHashes
}

// FalsePositiveRate 估算当前的假阳性率
func (bf *BloomFilter) FalsePositiveRate() float64 {
	// p ≈ (1 - e^(-kn/m))^k
	if bf.itemCount == 0 {
		return 0.0
	}

	m := float64(bf.size)
	k := float64(bf.numHashes)

	// 计算位数组中 1 的比例
	ratio := float64(bf.countSetBits()) / m
	if ratio >= 1.0 {
		return 1.0
	}

	// p ≈ (ratio)^k
	return math.Pow(ratio, k)
}

// countSetBits 统计位数组中 1 的数量
func (bf *BloomFilter) countSetBits() int {
	count := 0
	for _, b := range bf.bits {
		// 使用 Brian Kernighan 算法统计 1 的个数
		for b != 0 {
			b &= b - 1
			count++
		}
	}
	return count
}

// optimalNumBits 计算最优的位数组大小
func optimalNumBits(n uint64, p float64) uint64 {
	if n == 0 || p <= 0 || p >= 1 {
		return 0
	}

	// m = -((n * ln(p)) / (ln(2)^2))
	ln2 := math.Log(2)
	ln2Squared := ln2 * ln2
	m := -float64(n) * math.Log(p) / ln2Squared

	return uint64(math.Ceil(m))
}

// optimalNumHashes 计算最优的哈希函数数量
func optimalNumHashes(m, n uint64) int {
	if m == 0 || n == 0 {
		return 1
	}

	// k = (m/n) * ln(2)
	ln2 := math.Log(2)
	k := float64(m) / float64(n) * ln2

	return int(math.Max(1, math.Min(float64(m), math.Round(k))))
}

// hashKey 生成第 seed 个哈希值
// 使用 FNV-1a 哈希算法的变体
func hashKey(key []byte, seed uint32) uint64 {
	// 使用两个不同的种子值来模拟多个哈希函数
	// h1 = FNV-1a with seed1
	// h2 = FNV-1a with seed2
	// h_i = h1 + i * h2

	const (
		fnvPrime  uint64 = 1099511628211
		fnvOffset uint64 = 14695981039346656037
	)

	h1 := fnvOffset
	h2 := fnvOffset ^ uint64(seed)

	for _, b := range key {
		h1 ^= uint64(b)
		h1 *= fnvPrime

		h2 ^= uint64(b)
		h2 *= fnvPrime
	}

	// 组合两个哈希值
	return h1 + uint64(seed)*h2
}

// Encode 编码 Bloom Filter 为字节切片
func (bf *BloomFilter) Encode() []byte {
	// 格式：[numHashes:4bytes][itemCount:8bytes][size:8bytes][bits:len]
	data := make([]byte, 4+8+8+len(bf.bits))

	// 写入 numHashes
	for i := 0; i < 4; i++ {
		data[i] = byte(bf.numHashes >> (uint(i) * 8))
	}

	// 写入 itemCount
	for i := 0; i < 8; i++ {
		data[4+i] = byte(bf.itemCount >> (uint(i) * 8))
	}

	// 写入 size
	for i := 0; i < 8; i++ {
		data[12+i] = byte(bf.size >> (uint(i) * 8))
	}

	// 复制 bits
	copy(data[20:], bf.bits)

	return data
}

// DecodeBloomFilter 从字节切片解码 Bloom Filter
func DecodeBloomFilter(data []byte) (*BloomFilter, error) {
	if len(data) < 20 {
		return nil, ErrInvalidFormat
	}

	// 读取 numHashes
	var numHashes uint32
	for i := 0; i < 4; i++ {
		numHashes |= uint32(data[i]) << (uint(i) * 8)
	}

	// 读取 itemCount
	var itemCount uint64
	for i := 0; i < 8; i++ {
		itemCount |= uint64(data[4+i]) << (uint(i) * 8)
	}

	// 读取 size
	var size uint64
	for i := 0; i < 8; i++ {
		size |= uint64(data[12+i]) << (uint(i) * 8)
	}

	// 读取 bits
	bits := make([]byte, len(data)-20)
	copy(bits, data[20:])

	return &BloomFilter{
		bits:      bits,
		numHashes: int(numHashes),
		size:      size,
		itemCount: itemCount,
	}, nil
}
