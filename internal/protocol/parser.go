package protocol

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"unsafe"
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
	Cmd    string
	Args   [][]byte
	cmdBuf []byte
}

// Response Redis 响应
type Response struct {
	Type   byte
	Value  interface{}
	Error  error
	pooled bool
}

// Parser RESP 协议解析器
type Parser struct {
	reader *bufio.Reader
}

var requestPool = sync.Pool{
	New: func() interface{} {
		return &Request{Args: make([][]byte, 0, 8), cmdBuf: make([]byte, 0, 16)}
	},
}

var responsePool = sync.Pool{
	New: func() interface{} {
		return &Response{}
	},
}

var parseMaxBulkLen atomic.Int64
var parseMaxArrayLen atomic.Int64

func SetParseLimits(maxBulkLen, maxArrayLen int) {
	if maxBulkLen < 0 {
		maxBulkLen = 0
	}
	if maxArrayLen < 0 {
		maxArrayLen = 0
	}
	parseMaxBulkLen.Store(int64(maxBulkLen))
	parseMaxArrayLen.Store(int64(maxArrayLen))
}

func getParseMaxBulkLen() int {
	return int(parseMaxBulkLen.Load())
}

func getParseMaxArrayLen() int {
	return int(parseMaxArrayLen.Load())
}

func acquireResponse() *Response {
	r := responsePool.Get().(*Response)
	r.Type = 0
	r.Value = nil
	r.Error = nil
	r.pooled = true
	return r
}

func ReleaseResponse(resp *Response) {
	if resp == nil || !resp.pooled {
		return
	}
	resp.Type = 0
	resp.Value = nil
	resp.Error = nil
	resp.pooled = false
	responsePool.Put(resp)
}

func acquireRequest() *Request {
	req := requestPool.Get().(*Request)
	req.Cmd = ""
	req.Args = req.Args[:0]
	req.cmdBuf = req.cmdBuf[:0]
	return req
}

func ReleaseRequest(req *Request) {
	if req == nil {
		return
	}
	req.Cmd = ""
	for i := range req.cmdBuf {
		req.cmdBuf[i] = 0
	}
	req.cmdBuf = req.cmdBuf[:0]
	for i := range req.Args {
		req.Args[i] = nil
	}
	req.Args = req.Args[:0]
	requestPool.Put(req)
}

// NewParser 创建解析器
func NewParser(conn io.Reader) *Parser {
	return &Parser{
		reader: bufio.NewReader(conn),
	}
}

func parseIntBytes(b []byte) (int, error) {
	if len(b) == 0 {
		return 0, fmt.Errorf("invalid int")
	}
	sign := 1
	i := 0
	if b[0] == '-' {
		sign = -1
		i = 1
		if i >= len(b) {
			return 0, fmt.Errorf("invalid int")
		}
	}
	n := 0
	for ; i < len(b); i++ {
		c := b[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid int")
		}
		n = n*10 + int(c-'0')
	}
	return sign * n, nil
}

func setCmdUpperFromBytes(req *Request, src []byte) {
	n := len(src)
	if cap(req.cmdBuf) < n {
		req.cmdBuf = make([]byte, n)
	} else {
		req.cmdBuf = req.cmdBuf[:n]
	}
	for i := 0; i < n; i++ {
		c := src[i]
		if c >= 'a' && c <= 'z' {
			c = c - ('a' - 'A')
		}
		req.cmdBuf[i] = c
	}
	req.Cmd = unsafe.String(unsafe.SliceData(req.cmdBuf), n)
}

