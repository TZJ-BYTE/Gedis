package datastruct

import (
	"encoding/gob"
	"bytes"
	"fmt"
	"sort"
	"time"
)

// 初始化时注册所有可能的类型
func init() {
	// 注册指针类型，确保 gob 解码时还原为指针
	gob.Register(&String{})
	gob.Register(&List{})
	gob.Register(&Hash{})
	gob.Register(&Set{})
	gob.Register(&ZSet{})
}

// DataValue 存储的数据值结构
type DataValue struct {
	Value      interface{} // 实际数据
	ExpireTime int64       // 过期时间戳，0 表示永不过期
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
	var buf bytes.Buffer

	// 写入前缀 0x01，防止数据以 0x00 开头被误判为删除标记
	buf.WriteByte(0x01)

	encoder := gob.NewEncoder(&buf)
	
	// 先写入 ExpireTime
	err := encoder.Encode(dv.ExpireTime)
	if err != nil {
		return nil, err
	}
	
	// 再写入 Value
	// 注意：必须传入接口的指针，以便 Gob 编码类型信息，
	// 这样解码时 Decode(&interface{}) 才能正确工作。
	err = encoder.Encode(&dv.Value)
	if err != nil {
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
	
	// 检查并去除前缀 0x01
	prefix, err := buf.ReadByte()
	if err == nil {
		if prefix != 0x01 {
			// 没有前缀，回退（尝试兼容旧数据）
			buf.UnreadByte()
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

// String 字符串类型
type String struct {
	Data string
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
