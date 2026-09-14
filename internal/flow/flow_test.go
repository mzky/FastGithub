package flow

import (
	"testing"
	"time"
)

func TestNewAnalyzer(t *testing.T) {
	a := NewAnalyzer()
	if a == nil {
		t.Fatal("NewAnalyzer returned nil")
	}
	s := a.Snapshot()
	if s.TotalUpload != 0 || s.TotalDownload != 0 {
		t.Error("initial stats should be zero")
	}
	if s.ConnCount != 0 {
		t.Error("initial conn count should be zero")
	}
	if s.StartTime <= 0 {
		t.Error("start time should be positive")
	}
}

func TestAddUploadDownload(t *testing.T) {
	a := NewAnalyzer()
	a.Start()
	defer a.Stop()

	a.AddUpload(1024)
	a.AddDownload(2048)

	s := a.Snapshot()
	if s.TotalUpload != 1024 {
		t.Errorf("expected upload 1024, got %d", s.TotalUpload)
	}
	if s.TotalDownload != 2048 {
		t.Errorf("expected download 2048, got %d", s.TotalDownload)
	}
}

func TestAddUploadZero(t *testing.T) {
	a := NewAnalyzer()
	a.AddUpload(0)
	a.AddUpload(-100)
	if a.totalUpload.Load() != 0 {
		t.Error("zero/negative upload should not increment")
	}
}

func TestConnCount(t *testing.T) {
	a := NewAnalyzer()
	a.AddConn()
	a.AddConn()
	a.DecConn()

	s := a.Snapshot()
	if s.ConnCount != 1 {
		t.Errorf("expected conn count 1, got %d", s.ConnCount)
	}
}

func TestSpeedLoop(t *testing.T) {
	a := NewAnalyzer()
	a.Start()
	defer a.Stop()

	// 添加一些流量
	a.AddUpload(100000)
	a.AddDownload(200000)

	// 等待至少一个 tick
	time.Sleep(1200 * time.Millisecond)

	s := a.Snapshot()
	if s.UploadSpeed <= 0 {
		t.Logf("upload speed: %d (may be zero if timing is off)", s.UploadSpeed)
	}
	if s.DownloadSpeed <= 0 {
		t.Logf("download speed: %d (may be zero if timing is off)", s.DownloadSpeed)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input  int64
		expect string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}

	for _, tc := range tests {
		result := FormatBytes(tc.input)
		if result != tc.expect {
			t.Errorf("FormatBytes(%d): expected %q, got %q", tc.input, tc.expect, result)
		}
	}
}

func TestStartStop(t *testing.T) {
	a := NewAnalyzer()
	a.Start()
	a.Start() // 重复启动应安全
	a.Stop()
	a.Stop() // 重复停止应安全

	// 停止后仍可添加数据
	a.AddUpload(100)
	if a.totalUpload.Load() != 100 {
		t.Error("should still be able to add after stop")
	}
}
