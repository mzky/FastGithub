package flow

import (
	"sync"
	"sync/atomic"
	"time"
)

// Stats 流量统计
type Stats struct {
	TotalUpload   int64 `json:"total_upload"`
	TotalDownload int64 `json:"total_download"`
	UploadSpeed   int64 `json:"upload_speed"`   // bytes per second
	DownloadSpeed int64 `json:"download_speed"` // bytes per second
	ConnCount     int64 `json:"conn_count"`
	StartTime     int64 `json:"start_time"`
}

// Analyzer 流量分析器
type Analyzer struct {
	totalUpload   atomic.Int64
	totalDownload atomic.Int64
	connCount     atomic.Int64
	startTime     time.Time

	lastUpload    int64
	lastDownload  int64
	lastTime      time.Time
	uploadSpeed   atomic.Int64
	downloadSpeed atomic.Int64

	mu       sync.Mutex
	started  bool
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewAnalyzer 创建流量分析器（需手动调用 Start）
func NewAnalyzer() *Analyzer {
	return &Analyzer{
		startTime: time.Now(),
		lastTime:  time.Now(),
		stopCh:    make(chan struct{}),
	}
}

// Start 启动速率计算循环
func (a *Analyzer) Start() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		return
	}
	a.started = true
	a.lastTime = time.Now()
	go a.speedLoop()
}

// Stop 停止速率计算
func (a *Analyzer) Stop() {
	a.stopOnce.Do(func() {
		close(a.stopCh)
	})
}

// AddUpload 增加上传流量
func (a *Analyzer) AddUpload(n int64) {
	if n <= 0 {
		return
	}
	a.totalUpload.Add(n)
}

// AddDownload 增加下载流量
func (a *Analyzer) AddDownload(n int64) {
	if n <= 0 {
		return
	}
	a.totalDownload.Add(n)
}

// AddConn 增加连接计数
func (a *Analyzer) AddConn() {
	a.connCount.Add(1)
}

// DecConn 减少连接计数
func (a *Analyzer) DecConn() {
	a.connCount.Add(-1)
}

// Snapshot 获取当前统计快照
func (a *Analyzer) Snapshot() Stats {
	return Stats{
		TotalUpload:   a.totalUpload.Load(),
		TotalDownload: a.totalDownload.Load(),
		UploadSpeed:   a.uploadSpeed.Load(),
		DownloadSpeed: a.downloadSpeed.Load(),
		ConnCount:     a.connCount.Load(),
		StartTime:     a.startTime.Unix(),
	}
}

// speedLoop 每秒计算速率
func (a *Analyzer) speedLoop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopCh:
			return
		case <-ticker.C:
			now := time.Now()
			elapsed := now.Sub(a.lastTime).Seconds()
			if elapsed <= 0 {
				continue
			}

			curUpload := a.totalUpload.Load()
			curDownload := a.totalDownload.Load()

			uploadDelta := curUpload - a.lastUpload
			downloadDelta := curDownload - a.lastDownload

			a.uploadSpeed.Store(int64(float64(uploadDelta) / elapsed))
			a.downloadSpeed.Store(int64(float64(downloadDelta) / elapsed))

			a.lastUpload = curUpload
			a.lastDownload = curDownload
			a.lastTime = now
		}
	}
}

// FormatBytes 格式化字节数为人类可读形式
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return formatInt(b) + " B"
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return formatFloat(float64(b)/float64(div)) + " " + string("KMGTPE"[exp]) + "B"
}

func formatInt(n int64) string {
	return itoa(n)
}

func formatFloat(f float64) string {
	whole := int64(f)
	frac := int64((f - float64(whole)) * 10)
	if frac < 0 {
		frac = -frac
	}
	return itoa(whole) + "." + itoa(frac)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