func setCmdUpperFromString(req *Request, src string) {
	n := len(src)
	if cap(req.cmdBuf) < n {
		req.cmdBuf = make([]byte, n)
	} else {
		req.cmdBuf = req.cmdBuf[:n]
	}
	for i := 0; i < n; i++ {
		c := src[i]
		if c >= 'a' && c <= 'z' {
			c = c - ('a' - 'A')
		}
		req.cmdBuf[i] = c
	}
	req.Cmd = unsafe.String(unsafe.SliceData(req.cmdBuf), n)
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
	count, err := parseIntBytes(line[1:])
	if err != nil {
		return nil, fmt.Errorf("invalid array length")
	}
	if count < 0 {
		return nil, fmt.Errorf("invalid array length")
	}
	if maxArrayLen := getParseMaxArrayLen(); maxArrayLen > 0 && count > maxArrayLen {
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
	req := acquireRequest()
	if cap(req.Args) < count-1 {
		req.Args = make([][]byte, 0, count-1)
	}
	for i := 1; i < count; i++ {
		arg, err := p.readBulkString()
		if err != nil {
			ReleaseRequest(req)
			return nil, err
		}
		req.Args = append(req.Args, []byte(arg))
	}

	setCmdUpperFromString(req, cmd)
	return req, nil
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
	length, err := parseIntBytes(line[1:])
	if err != nil {
		return "", fmt.Errorf("invalid bulk string length")
	}
	if length < -1 {
		return "", fmt.Errorf("invalid bulk string length")
	}
	if maxBulkLen := getParseMaxBulkLen(); maxBulkLen > 0 && length > maxBulkLen {
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
	if count < 0 {
		return nil, 0, fmt.Errorf("invalid array length")
	}
	if maxArrayLen := getParseMaxArrayLen(); maxArrayLen > 0 && count > maxArrayLen {
		return nil, 0, fmt.Errorf("invalid array length")
	}

	if count == 0 {
		return nil, 0, fmt.Errorf("empty array")
	}

	// 3. 循环解析每个参数
	offset := idx + 1
	args := make([][]byte, 0, count)

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
		if length < -1 {
			return nil, 0, fmt.Errorf("invalid bulk string length")
		}
		if maxBulkLen := getParseMaxBulkLen(); maxBulkLen > 0 && length > maxBulkLen {
			return nil, 0, fmt.Errorf("invalid bulk string length")
		}

		offset = nextIdx + 1

		if length == -1 {
			args = append(args, nil)
			continue
		}

		// 检查数据是否足够
		// 需要 length + 2 (for \r\n)
		if offset+length+2 > len(data) {
			return nil, 0, nil
		}

		arg := append([]byte(nil), data[offset:offset+length]...)
		args = append(args, arg)

		offset += length + 2
	}

	if len(args) == 0 {
		return nil, 0, fmt.Errorf("empty request")
	}

	req := acquireRequest()
	setCmdUpperFromString(req, string(args[0]))
	if cap(req.Args) < len(args)-1 {
		req.Args = make([][]byte, 0, len(args)-1)
	}
	req.Args = append(req.Args, args[1:]...)

	return req, offset, nil
}

func ParseOneRequestFast(data []byte) (*Request, int, error) {
	if len(data) == 0 {
		return nil, 0, nil
	}
	if data[0] != Array {
		return nil, 0, fmt.Errorf("invalid protocol: expected array")
	}

	idx := bytes.IndexByte(data, '\n')
	if idx == -1 {
		return nil, 0, nil
	}

	line := data[1:idx]
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	count, err := parseIntBytes(line)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid array length")
	}
	if count < 0 {
		return nil, 0, fmt.Errorf("invalid array length")
	}
	if maxArrayLen := getParseMaxArrayLen(); maxArrayLen > 0 && count > maxArrayLen {
		return nil, 0, fmt.Errorf("invalid array length")
	}
	if count == 0 {
		return nil, 0, fmt.Errorf("empty array")
	}

	offset := idx + 1
	req := acquireRequest()
	if cap(req.Args) < count-1 {
		req.Args = make([][]byte, 0, count-1)
	}

	var cmdBytes []byte
	for i := 0; i < count; i++ {
		if offset >= len(data) {
			ReleaseRequest(req)
			return nil, 0, nil
		}
		if data[offset] != BulkString {
			ReleaseRequest(req)
			return nil, 0, fmt.Errorf("expected bulk string")
		}

		nextIdx := bytes.IndexByte(data[offset:], '\n')
		if nextIdx == -1 {
			ReleaseRequest(req)
			return nil, 0, nil
		}
		nextIdx += offset

		line := data[offset+1 : nextIdx]
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		length, err := parseIntBytes(line)
		if err != nil {
			ReleaseRequest(req)
			return nil, 0, fmt.Errorf("invalid bulk string length")
		}
		if length < -1 {
			ReleaseRequest(req)
			return nil, 0, fmt.Errorf("invalid bulk string length")
		}
		if maxBulkLen := getParseMaxBulkLen(); maxBulkLen > 0 && length > maxBulkLen {
			ReleaseRequest(req)
			return nil, 0, fmt.Errorf("invalid bulk string length")
		}

		offset = nextIdx + 1
		if length == -1 {
			if i == 0 {
				cmdBytes = nil
			} else {
				req.Args = append(req.Args, nil)
			}
			continue
		}

		if offset+length+2 > len(data) {
			ReleaseRequest(req)
			return nil, 0, nil
		}

		if i == 0 {
			cmdBytes = data[offset : offset+length]
		} else {
			req.Args = append(req.Args, data[offset:offset+length])
		}
		offset += length + 2
	}

	if len(cmdBytes) == 0 {
		ReleaseRequest(req)
		return nil, 0, fmt.Errorf("empty request")
	}

	setCmdUpperFromBytes(req, cmdBytes)
	return req, offset, nil
}

