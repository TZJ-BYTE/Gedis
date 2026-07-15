package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

func main() {
	host := "127.0.0.1"
	port := 16379 // 修改为与服务器一致的端口

	if len(os.Args) > 1 {
		host = os.Args[1]
	}
	if len(os.Args) > 2 {
		fmt.Sscanf(os.Args[2], "%d", &port)
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		fmt.Printf("连接失败：%v\n", err)
		os.Exit(1)
	}
	defer conn.Close()

	fmt.Println("RediGo 客户端")
	fmt.Printf("已连接到 %s\n", addr)
	fmt.Println("输入命令开始交互，输入 'exit' 退出")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Print("redigo> ")

		input, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("读取输入失败：%v\n", err)
			break
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if strings.ToLower(input) == "exit" || strings.ToLower(input) == "quit" {
			break
		}

		// 解析命令
		parts := strings.Fields(input)
		if len(parts) == 0 {
			continue
		}

		// 构建请求
		cmd := parts[0]
		args := parts[1:]

		// 发送请求
		request := buildRequest(cmd, args)
		conn.Write(request)

		// 接收响应
		resp, err := readResponse(conn)
		if err != nil {
			fmt.Printf("接收响应失败：%v\n", err)
			continue
		}

		fmt.Printf("%s\n", resp)
	}
}

// readResponse 读取服务器响应
func readResponse(conn net.Conn) (string, error) {
	reader := bufio.NewReader(conn)

	line, err := reader.ReadBytes('\n')
	if err != nil {
		return "", err
	}

	if len(line) < 2 {
		return "", fmt.Errorf("invalid response")
	}

	respType := line[0]
	content := string(line[1 : len(line)-2]) // 移除 \r\n

	switch respType {
	case '+': // 简单字符串
		return content, nil
	case '-': // 错误
		return "ERR: " + content, nil
	case ':': // 整数
		return "(integer) " + content, nil
	case '$': // 批量字符串
		length := content
		if length == "-1" {
			return "(nil)", nil
		}
		// 读取实际内容
		size := 0
		fmt.Sscanf(length, "%d", &size)
		buf := make([]byte, size+2)
		_, err = reader.Read(buf)
		if err != nil {
			return "", err
		}
		return string(buf[:size]), nil
	case '*': // 数组
		count := 0
		fmt.Sscanf(content, "%d", &count)
		if count == -1 {
			return "(nil)", nil
		}

		result := make([]string, 0, count)
		for i := 0; i < count; i++ {
			// 读取每个元素
			elemLine, err := reader.ReadBytes('\n')
			if err != nil {
				return "", err
			}
			if len(elemLine) >= 2 {
				// 跳过类型标识符，解析内容
				elemContent := string(elemLine[2 : len(elemLine)-2])
				result = append(result, elemContent)
			}
		}
		return fmt.Sprintf("%v", result), nil
	default:
		return string(line), nil
	}
}

// buildRequest 构建 RESP 协议请求
func buildRequest(cmd string, args []string) []byte {
	var builder strings.Builder

	builder.WriteString("*")
	builder.WriteString(fmt.Sprintf("%d", len(args)+1))
	builder.WriteString("\r\n")

	builder.WriteString("$")
	builder.WriteString(fmt.Sprintf("%d", len(cmd)))
	builder.WriteString("\r\n")
	builder.WriteString(cmd)
	builder.WriteString("\r\n")

	for _, arg := range args {
		builder.WriteString("$")
		builder.WriteString(fmt.Sprintf("%d", len(arg)))
		builder.WriteString("\r\n")
		builder.WriteString(arg)
		builder.WriteString("\r\n")
	}

	return []byte(builder.String())
}
