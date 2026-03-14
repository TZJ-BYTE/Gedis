package persistence

import (
	"fmt"
	"testing"
)

func TestBloomFilter_Basic(t *testing.T) {
	bf := NewBloomFilter(1000, 0.001)

	// 添加一些数据
	keys := [][]byte{
		[]byte("key1"),
		[]byte("key2"),
		[]byte("key3"),
	}

	for _, key := range keys {
		bf.Add(key)
	}

	// 验证存在的 key
	for _, key := range keys {
		if !bf.MayContain(key) {
			t.Errorf("Expected to contain %s", string(key))
		}
	}

	// 验证不存在的 key（可能误判，但概率很低）
	notExistKeys := [][]byte{
		[]byte("key4"),
		[]byte("key5"),
		[]byte("nonexistent"),
	}

	falsePositives := 0
	for _, key := range notExistKeys {
		if bf.MayContain(key) {
			falsePositives++
		}
	}

	// 允许少量误判（但在这个小测试中应该没有）
	if falsePositives > 1 {
		t.Errorf("Too many false positives: %d", falsePositives)
	}
}

func TestBloomFilter_FalsePositiveRate(t *testing.T) {
	expectedItems := uint64(10000)
	falsePositiveRate := 0.001 // 0.1%

	bf := NewBloomFilter(expectedItems, falsePositiveRate)

	// 添加预期数量的元素
	for i := 0; i < int(expectedItems); i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		bf.Add(key)
	}

	// 测试大量不存在的 key，统计误判率
	testCount := 10000
	falsePositives := 0

	for i := int(expectedItems); i < int(expectedItems)+testCount; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		if bf.MayContain(key) {
			falsePositives++
		}
	}

	actualRate := float64(falsePositives) / float64(testCount)

	// 实际误判率应该接近或低于目标值（允许一定波动）
	t.Logf("Target FPR: %.4f, Actual FPR: %.4f (%d/%d)", 
		falsePositiveRate, actualRate, falsePositives, testCount)

	// 由于随机性，允许 2 倍的目标值作为上限
	if actualRate > falsePositiveRate*2 {
		t.Errorf("False positive rate too high: %.4f (target: %.4f)", 
			actualRate, falsePositiveRate)
	}
}

func TestBloomFilter_Clear(t *testing.T) {
	bf := NewBloomFilter(1000, 0.001)

	// 添加一些数据
	for i := 0; i < 100; i++ {
		bf.Add([]byte(fmt.Sprintf("key%d", i)))
	}

	// 验证都包含
	if !bf.MayContain([]byte("key50")) {
		t.Error("Expected to contain key50 before clear")
	}

	// 清空
	bf.Clear()

	// 验证都不包含（可能有误判，但概率极低）
	falsePositives := 0
	for i := 0; i < 100; i++ {
		if bf.MayContain([]byte(fmt.Sprintf("key%d", i))) {
			falsePositives++
		}
	}

	// 清空后不应该有任何元素
	if falsePositives > 5 {
		t.Errorf("Too many items after clear: %d", falsePositives)
	}
}

func TestBloomFilter_EncodeDecode(t *testing.T) {
	bf1 := NewBloomFilter(1000, 0.001)

	// 添加一些数据
	for i := 0; i < 100; i++ {
		bf1.Add([]byte(fmt.Sprintf("key%d", i)))
	}

	// 编码
	data := bf1.Encode()
	if len(data) == 0 {
		t.Fatal("Encode returned empty data")
	}

	// 解码
	bf2, err := DecodeBloomFilter(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// 验证解码后的 Bloom Filter 与原数据一致
	if bf1.Size() != bf2.Size() {
		t.Errorf("Size mismatch: %d vs %d", bf1.Size(), bf2.Size())
	}

	if bf1.NumHashes() != bf2.NumHashes() {
		t.Errorf("NumHashes mismatch: %d vs %d", bf1.NumHashes(), bf2.NumHashes())
	}

	if bf1.ItemCount() != bf2.ItemCount() {
		t.Errorf("ItemCount mismatch: %d vs %d", bf1.ItemCount(), bf2.ItemCount())
	}

	// 验证查询结果一致
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("key%d", i))
		c1 := bf1.MayContain(key)
		c2 := bf2.MayContain(key)
		if c1 != c2 {
			t.Errorf("Inconsistent result for key%d: %v vs %v", i, c1, c2)
		}
	}
}

func TestBloomFilter_OptimalParameters(t *testing.T) {
	tests := []struct {
		n     uint64
		p     float64
		wantM uint64
		wantK int
	}{
		{1000, 0.001, 14377, 10},
		{10000, 0.001, 143767, 10},
		{1000, 0.01, 9586, 7},
	}

	for _, tt := range tests {
		m := optimalNumBits(tt.n, tt.p)
		k := optimalNumHashes(m, tt.n)

		// 允许 10% 的误差
		tolerance := 0.1
		if float64(m) < float64(tt.wantM)*(1-tolerance) || 
		   float64(m) > float64(tt.wantM)*(1+tolerance) {
			t.Errorf("optimalNumBits(%d, %.3f) = %d, want ~%d", 
				tt.n, tt.p, m, tt.wantM)
		}

		if k != tt.wantK {
			t.Errorf("optimalNumHashes(%d, %d) = %d, want %d", 
				m, tt.n, k, tt.wantK)
		}
	}
}

func BenchmarkBloomFilter_Add(b *testing.B) {
	bf := NewBloomFilter(100000, 0.001)
	key := []byte("test_key_for_benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.Add(key)
	}
}

func BenchmarkBloomFilter_MayContain(b *testing.B) {
	bf := NewBloomFilter(100000, 0.001)
	key := []byte("test_key_for_benchmark")

	// 先添加
	bf.Add(key)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.MayContain(key)
	}
}
