package datastruct

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"math"
	"sort"
	"time"
)

// 初始化时注册所有可能的类型
func init() {
	// 注册指针类型，确保 gob 解码时还原为指针
	gob.Register(&String{})
	gob.Register(&BytesString{})
	gob.Register(&List{})
	gob.Register(&Hash{})
	gob.Register(&Set{})
	gob.Register(&ZSet{})
}

// DataValue 存储的数据值结构
type DataValue struct {
	Value          interface{} // 实际数据
	ExpireTime     int64       // 过期时间戳，0 表示永不过期
	LastAccessedAt int64       // 最后访问时间戳（毫秒），用于 LRU
}

// UpdateLastAccessed 更新最后访问时间
func (dv *DataValue) UpdateLastAccessed() {
	dv.LastAccessedAt = time.Now().UnixMilli()
}

// ApproximateSize 返回估算的内存大小（字节）
func (dv *DataValue) ApproximateSize() int64 {
	size := int64(24) // struct base overhead (approx)

	switch v := dv.Value.(type) {
	case *String:
		size += int64(len(v.Data))
	case *BytesString:
		size += int64(len(v.Data))
	case *List:
		for _, s := range v.Data {
			size += int64(len(s)) + 16 // string header overhead
		}
		size += int64(len(v.Data) * 8) // slice overhead
	case *Hash:
		for k, val := range v.Data {
			size += int64(len(k)) + int64(len(val)) + 32 // map entry overhead
		}
	case *Set:
		for k := range v.Data {
			size += int64(len(k)) + 24 // map entry overhead
		}
	case *ZSet:
		for k := range v.Data {
			size += int64(len(k)) + 48 // map entry + float64 overhead
		}
		size += int64(len(v.Scores) * 24) // slice overhead
	case string: // 兼容纯字符串 Value
		size += int64(len(v))
	}

	return size
}

// IsExpired 检查是否过期
func (dv *DataValue) IsExpired() bool {
	if dv.ExpireTime == 0 {
		return false
	}
	return time.Now().UnixMilli() > dv.ExpireTime
}

// Serialize 序列化 DataValue
func (dv *DataValue) Serialize() ([]byte, error) {
	if b, ok := dv.serializeV3(); ok {
		return b, nil
	}

	var buf bytes.Buffer
	buf.WriteByte(0x02)
	encoder := gob.NewEncoder(&buf)
	if err := encoder.Encode(dv.ExpireTime); err != nil {
		return nil, err
	}
	if err := encoder.Encode(dv.LastAccessedAt); err != nil {
		return nil, err
	}
	if err := encoder.Encode(&dv.Value); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DeserializeDataValue 反序列化 DataValue
func DeserializeDataValue(data []byte) (*DataValue, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty data")
	}

	buf := bytes.NewBuffer(data)

	prefix, err := buf.ReadByte()
	if err == nil {
		if prefix == 0x03 {
			return deserializeDataValueV3(buf)
		}
		if prefix != 0x01 && prefix != 0x02 {
			buf.UnreadByte()
			prefix = 0x01
		}
	}

	decoder := gob.NewDecoder(buf)

	// 使用对象池
	dv := NewDataValue()

	// 先读取 ExpireTime
	err = decoder.Decode(&dv.ExpireTime)
	if err != nil {
		FreeDataValue(dv) // 失败归还
		return nil, err
	}

	// 根据版本读取 LastAccessedAt
	if prefix == 0x02 {
		err = decoder.Decode(&dv.LastAccessedAt)
		if err != nil {
			FreeDataValue(dv)
			return nil, err
		}
	} else {
		// v1.0 格式，没有 LastAccessedAt
		dv.LastAccessedAt = time.Now().UnixMilli()
	}

	// 再读取 Value - 创建一个空接口来接收
	// Gob 会自动根据注册的类型信息解码为具体类型
	var value interface{}
	err = decoder.Decode(&value)
	if err != nil {
		FreeDataValue(dv) // 失败归还
		return nil, fmt.Errorf("failed to decode value: %w", err)
	}

	dv.Value = value

	return dv, nil
}

