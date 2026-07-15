package logger

import (
	"log"
	"os"
	"path/filepath"
)

var (
	infoLogger  *log.Logger
	warnLogger  *log.Logger
	errorLogger *log.Logger
	debugLogger *log.Logger
)

// Init 初始化日志系统
func Init(logPath string, level string) error {
	// 确保日志目录存在
	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}

	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}

	infoLogger = log.New(file, "[INFO] ", log.Ldate|log.Ltime|log.Lshortfile)
	warnLogger = log.New(file, "[WARN] ", log.Ldate|log.Ltime|log.Lshortfile)
	errorLogger = log.New(file, "[ERROR] ", log.Ldate|log.Ltime|log.Lshortfile)
	debugLogger = log.New(file, "[DEBUG] ", log.Ldate|log.Ltime|log.Lshortfile)

	return nil
}

// Info 打印信息级别日志
func Info(format string, v ...interface{}) {
	if infoLogger != nil {
		infoLogger.Printf(format, v...)
	}
}

// Warn 打印警告级别日志
func Warn(format string, v ...interface{}) {
	if warnLogger != nil {
		warnLogger.Printf(format, v...)
	}
}

// Error 打印错误级别日志
func Error(format string, v ...interface{}) {
	if errorLogger != nil {
		errorLogger.Printf(format, v...)
	}
}

// Debug 打印调试级别日志
func Debug(format string, v ...interface{}) {
	if debugLogger != nil {
		debugLogger.Printf(format, v...)
	}
}
