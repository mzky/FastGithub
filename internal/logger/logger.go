package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"time"
)

// Entry 日志条目
type Entry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Buffer 环形日志缓冲
type Buffer struct {
	mu      sync.RWMutex
	entries []Entry
	max     int
	writers []io.Writer
}

// NewBuffer 创建日志缓冲
func NewBuffer(maxEntries int) *Buffer {
	if maxEntries <= 0 {
		maxEntries = 500
	}
	return &Buffer{
		entries: make([]Entry, 0, maxEntries),
		max:     maxEntries,
	}
}

// AddWriter 添加输出目标
func (b *Buffer) AddWriter(w io.Writer) {
	b.mu.Lock()
	b.writers = append(b.writers, w)
	b.mu.Unlock()
}

// Info 记录 INFO 级别日志
func (b *Buffer) Info(format string, args ...interface{}) {
	b.push("INFO", fmt.Sprintf(format, args...))
}

// Warn 记录 WARN 级别日志
func (b *Buffer) Warn(format string, args ...interface{}) {
	b.push("WARN", fmt.Sprintf(format, args...))
}

// Error 记录 ERROR 级别日志
func (b *Buffer) Error(format string, args ...interface{}) {
	b.push("ERROR", fmt.Sprintf(format, args...))
}

// Debug 记录 DEBUG 级别日志
func (b *Buffer) Debug(format string, args ...interface{}) {
	b.push("DEBUG", fmt.Sprintf(format, args...))
}

func (b *Buffer) push(level, msg string) {
	entry := Entry{
		Time:    time.Now().Format("2006-01-02 15:04:05"),
		Level:   level,
		Message: msg,
	}

	b.mu.Lock()
	if len(b.entries) >= b.max {
		b.entries = b.entries[1:]
	}
	b.entries = append(b.entries, entry)

	// 同步写入到所有 writer
	line := fmt.Sprintf("[%s] [%s] %s\n", entry.Time, entry.Level, entry.Message)
	writers := b.writers
	b.mu.Unlock()

	for _, w := range writers {
		w.Write([]byte(line))
	}
}

// Entries 获取所有日志条目
func (b *Buffer) Entries() []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]Entry, len(b.entries))
	copy(result, b.entries)
	return result
}

// LastN 获取最近 N 条日志
func (b *Buffer) LastN(n int) []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n >= len(b.entries) {
		result := make([]Entry, len(b.entries))
		copy(result, b.entries)
		return result
	}
	result := make([]Entry, n)
	copy(result, b.entries[len(b.entries)-n:])
	return result
}

// Clear 清空日志
func (b *Buffer) Clear() {
	b.mu.Lock()
	b.entries = b.entries[:0]
	b.mu.Unlock()
}

// StdLogger 返回标准 log.Logger，适配 Go 标准库 log
func (b *Buffer) StdLogger(prefix string) *log.Logger {
	writer := &bufferWriter{buf: b, level: "INFO", prefix: prefix}
	return log.New(writer, "", 0)
}

type bufferWriter struct {
	buf    *Buffer
	level  string
	prefix string
}

func (w *bufferWriter) Write(p []byte) (int, error) {
	msg := string(p)
	// 去掉末尾换行
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	if w.prefix != "" {
		msg = w.prefix + msg
	}
	w.buf.push(w.level, msg)
	return len(p), nil
}

// SetConsoleOutput 设置是否输出到控制台
func (b *Buffer) SetConsoleOutput(enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if enabled {
		// 添加 stdout writer（去重）
		found := false
		for _, w := range b.writers {
			if w == os.Stdout {
				found = true
				break
			}
		}
		if !found {
			b.writers = append(b.writers, os.Stdout)
		}
	} else {
		// 移除 stdout writer
		newWriters := make([]io.Writer, 0, len(b.writers))
		for _, w := range b.writers {
			if w != os.Stdout {
				newWriters = append(newWriters, w)
			}
		}
		b.writers = newWriters
	}
}

// SetSilent 设置静默模式（不输出到控制台）
func SetSilent(silent bool) {
	if silent {
		log.SetOutput(io.Discard)
	} else {
		log.SetOutput(os.Stdout)
	}
}