func (dv *DataValue) serializeV3() ([]byte, bool) {
	out := make([]byte, 0, 128)
	out = append(out, 0x03)
	var tmp [8]byte
	binary.LittleEndian.PutUint64(tmp[:], uint64(dv.ExpireTime))
	out = append(out, tmp[:]...)
	binary.LittleEndian.PutUint64(tmp[:], uint64(dv.LastAccessedAt))
	out = append(out, tmp[:]...)

	switch v := dv.Value.(type) {
	case *String:
		out = append(out, 1)
		out = appendU32(out, uint32(len(v.Data)))
		out = append(out, v.Data...)
		return out, true
	case *BytesString:
		out = append(out, 2)
		out = appendU32(out, uint32(len(v.Data)))
		out = append(out, v.Data...)
		return out, true
	case *List:
		out = append(out, 3)
		out = appendU32(out, uint32(len(v.Data)))
		for i := range v.Data {
			s := v.Data[i]
			out = appendU32(out, uint32(len(s)))
			out = append(out, s...)
		}
		return out, true
	case *Hash:
		out = append(out, 4)
		if v.Data == nil {
			out = appendU32(out, 0)
			return out, true
		}
		keys := make([]string, 0, len(v.Data))
		for k := range v.Data {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out = appendU32(out, uint32(len(keys)))
		for _, k := range keys {
			val := v.Data[k]
			out = appendU32(out, uint32(len(k)))
			out = append(out, k...)
			out = appendU32(out, uint32(len(val)))
			out = append(out, val...)
		}
		return out, true
	case *Set:
		out = append(out, 5)
		if v.Data == nil {
			out = appendU32(out, 0)
			return out, true
		}
		members := make([]string, 0, len(v.Data))
		for m := range v.Data {
			members = append(members, m)
		}
		sort.Strings(members)
		out = appendU32(out, uint32(len(members)))
		for _, m := range members {
			out = appendU32(out, uint32(len(m)))
			out = append(out, m...)
		}
		return out, true
	case *ZSet:
		out = append(out, 6)
		if v.Data == nil {
			out = appendU32(out, 0)
			return out, true
		}
		members := make([]string, 0, len(v.Data))
		for m := range v.Data {
			members = append(members, m)
		}
		sort.Strings(members)
		out = appendU32(out, uint32(len(members)))
		for _, m := range members {
			score := v.Data[m]
			out = appendU32(out, uint32(len(m)))
			out = append(out, m...)
			var fb [8]byte
			binary.LittleEndian.PutUint64(fb[:], math.Float64bits(score))
			out = append(out, fb[:]...)
		}
		return out, true
	default:
		return nil, false
	}
}

func appendU32(dst []byte, v uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return append(dst, b[:]...)
}

func deserializeDataValueV3(buf *bytes.Buffer) (*DataValue, error) {
	data := buf.Bytes()
	pos := 0
	read := func(n int) ([]byte, bool) {
		if pos+n > len(data) {
			return nil, false
		}
		b := data[pos : pos+n]
		pos += n
		return b, true
	}

	b, ok := read(8)
	if !ok {
		return nil, fmt.Errorf("corrupt data")
	}
	expire := int64(binary.LittleEndian.Uint64(b))
	b, ok = read(8)
	if !ok {
		return nil, fmt.Errorf("corrupt data")
	}
	last := int64(binary.LittleEndian.Uint64(b))
	tb, ok := read(1)
	if !ok {
		return nil, fmt.Errorf("corrupt data")
	}
	typ := tb[0]

	dv := NewDataValue()
	dv.ExpireTime = expire
	dv.LastAccessedAt = last

	switch typ {
	case 1:
		lb, ok := read(4)
		if !ok {
			FreeDataValue(dv)
			return nil, fmt.Errorf("corrupt data")
		}
		n := int(binary.LittleEndian.Uint32(lb))
		sb, ok := read(n)
		if !ok {
			FreeDataValue(dv)
			return nil, fmt.Errorf("corrupt data")
		}
		dv.Value = &String{Data: string(sb)}
	case 2:
		lb, ok := read(4)
		if !ok {
			FreeDataValue(dv)
			return nil, fmt.Errorf("corrupt data")
		}
		n := int(binary.LittleEndian.Uint32(lb))
		vb, ok := read(n)
		if !ok {
			FreeDataValue(dv)
			return nil, fmt.Errorf("corrupt data")
		}
		out := make([]byte, n)
		copy(out, vb)
		dv.Value = &BytesString{Data: out}
	case 3:
		lb, ok := read(4)
		if !ok {
			FreeDataValue(dv)
			return nil, fmt.Errorf("corrupt data")
		}
		cnt := int(binary.LittleEndian.Uint32(lb))
		arr := make([]string, 0, cnt)
		for i := 0; i < cnt; i++ {
			lb, ok := read(4)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			n := int(binary.LittleEndian.Uint32(lb))
			sb, ok := read(n)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			arr = append(arr, string(sb))
		}
		dv.Value = &List{Data: arr}
	case 4:
		lb, ok := read(4)
		if !ok {
			FreeDataValue(dv)
			return nil, fmt.Errorf("corrupt data")
		}
		cnt := int(binary.LittleEndian.Uint32(lb))
		m := make(map[string]string, cnt)
		for i := 0; i < cnt; i++ {
			kl, ok := read(4)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			kn := int(binary.LittleEndian.Uint32(kl))
			kb, ok := read(kn)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			vl, ok := read(4)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			vn := int(binary.LittleEndian.Uint32(vl))
			vb, ok := read(vn)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			m[string(kb)] = string(vb)
		}
		dv.Value = &Hash{Data: m}
	case 5:
		lb, ok := read(4)
		if !ok {
			FreeDataValue(dv)
			return nil, fmt.Errorf("corrupt data")
		}
		cnt := int(binary.LittleEndian.Uint32(lb))
		m := make(map[string]struct{}, cnt)
		for i := 0; i < cnt; i++ {
			kl, ok := read(4)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			kn := int(binary.LittleEndian.Uint32(kl))
			kb, ok := read(kn)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			m[string(kb)] = struct{}{}
		}
		dv.Value = &Set{Data: m}
	case 6:
		lb, ok := read(4)
		if !ok {
			FreeDataValue(dv)
			return nil, fmt.Errorf("corrupt data")
		}
		cnt := int(binary.LittleEndian.Uint32(lb))
		m := make(map[string]float64, cnt)
		scores := make([]ZSetMember, 0, cnt)
		for i := 0; i < cnt; i++ {
			kl, ok := read(4)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			kn := int(binary.LittleEndian.Uint32(kl))
			kb, ok := read(kn)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			sb, ok := read(8)
			if !ok {
				FreeDataValue(dv)
				return nil, fmt.Errorf("corrupt data")
			}
			score := math.Float64frombits(binary.LittleEndian.Uint64(sb))
			member := string(kb)
			m[member] = score
			scores = append(scores, ZSetMember{Member: member, Score: score})
		}
		dv.Value = &ZSet{Data: m, Scores: scores}
	default:
		FreeDataValue(dv)
		return nil, fmt.Errorf("unknown value type")
	}

	buf.Next(pos)
	return dv, nil
}

// String 字符串类型
type String struct {
	Data string
}

type BytesString struct {
	Data []byte
}

// List 列表类型
type List struct {
	Data []string
}

// PushLeft 左侧插入
func (l *List) PushLeft(value string) {
	l.Data = append([]string{value}, l.Data...)
}

// PushRight 右侧插入
func (l *List) PushRight(value string) {
	l.Data = append(l.Data, value)
}

// PopLeft 左侧弹出
func (l *List) PopLeft() (string, bool) {
	if len(l.Data) == 0 {
		return "", false
	}
	value := l.Data[0]
	l.Data = l.Data[1:]
	return value, true
}

// PopRight 右侧弹出
func (l *List) PopRight() (string, bool) {
	if len(l.Data) == 0 {
		return "", false
	}
	value := l.Data[len(l.Data)-1]
	l.Data = l.Data[:len(l.Data)-1]
	return value, true
}

// Range 获取指定范围的元素
func (l *List) Range(start, stop int) []string {
	length := len(l.Data)
	if length == 0 {
		return []string{}
	}

	// 处理负数索引
	if start < 0 {
		start = length + start
	}
	if stop < 0 {
		stop = length + stop
	}

	// 边界检查
	if start < 0 {
		start = 0
	}
	if stop >= length {
		stop = length - 1
	}

	if start > stop {
		return []string{}
	}

	return l.Data[start : stop+1]
}

// Hash 哈希类型
type Hash struct {
	Data map[string]string
}

// Set 设置字段值
func (h *Hash) Set(field, value string) {
	if h.Data == nil {
		h.Data = make(map[string]string)
	}
	h.Data[field] = value
}

// Get 获取字段值
func (h *Hash) Get(field string) (string, bool) {
	if h.Data == nil {
		return "", false
	}
	value, exists := h.Data[field]
	return value, exists
}

// Delete 删除字段
func (h *Hash) Delete(field string) bool {
	if h.Data == nil {
		return false
	}
	_, exists := h.Data[field]
	if exists {
		delete(h.Data, field)
	}
	return exists
}

// Size 返回哈希大小
func (h *Hash) Size() int {
	if h.Data == nil {
		return 0
	}
	return len(h.Data)
}

// Set 集合类型
type Set struct {
	Data map[string]struct{}
}

// Add 添加元素
func (s *Set) Add(member string) bool {
	if s.Data == nil {
		s.Data = make(map[string]struct{})
	}
	if _, exists := s.Data[member]; exists {
		return false
	}
	s.Data[member] = struct{}{}
	return true
}

// Remove 移除元素
func (s *Set) Remove(member string) bool {
	if s.Data == nil {
		return false
	}
	if _, exists := s.Data[member]; !exists {
		return false
	}
	delete(s.Data, member)
	return true
}

// Contains 检查元素是否存在
func (s *Set) Contains(member string) bool {
	if s.Data == nil {
		return false
	}
	_, exists := s.Data[member]
	return exists
}

// Members 返回所有成员
func (s *Set) Members() []string {
	if s.Data == nil {
		return []string{}
	}
	members := make([]string, 0, len(s.Data))
	for member := range s.Data {
		members = append(members, member)
	}
	return members
}

// ZSetMember 有序集合成员
type ZSetMember struct {
	Member string
	Score  float64
}

// ZSet 有序集合类型
type ZSet struct {
	Data   map[string]float64 // member -> score
	Scores []ZSetMember       // 按分数排序的列表
}

// Add 添加元素
func (zs *ZSet) Add(member string, score float64) bool {
	if zs.Data == nil {
		zs.Data = make(map[string]float64)
	}

	_, exists := zs.Data[member]
	zs.Data[member] = score

	// 更新排序列表
	zs.updateScores()
	return !exists
}

// Remove 移除元素
func (zs *ZSet) Remove(member string) bool {
	if zs.Data == nil {
		return false
	}
	if _, exists := zs.Data[member]; !exists {
		return false
	}
	delete(zs.Data, member)
	zs.updateScores()
	return true
}

// Score 获取元素的分数
func (zs *ZSet) Score(member string) (float64, bool) {
	if zs.Data == nil {
		return 0, false
	}
	score, exists := zs.Data[member]
	return score, exists
}

// updateScores 更新排序列表
func (zs *ZSet) updateScores() {
	zs.Scores = make([]ZSetMember, 0, len(zs.Data))
	for member, score := range zs.Data {
		zs.Scores = append(zs.Scores, ZSetMember{
			Member: member,
			Score:  score,
		})
	}

	// 按分数排序
	sort.Slice(zs.Scores, func(i, j int) bool {
		return zs.Scores[i].Score < zs.Scores[j].Score
	})
}

// RangeByScore 根据分数范围获取成员
func (zs *ZSet) RangeByScore(min, max float64) []ZSetMember {
	result := make([]ZSetMember, 0)
	for _, item := range zs.Scores {
		if item.Score >= min && item.Score <= max {
			result = append(result, item)
		}
	}
	return result
}
