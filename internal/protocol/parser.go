package protocol

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// RESP 协议类型
const (
	SimpleString = '+' // 简单字符串
	Error        = '-' // 错误
	Integer      = ':' // 整数
	BulkString   = '$' // 批量字符串
	Array        = '*' // 数组
)

// Request Redis 请求
type Request struct {
	Cmd  string
	Args []string
}

// Response Redis 响应
type Response struct {
	Type  byte
	Value interface{}
	Error error
}

// Parser RESP 协议解析器
type Parser struct {
	reader *bufio.Reader
}

// NewParser 创建解析器
func NewParser(conn io.Reader) *Parser {
	return &Parser{
		reader: bufio.NewReader(conn),
	}
}

// ParseRequest 解析请求
func (p *Parser) ParseRequest() (*Request, error) {
	line, err := p.readLine()
	if err != nil {
		return nil, err
	}
	
	if len(line) == 0 {
		return nil, fmt.Errorf("empty request")
	}
	
	// 检查是否是数组类型
	if line[0] != Array {
		return nil, fmt.Errorf("invalid protocol: expected array")
	}
	
	// 解析数组长度
	count, err := strconv.Atoi(string(line[1:]))
	if err != nil {
		return nil, fmt.Errorf("invalid array length")
	}
	
	if count == 0 {
		return nil, fmt.Errorf("empty array")
	}
	
	// 第一个元素是命令
	cmd, err := p.readBulkString()
	if err != nil {
		return nil, err
	}
	
	// 解析参数
	args := make([]string, 0, count-1)
	for i := 1; i < count; i++ {
		arg, err := p.readBulkString()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	
	return &Request{
		Cmd:  strings.ToUpper(cmd),
		Args: args,
	}, nil
}

// readLine 读取一行
func (p *Parser) readLine() ([]byte, error) {
	line, err := p.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	
	// 移除 \r\n
	if len(line) >= 2 && line[len(line)-2] == '\r' {
		line = line[:len(line)-2]
	} else if len(line) >= 1 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	
	return line, nil
}

// readBulkString 读取批量字符串
func (p *Parser) readBulkString() (string, error) {
	line, err := p.readLine()
	if err != nil {
		return "", err
	}
	
	if len(line) == 0 || line[0] != BulkString {
		return "", fmt.Errorf("expected bulk string")
	}
	
	// 解析长度
	length, err := strconv.Atoi(string(line[1:]))
	if err != nil {
		return "", fmt.Errorf("invalid bulk string length")
	}
	
	// -1 表示空值
	if length == -1 {
		return "", nil
	}
	
	// 读取数据
	buf := make([]byte, length+2) // +2 for \r\n
	_, err = io.ReadFull(p.reader, buf)
	if err != nil {
		return "", err
	}
	
	// 移除 \r\n
	if length > 0 {
		buf = buf[:length]
	}
	
	return string(buf), nil
}

// ParseOneRequest 从字节切片中解析一个请求
// 返回：request, consumed bytes, error
// 如果数据不足以构成一个完整请求，返回 nil, 0, nil
func ParseOneRequest(data []byte) (*Request, int, error) {
	if len(data) == 0 {
		return nil, 0, nil
	}

	// 1. 检查类型
	if data[0] != Array {
		// 这里可能是内联命令（inline command），暂时只支持 RESP 数组
		return nil, 0, fmt.Errorf("invalid protocol: expected array")
	}

	// 2. 找到第一行结束符
	idx := bytes.IndexByte(data, '\n')
	if idx == -1 {
		return nil, 0, nil // 数据不足
	}

	// 解析数组长度
	line := data[1:idx]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	count, err := strconv.Atoi(string(line))
	if err != nil {
		return nil, 0, fmt.Errorf("invalid array length")
	}

	if count == 0 {
		return nil, 0, fmt.Errorf("empty array")
	}

	// 3. 循环解析每个参数
	offset := idx + 1
	args := make([]string, 0, count)

	for i := 0; i < count; i++ {
		// 检查是否有足够的字节读取类型
		if offset >= len(data) {
			return nil, 0, nil
		}

		if data[offset] != BulkString {
			return nil, 0, fmt.Errorf("expected bulk string")
		}

		// 找到下一行（长度）
		nextIdx := bytes.IndexByte(data[offset:], '\n')
		if nextIdx == -1 {
			return nil, 0, nil
		}
		nextIdx += offset // 转换为绝对索引

		line := data[offset+1 : nextIdx]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		length, err := strconv.Atoi(string(line))
		if err != nil {
			return nil, 0, fmt.Errorf("invalid bulk string length")
		}

		offset = nextIdx + 1

		if length == -1 {
			args = append(args, "")
			continue
		}

		// 检查数据是否足够
		// 需要 length + 2 (for \r\n)
		if offset+length+2 > len(data) {
			return nil, 0, nil
		}

		arg := string(data[offset : offset+length])
		args = append(args, arg)

		offset += length + 2
	}

	if len(args) == 0 {
		return nil, 0, fmt.Errorf("empty request")
	}

	req := &Request{
		Cmd:  strings.ToUpper(args[0]),
		Args: args[1:],
	}

	return req, offset, nil
}

// EncodeResponse 编码响应
func EncodeResponse(resp *Response) []byte {
	var buf bytes.Buffer
	
	switch resp.Type {
	case SimpleString:
		buf.WriteByte(SimpleString)
		buf.WriteString(resp.Value.(string))
		buf.WriteString("\r\n")
		
	case Error:
		buf.WriteByte(Error)
		if resp.Error != nil {
			buf.WriteString(resp.Error.Error())
		} else {
			buf.WriteString(fmt.Sprintf("%v", resp.Value))
		}
		buf.WriteString("\r\n")
		
	case Integer:
		buf.WriteByte(Integer)
		buf.WriteString(fmt.Sprintf("%d", resp.Value))
		buf.WriteString("\r\n")
		
	case BulkString:
		buf.WriteByte(BulkString)
		str := resp.Value.(string)
		buf.WriteString(strconv.Itoa(len(str)))
		buf.WriteString("\r\n")
		buf.WriteString(str)
		buf.WriteString("\r\n")
		
	case Array:
		buf.WriteByte(Array)
		arr := resp.Value.([]interface{})
		buf.WriteString(strconv.Itoa(len(arr)))
		buf.WriteString("\r\n")
		for _, item := range arr {
			itemResp := &Response{
				Type:  BulkString,
				Value: item,
			}
			buf.Write(EncodeResponse(itemResp))
		}
	}
	
	return buf.Bytes()
}

// MakeSimpleString 创建简单字符串响应
func MakeSimpleString(value string) *Response {
	return &Response{
		Type:  SimpleString,
		Value: value,
	}
}

// MakeError 创建错误响应
func MakeError(err error) *Response {
	return &Response{
		Type:  Error,
		Error: err,
	}
}

// MakeInteger 创建整数响应
func MakeInteger(value int64) *Response {
	return &Response{
		Type:  Integer,
		Value: value,
	}
}

// MakeBulkString 创建批量字符串响应
func MakeBulkString(value string) *Response {
	return &Response{
		Type:  BulkString,
		Value: value,
	}
}

// MakeArray 创建数组响应
func MakeArray(items []string) *Response {
	result := make([]interface{}, len(items))
	for i, item := range items {
		result[i] = item
	}
	return &Response{
		Type:  Array,
		Value: result,
	}
}

// MakeNull 创建空响应
func MakeNull() *Response {
	return &Response{
		Type:  BulkString,
		Value: "",
	}
}