// EncodeResponse 编码响应
func EncodeResponse(resp *Response) []byte {
	dst := make([]byte, 0, 256)
	return EncodeResponseInto(dst, resp)
}

func EncodeResponseInto(dst []byte, resp *Response) []byte {
	dst = dst[:0]
	appendResponse(&dst, resp)
	return dst
}

func AppendResponse(dst []byte, resp *Response) []byte {
	appendResponse(&dst, resp)
	return dst
}

func appendCRLF(dst *[]byte) {
	*dst = append(*dst, '\r', '\n')
}

func appendInt(dst *[]byte, v int64) {
	*dst = strconv.AppendInt(*dst, v, 10)
}

func appendUint(dst *[]byte, v uint64) {
	*dst = strconv.AppendUint(*dst, v, 10)
}

func appendBulkString(dst *[]byte, s string) {
	*dst = append(*dst, BulkString)
	appendInt(dst, int64(len(s)))
	appendCRLF(dst)
	*dst = append(*dst, s...)
	appendCRLF(dst)
}

func appendResponse(dst *[]byte, resp *Response) {
	switch resp.Type {
	case SimpleString:
		*dst = append(*dst, SimpleString)
		*dst = append(*dst, resp.Value.(string)...)
		appendCRLF(dst)
	case Error:
		*dst = append(*dst, Error)
		if resp.Error != nil {
			*dst = append(*dst, resp.Error.Error()...)
		} else if s, ok := resp.Value.(string); ok {
			*dst = append(*dst, s...)
		} else {
			*dst = append(*dst, fmt.Sprintf("%v", resp.Value)...)
		}
		appendCRLF(dst)
	case Integer:
		*dst = append(*dst, Integer)
		switch v := resp.Value.(type) {
		case int64:
			appendInt(dst, v)
		case int:
			appendInt(dst, int64(v))
		case uint64:
			appendUint(dst, v)
		default:
			appendInt(dst, 0)
		}
		appendCRLF(dst)
	case BulkString:
		if resp.Value == nil {
			*dst = append(*dst, BulkString)
			*dst = append(*dst, '-', '1')
			appendCRLF(dst)
			return
		}
		switch v := resp.Value.(type) {
		case string:
			appendBulkString(dst, v)
		case []byte:
			*dst = append(*dst, BulkString)
			appendInt(dst, int64(len(v)))
			appendCRLF(dst)
			*dst = append(*dst, v...)
			appendCRLF(dst)
		default:
			appendBulkString(dst, fmt.Sprintf("%v", resp.Value))
		}
	case Array:
		*dst = append(*dst, Array)
		switch arr := resp.Value.(type) {
		case []string:
			appendInt(dst, int64(len(arr)))
			appendCRLF(dst)
			for _, item := range arr {
				appendBulkString(dst, item)
			}
		case []*Response:
			appendInt(dst, int64(len(arr)))
			appendCRLF(dst)
			for _, item := range arr {
				if item == nil {
					*dst = append(*dst, BulkString)
					*dst = append(*dst, '-', '1')
					appendCRLF(dst)
					continue
				}
				appendResponse(dst, item)
			}
		default:
			appendInt(dst, 0)
			appendCRLF(dst)
		}
	}
}

// MakeSimpleString 创建简单字符串响应
func MakeSimpleString(value string) *Response {
	resp := acquireResponse()
	resp.Type = SimpleString
	resp.Value = value
	return resp
}

// MakeError 创建错误响应
func MakeError(err error) *Response {
	resp := acquireResponse()
	resp.Type = Error
	resp.Error = err
	return resp
}

// MakeInteger 创建整数响应
func MakeInteger(value int64) *Response {
	resp := acquireResponse()
	resp.Type = Integer
	resp.Value = value
	return resp
}

// MakeBulkString 创建批量字符串响应
func MakeBulkString(value string) *Response {
	resp := acquireResponse()
	resp.Type = BulkString
	resp.Value = value
	return resp
}

// MakeArray 创建数组响应
func MakeArray(items []string) *Response {
	resp := acquireResponse()
	resp.Type = Array
	resp.Value = items
	return resp
}

func MakeArrayResponses(items []*Response) *Response {
	resp := acquireResponse()
	resp.Type = Array
	resp.Value = items
	return resp
}

// MakeNull 创建空响应
func MakeNull() *Response {
	resp := acquireResponse()
	resp.Type = BulkString
	resp.Value = nil
	return resp
}
