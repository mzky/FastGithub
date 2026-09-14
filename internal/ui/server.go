package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/creazyboyone/fastgithub/internal/flow"
	"github.com/creazyboyone/fastgithub/internal/logger"
	"github.com/creazyboyone/fastgithub/internal/version"
)

// Server Web UI 服务器
type Server struct {
	flow   *flow.Analyzer
	logBuf *logger.Buffer
	http   *http.Server
}

// NewServer 创建 Web UI 服务器
func NewServer(flow *flow.Analyzer, logBuf *logger.Buffer) *Server {
	s := &Server{
		flow:   flow,
		logBuf: logBuf,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/logs", s.handleLogs)
	mux.HandleFunc("/api/version", s.handleVersion)

	s.http = &http.Server{
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s
}

// Run 启动 Web UI 服务器
func (s *Server) Run(addr string) error {
	s.http.Addr = addr
	return s.http.ListenAndServe()
}

// Stop 停止 Web UI 服务器
func (s *Server) Stop() error {
	return s.http.Close()
}

// handleIndex 返回主页面
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

// handleStats 返回流量统计 JSON
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.flow.Snapshot()
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_upload":    stats.TotalUpload,
		"total_download":  stats.TotalDownload,
		"upload_speed":    stats.UploadSpeed,
		"download_speed":  stats.DownloadSpeed,
		"conn_count":      stats.ConnCount,
		"start_time":      stats.StartTime,
		"uptime":          int(time.Now().Unix() - stats.StartTime),
		"total_upload_h":  flow.FormatBytes(stats.TotalUpload),
		"total_download_h": flow.FormatBytes(stats.TotalDownload),
		"upload_speed_h":  flow.FormatBytes(stats.UploadSpeed) + "/s",
		"download_speed_h": flow.FormatBytes(stats.DownloadSpeed) + "/s",
	})
}

// handleLogs 返回日志 JSON
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	n := 100
	if v := r.URL.Query().Get("n"); v != "" {
		if num, err := strconv.Atoi(v); err == nil && num > 0 {
			n = num
		}
	}

	entries := s.logBuf.LastN(n)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(entries)
}

// handleVersion 返回版本信息
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]string{
		"version":   version.Version,
		"build_time": version.BuildTime,
	})
}

