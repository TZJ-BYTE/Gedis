package persistence

// Iterator 迭代器接口
type Iterator interface {
	// Next 移动到下一个元素，返回是否成功
	Next() bool
	
	// Prev 移动到前一个元素，返回是否成功
	// 注意：需要底层数据结构支持反向遍历
	Prev() bool
	
	// Key 获取当前元素的 key
	// 如果迭代器无效，返回 nil
	Key() []byte
	
	// Value 获取当前元素的 value
	// 如果迭代器无效，返回 nil
	Value() []byte
	
	// Valid 检查迭代器是否有效
	// 当到达末尾或初始状态时返回 false
	Valid() bool
	
	// Error 获取错误信息
	// 如果迭代过程中发生错误，返回 error
	Error() error
	
	// Seek 定位到第一个 >= key 的元素
	// 返回是否找到
	Seek(key []byte) bool
	
	// First 定位到第一个元素
	// 返回是否成功
	First() bool
	
	// Last 定位到最后一个元素
	// 返回是否成功
	Last() bool
	
	// Release 释放迭代器资源
	Release()
}

// Ensure SkipListIterator implements Iterator
var _ Iterator = (*SkipListIterator)(nil)
