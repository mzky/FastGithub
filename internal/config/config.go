package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Config 应用配置
type Config struct {
	Proxy  ProxyConfig  `json:"proxy"`
	UI     UIConfig     `json:"ui"`
	DNS    DNSConfig    `json:"dns"`
	SpeedTest SpeedTestConfig `json:"speed_test"`
	Update UpdateConfig `json:"update"`
	CACertPath string   `json:"ca_cert_path"`
	CAKeyPath  string   `json:"ca_key_path"`
	CertDir    string   `json:"cert_dir"`
	Domains    []DomainConfig `json:"domains"`
}

// ProxyConfig 代理配置
type ProxyConfig struct {
	Listen            string `json:"listen"`
	EnableSystemProxy *bool  `json:"enable_system_proxy"` // 启动时自动开启系统代理、退出时自动关闭；nil 时默认为 true
}

// SystemProxyEnabled 返回是否启用系统代理自动开关（默认为 true）
func (p *ProxyConfig) SystemProxyEnabled() bool {
	if p.EnableSystemProxy == nil {
		return true
	}
	return *p.EnableSystemProxy
}

// UIConfig Web UI 配置
type UIConfig struct {
	Listen string `json:"listen"`
}

// DNSConfig DNS 配置
type DNSConfig struct {
	Server   string   `json:"server"`
	Upstreams []string `json:"upstreams"`
}

// SpeedTestConfig 测速配置
type SpeedTestConfig struct {
	Concurrent int           `json:"concurrent"`
	Timeout    time.Duration `json:"timeout"`
	CacheTTL   time.Duration `json:"cache_ttl"`
}

// UpdateConfig 更新配置
type UpdateConfig struct {
	AutoCheck     bool          `json:"auto_check"`
	CheckInterval time.Duration `json:"check_interval"`
	Repo          string        `json:"repo"`
	Prerelease    bool          `json:"prerelease"`
}

// DomainConfig 域名配置
type DomainConfig struct {
	Name        string   `json:"name"`
	Patterns    []string `json:"patterns"`
	SNIPatterns []string `json:"sni_patterns"`
}

// Manager 配置管理器，支持热重载
type Manager struct {
	path string
	mu   sync.RWMutex
	cfg  *Config
}

// LoadManager 创建并加载配置管理器
func LoadManager(path string) (*Manager, error) {
	m := &Manager{path: path}
	if err := m.Load(); err != nil {
		return nil, err
	}
	return m, nil
}

// NewManager 创建配置管理器（别名，兼容旧代码）
func NewManager(path string) (*Manager, error) {
	return LoadManager(path)
}

// Load 从文件加载配置
func (m *Manager) Load() error {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return fmt.Errorf("read config %s: %w", m.path, err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// 设置默认值
	if cfg.Proxy.Listen == "" {
		cfg.Proxy.Listen = "127.0.0.1:38457"
	}
	if cfg.UI.Listen == "" {
		cfg.UI.Listen = "127.0.0.1:38458"
	}
	if cfg.DNS.Server == "" {
		cfg.DNS.Server = "223.5.5.5:53"
	}
	if len(cfg.DNS.Upstreams) == 0 {
		cfg.DNS.Upstreams = []string{"8.8.8.8:53", "1.1.1.1:53"}
	}
	if cfg.SpeedTest.Concurrent == 0 {
		cfg.SpeedTest.Concurrent = 10
	}
	if cfg.SpeedTest.Timeout == 0 {
		cfg.SpeedTest.Timeout = 2 * time.Second
	}
	if cfg.SpeedTest.CacheTTL == 0 {
		cfg.SpeedTest.CacheTTL = 5 * time.Minute
	}
	if cfg.CertDir == "" {
		cfg.CertDir = "cacert"
	}
	if cfg.CACertPath == "" {
		cfg.CACertPath = filepath.Join(cfg.CertDir, "ca.crt")
	}
	if cfg.CAKeyPath == "" {
		cfg.CAKeyPath = filepath.Join(cfg.CertDir, "ca.key")
	}
	if cfg.Update.Repo == "" {
		cfg.Update.Repo = "creazyboyone/fastgithub"
	}
	if cfg.Update.CheckInterval == 0 {
		cfg.Update.CheckInterval = 24 * time.Hour
	}

	m.mu.Lock()
	m.cfg = &cfg
	m.mu.Unlock()
	return nil
}

// Get 获取当前配置的副本
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg := *m.cfg
	return &cfg
}

// FindDomain 根据主机名查找匹配的域名配置
func (m *Manager) FindDomain(host string) *DomainConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	host = strings.ToLower(strings.TrimSpace(host))
	for i := range m.cfg.Domains {
		for _, pattern := range m.cfg.Domains[i].Patterns {
			if matchPattern(host, pattern) {
				return &m.cfg.Domains[i]
			}
		}
	}
	return nil
}

// ResolveSNI 返回域名应使用的 SNI 覆盖值，若无覆盖则返回空
func (m *Manager) ResolveSNI(host string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	host = strings.ToLower(strings.TrimSpace(host))
	for i := range m.cfg.Domains {
		matched := false
		for _, pattern := range m.cfg.Domains[i].Patterns {
			if matchPattern(host, pattern) {
				matched = true
				break
			}
		}
		if matched && len(m.cfg.Domains[i].SNIPatterns) > 0 {
			return m.cfg.Domains[i].SNIPatterns[0]
		}
	}
	return ""
}

// CertBasePath 返回证书目录的绝对路径
func (m *Manager) CertBasePath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if filepath.IsAbs(m.cfg.CertDir) {
		return m.cfg.CertDir
	}
	base := filepath.Dir(m.path)
	return filepath.Join(base, m.cfg.CertDir)
}

// GetConfigPath 返回配置文件路径
func (m *Manager) GetConfigPath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.path
}

// matchPattern 支持通配符 * 的域名匹配
func matchPattern(host, pattern string) bool {
	host = strings.ToLower(host)
	pattern = strings.ToLower(pattern)

	if pattern == host {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:]
		return strings.HasSuffix(host, suffix)
	}
	return false
}