// indexHTML 主页面 HTML
var indexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>FastGithub 状态面板</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body {
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
  background: #f5f7fa;
  color: #333;
  padding: 20px;
}
.container { max-width: 960px; margin: 0 auto; }
header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
  padding-bottom: 15px;
  border-bottom: 1px solid #e4e7ed;
}
header h1 { font-size: 20px; color: #409eff; }
header .version { font-size: 12px; color: #909399; }
.cards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 15px;
  margin-bottom: 20px;
}
.card {
  background: #fff;
  border-radius: 8px;
  padding: 18px;
  box-shadow: 0 2px 8px rgba(0,0,0,0.06);
}
.card .label { font-size: 13px; color: #909399; margin-bottom: 8px; }
.card .value { font-size: 24px; font-weight: 600; color: #303133; }
.card .value.green { color: #67c23a; }
.card .value.blue { color: #409eff; }
.card .value.orange { color: #e6a23c; }
.log-panel {
  background: #fff;
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0,0,0,0.06);
  overflow: hidden;
}
.log-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 18px;
  border-bottom: 1px solid #ebeef5;
  background: #fafafa;
}
.log-header h3 { font-size: 15px; color: #303133; }
.log-header .actions button {
  background: #409eff;
  color: #fff;
  border: none;
  padding: 6px 14px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
}
.log-header .actions button:hover { background: #66b1ff; }
.log-body {
  height: 400px;
  overflow-y: auto;
  padding: 10px 18px;
  font-family: "Consolas", "Monaco", monospace;
  font-size: 12px;
  line-height: 1.8;
  background: #1e1e1e;
  color: #d4d4d4;
}
.log-entry { display: flex; gap: 10px; }
.log-entry .time { color: #6a9955; flex-shrink: 0; }
.log-entry .level { flex-shrink: 0; min-width: 50px; font-weight: bold; }
.log-entry .level.INFO { color: #569cd6; }
.log-entry .level.WARN { color: #dcdcaa; }
.log-entry .level.ERROR { color: #f44747; }
.log-entry .level.DEBUG { color: #c586c0; }
.log-entry .msg { color: #d4d4d4; word-break: break-all; }
.status-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #67c23a;
  margin-right: 6px;
  animation: pulse 2s infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.4; }
}
</style>
</head>
<body>
<div class="container">
  <header>
    <h1><span class="status-dot"></span>FastGithub 状态面板</h1>
    <div class="version" id="version">v0.0.0</div>
  </header>

  <div class="cards">
    <div class="card">
      <div class="label">上行速率</div>
      <div class="value green" id="upload-speed">0 B/s</div>
    </div>
    <div class="card">
      <div class="label">下行速率</div>
      <div class="value blue" id="download-speed">0 B/s</div>
    </div>
    <div class="card">
      <div class="label">累计上行</div>
      <div class="value orange" id="total-upload">0 B</div>
    </div>
    <div class="card">
      <div class="label">累计下行</div>
      <div class="value orange" id="total-download">0 B</div>
    </div>
    <div class="card">
      <div class="label">活跃连接</div>
      <div class="value" id="conn-count">0</div>
    </div>
    <div class="card">
      <div class="label">运行时间</div>
      <div class="value" id="uptime">0s</div>
    </div>
  </div>

  <div class="log-panel">
    <div class="log-header">
      <h3>运行日志</h3>
      <div class="actions">
        <button onclick="clearLogs()">清空</button>
      </div>
    </div>
    <div class="log-body" id="log-body"></div>
  </div>
</div>

<script>
let lastLogCount = 0;
let autoScroll = true;

const logBody = document.getElementById('log-body');
logBody.addEventListener('scroll', () => {
  autoScroll = logBody.scrollTop + logBody.clientHeight >= logBody.scrollHeight - 10;
});

function formatUptime(seconds) {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  if (d > 0) return d + '天 ' + h + '时 ' + m + '分';
  if (h > 0) return h + '时 ' + m + '分 ' + s + '秒';
  if (m > 0) return m + '分 ' + s + '秒';
  return s + '秒';
}

async function loadStats() {
  try {
    const res = await fetch('/api/stats');
    const data = await res.json();
    document.getElementById('upload-speed').textContent = data.upload_speed_h;
    document.getElementById('download-speed').textContent = data.download_speed_h;
    document.getElementById('total-upload').textContent = data.total_upload_h;
    document.getElementById('total-download').textContent = data.total_download_h;
    document.getElementById('conn-count').textContent = data.conn_count;
    document.getElementById('uptime').textContent = formatUptime(data.uptime);
  } catch(e) {}
}

async function loadLogs() {
  try {
    const res = await fetch('/api/logs?n=200');
    const data = await res.json();
    if (data.length === lastLogCount) return;
    lastLogCount = data.length;
    logBody.innerHTML = data.map(e =>
      '<div class="log-entry"><span class="time">' + e.time + '</span>' +
      '<span class="level ' + e.level + '">[' + e.level + ']</span>' +
      '<span class="msg">' + escapeHtml(e.message) + '</span></div>'
    ).join('');
    if (autoScroll) {
      logBody.scrollTop = logBody.scrollHeight;
    }
  } catch(e) {}
}

function escapeHtml(s) {
  const div = document.createElement('div');
  div.textContent = s;
  return div.innerHTML;
}

async function loadVersion() {
  try {
    const res = await fetch('/api/version');
    const data = await res.json();
    document.getElementById('version').textContent = 'v' + data.version;
  } catch(e) {}
}

function clearLogs() {
  logBody.innerHTML = '';
  lastLogCount = 0;
}

loadVersion();
loadStats();
loadLogs();
setInterval(loadStats, 1000);
setInterval(loadLogs, 2000);
</script>
</body>
</html>`

// ListenAddr 计算 UI 监听地址（代理端口 +1）
func ListenAddr(proxyAddr string) (string, error) {
	host, portStr, err := splitHostPort(proxyAddr)
	if err != nil {
		return "", err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", host, port+1), nil
}

func splitHostPort(addr string) (string, string, error) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:], nil
		}
	}
	return "", "", fmt.Errorf("invalid addr: %s", addr)
}
