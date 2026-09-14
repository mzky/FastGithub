package logger

import (
	"bytes"
	"testing"
)

func TestNewBuffer(t *testing.T) {
	b := NewBuffer(100)
	if b == nil {
		t.Fatal("NewBuffer returned nil")
	}
	if b.max != 100 {
		t.Errorf("expected max 100, got %d", b.max)
	}
	if len(b.entries) != 0 {
		t.Error("buffer should start empty")
	}
}

func TestNewBufferDefault(t *testing.T) {
	b := NewBuffer(0)
	if b.max != 500 {
		t.Errorf("expected default max 500, got %d", b.max)
	}
}

func TestLogLevels(t *testing.T) {
	b := NewBuffer(10)

	b.Info("info message")
	b.Warn("warn message")
	b.Error("error message")
	b.Debug("debug message")

	entries := b.Entries()
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	expectedLevels := []string{"INFO", "WARN", "ERROR", "DEBUG"}
	for i, level := range expectedLevels {
		if entries[i].Level != level {
			t.Errorf("entry %d: expected level %s, got %s", i, level, entries[i].Level)
		}
	}
}

func TestLogFormat(t *testing.T) {
	b := NewBuffer(10)
	b.Info("hello %s", "world")

	entries := b.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Message != "hello world" {
		t.Errorf("expected 'hello world', got %q", entries[0].Message)
	}
	if entries[0].Time == "" {
		t.Error("time should not be empty")
	}
}

func TestRingBuffer(t *testing.T) {
	b := NewBuffer(3)

	for i := 0; i < 5; i++ {
		b.Info("msg %d", i)
	}

	entries := b.Entries()
	if len(entries) != 3 {
		t.Errorf("expected 3 entries (ring buffer), got %d", len(entries))
	}
	// 最早的两条应该被丢弃
	if entries[0].Message != "msg 2" {
		t.Errorf("expected first entry 'msg 2', got %q", entries[0].Message)
	}
	if entries[2].Message != "msg 4" {
		t.Errorf("expected last entry 'msg 4', got %q", entries[2].Message)
	}
}

func TestLastN(t *testing.T) {
	b := NewBuffer(100)

	for i := 0; i < 10; i++ {
		b.Info("msg %d", i)
	}

	// N > 总条目数
	all := b.LastN(100)
	if len(all) != 10 {
		t.Errorf("LastN(100): expected 10, got %d", len(all))
	}

	// N < 总条目数
	last3 := b.LastN(3)
	if len(last3) != 3 {
		t.Errorf("LastN(3): expected 3, got %d", len(last3))
	}
	if last3[0].Message != "msg 7" {
		t.Errorf("expected first of last 3 to be 'msg 7', got %q", last3[0].Message)
	}
}

func TestClear(t *testing.T) {
	b := NewBuffer(10)
	b.Info("test")
	b.Clear()

	if len(b.Entries()) != 0 {
		t.Errorf("expected 0 entries after clear, got %d", len(b.Entries()))
	}
}

func TestAddWriter(t *testing.T) {
	b := NewBuffer(10)
	var buf bytes.Buffer
	b.AddWriter(&buf)

	b.Info("test writer")

	output := buf.String()
	if output == "" {
		t.Error("writer should receive log output")
	}
	if !bytes.Contains([]byte(output), []byte("test writer")) {
		t.Errorf("output should contain message, got %q", output)
	}
}

func TestSetConsoleOutput(t *testing.T) {
	b := NewBuffer(10)

	// 默认没有 stdout writer
	b.SetConsoleOutput(true)
	found := false
	for _, w := range b.writers {
		if w == nil {
			continue
		}
		found = true
	}
	if !found {
		t.Error("should have stdout writer after enabling")
	}

	// 禁用
	b.SetConsoleOutput(false)
	if len(b.writers) != 0 {
		t.Errorf("should have no writers after disabling, got %d", len(b.writers))
	}

	// 重复启用不应重复添加
	b.SetConsoleOutput(true)
	b.SetConsoleOutput(true)
	count := 0
	for range b.writers {
		count++
	}
	if count != 1 {
		t.Errorf("should have exactly 1 writer, got %d", count)
	}
}

func TestStdLogger(t *testing.T) {
	b := NewBuffer(10)
	logger := b.StdLogger("[test] ")

	logger.Print("hello")

	entries := b.Entries()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Message != "[test] hello" {
		t.Errorf("expected '[test] hello', got %q", entries[0].Message)
	}
	if entries[0].Level != "INFO" {
		t.Errorf("expected INFO level, got %s", entries[0].Level)
	}
}
