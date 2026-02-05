package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config 日志配置
type Config struct {
	// 日志目录
	LogDir string
	// 日志文件名后缀（如 app.log → 20260105-app.log）
	LogFile string
	// 单个日志文件最大大小 (MB) - 保留用于兼容，但日期轮转不使用
	MaxSize int
	// 保留的旧日志文件最大数量
	MaxBackups int
	// 保留的旧日志文件最大天数
	MaxAge int
	// 是否压缩旧日志文件 - 保留用于兼容，日期轮转暂不支持
	Compress bool
	// 是否同时输出到控制台
	Console bool
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		LogDir:     "logs",
		LogFile:    "app.log",
		MaxSize:    100, // 100MB (保留兼容)
		MaxBackups: 10,
		MaxAge:     30, // 30 days
		Compress:   true,
		Console:    true,
	}
}

// DailyWriter 按日期轮转的日志写入器
type DailyWriter struct {
	mu          sync.Mutex
	logDir      string
	logSuffix   string // 文件名后缀，如 "app.log"
	maxAge      int    // 保留天数
	currentDate string // 当前日期 YYYYMMDD
	file        *os.File
}

// NewDailyWriter 创建按日期轮转的日志写入器
func NewDailyWriter(logDir, logSuffix string, maxAge int) *DailyWriter {
	return &DailyWriter{
		logDir:    logDir,
		logSuffix: logSuffix,
		maxAge:    maxAge,
	}
}

// getDateString 获取当前日期字符串 YYYYMMDD
func getDateString() string {
	return time.Now().Format("20060102")
}

// getFilename 根据日期生成文件名
func (w *DailyWriter) getFilename(date string) string {
	return filepath.Join(w.logDir, fmt.Sprintf("%s-%s", date, w.logSuffix))
}

// Write 实现 io.Writer 接口
func (w *DailyWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	currentDate := getDateString()

	// 检查是否需要轮转（日期变化或文件未打开）
	if w.file == nil || w.currentDate != currentDate {
		if err := w.rotate(currentDate); err != nil {
			return 0, err
		}
	}

	return w.file.Write(p)
}

// rotate 轮转到新的日志文件
func (w *DailyWriter) rotate(newDate string) error {
	// 关闭旧文件
	if w.file != nil {
		w.file.Close()
	}

	// 打开新文件
	filename := w.getFilename(newDate)
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	w.file = file
	w.currentDate = newDate
	return nil
}

// Close 关闭日志文件
func (w *DailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// Cleanup 清理过期的日志文件
func (w *DailyWriter) Cleanup() error {
	if w.maxAge <= 0 {
		return nil
	}

	cutoff := time.Now().AddDate(0, 0, -w.maxAge)
	cutoffDate := cutoff.Format("20060102")

	entries, err := os.ReadDir(w.logDir)
	if err != nil {
		return fmt.Errorf("failed to read log directory: %w", err)
	}

	var deleted int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// 匹配格式: YYYYMMDD-suffix
		if !strings.HasSuffix(name, "-"+w.logSuffix) {
			continue
		}

		// 提取日期部分
		dateStr := strings.TrimSuffix(name, "-"+w.logSuffix)
		if len(dateStr) != 8 {
			continue
		}

		// 比较日期
		if dateStr < cutoffDate {
			path := filepath.Join(w.logDir, name)
			if err := os.Remove(path); err != nil {
				log.Printf("⚠️ 删除过期日志失败: %s: %v", path, err)
			} else {
				deleted++
			}
		}
	}

	if deleted > 0 {
		log.Printf("🗑️ 已清理 %d 个过期日志文件", deleted)
	}

	return nil
}

// ListLogFiles 列出所有日志文件（按日期排序）
func (w *DailyWriter) ListLogFiles() ([]string, error) {
	entries, err := os.ReadDir(w.logDir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.HasSuffix(name, "-"+w.logSuffix) {
			files = append(files, filepath.Join(w.logDir, name))
		}
	}

	sort.Strings(files)
	return files, nil
}

// Setup 初始化日志系统
func Setup(cfg *Config) error {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// 确保日志目录存在
	if err := os.MkdirAll(cfg.LogDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	// 创建按日期轮转的日志写入器
	dailyWriter := NewDailyWriter(cfg.LogDir, cfg.LogFile, cfg.MaxAge)

	var writer io.Writer
	if cfg.Console {
		// 同时输出到控制台和文件
		writer = io.MultiWriter(os.Stdout, dailyWriter)
	} else {
		// 仅输出到文件
		writer = dailyWriter
	}

	// 设置标准库 log 的输出
	log.SetOutput(writer)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	currentFile := dailyWriter.getFilename(getDateString())
	log.Printf("📝 日志系统已初始化")
	log.Printf("📂 日志文件: %s", currentFile)
	log.Printf("📊 轮转配置: 按日期轮转, 保留 %d 天", cfg.MaxAge)

	// 启动后台清理协程
	go func() {
		// 启动时立即清理一次
		if err := dailyWriter.Cleanup(); err != nil {
			log.Printf("⚠️ 日志清理失败: %v", err)
		}

		// 每小时检查一次
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for range ticker.C {
			if err := dailyWriter.Cleanup(); err != nil {
				log.Printf("⚠️ 日志清理失败: %v", err)
			}
		}
	}()

	return nil
}
