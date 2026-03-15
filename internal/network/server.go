package network

// Server 定义服务器通用接口
type Server interface {
	// Start 启动服务器，阻塞直到服务器停止
	Start() error

	// Stop 停止服务器
	Stop() error
}
