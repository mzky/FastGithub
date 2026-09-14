package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManager(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfgContent := `{
  "proxy": { "listen": "127.0.0.1:38457" },
  "cert_dir": "cacert",
  "dns": { "upstreams": ["8.8.8.8:53"] },
  "update": { "repo": "test/repo" },
  "domains": [
    {
      "name": "github",
      "patterns": ["*.github.com", "github.com"],
      "sni_patterns": ["github.com"]
    }
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(cfgPath)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}

	cfg := m.Get()
	if cfg.Proxy.Listen != "127.0.0.1:38457" {
		t.Errorf("expected listen 127.0.0.1:38457, got %s", cfg.Proxy.Listen)
	}
	if len(cfg.DNS.Upstreams) != 1 {
		t.Errorf("expected 1 dns upstream, got %d", len(cfg.DNS.Upstreams))
	}
	if cfg.Update.Repo != "test/repo" {
		t.Errorf("expected update repo test/repo, got %s", cfg.Update.Repo)
	}
}

func TestFindDomain(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfgContent := `{
  "domains": [
    {
      "name": "github",
      "patterns": ["*.github.com", "github.com"],
      "sni_patterns": ["github.com"]
    },
    {
      "name": "example",
      "patterns": ["*.example.com"],
      "sni_patterns": []
    }
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		host   string
		expect string
	}{
		{"github.com", "github"},
		{"api.github.com", "github"},
		{"GITHUB.COM", "github"},
		{"example.com", ""},
		{"sub.example.com", "example"},
		{"other.com", ""},
	}

	for _, tc := range tests {
		d := m.FindDomain(tc.host)
		if tc.expect == "" {
			if d != nil {
				t.Errorf("FindDomain(%s): expected nil, got %s", tc.host, d.Name)
			}
		} else {
			if d == nil {
				t.Errorf("FindDomain(%s): expected %s, got nil", tc.host, tc.expect)
			} else if d.Name != tc.expect {
				t.Errorf("FindDomain(%s): expected %s, got %s", tc.host, tc.expect, d.Name)
			}
		}
	}
}

func TestResolveSNI(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfgContent := `{
  "domains": [
    {
      "name": "github",
      "patterns": ["*.github.com"],
      "sni_patterns": ["github.com"]
    },
    {
      "name": "no-sni",
      "patterns": ["nosni.com"],
      "sni_patterns": []
    }
  ]
}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		host   string
		expect string
	}{
		{"api.github.com", "github.com"},
		{"nosni.com", ""},
		{"other.com", ""},
	}

	for _, tc := range tests {
		sni := m.ResolveSNI(tc.host)
		if sni != tc.expect {
			t.Errorf("ResolveSNI(%s): expected %q, got %q", tc.host, tc.expect, sni)
		}
	}
}

func TestCertBasePath(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfgContent := `{"cert_dir": "cacert"}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	certPath := m.CertBasePath()
	expected := filepath.Join(tmp, "cacert")
	if certPath != expected {
		t.Errorf("CertBasePath: expected %s, got %s", expected, certPath)
	}
}

func TestLoad(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfgContent := `{"proxy": {"listen": "127.0.0.1:8080"}}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if m.Get().Proxy.Listen != "127.0.0.1:8080" {
		t.Errorf("initial listen: expected 8080, got %s", m.Get().Proxy.Listen)
	}

	newContent := `{"proxy": {"listen": "0.0.0.0:9090"}}`
	if err := os.WriteFile(cfgPath, []byte(newContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := m.Load(); err != nil {
		t.Fatalf("reload error: %v", err)
	}

	if m.Get().Proxy.Listen != "0.0.0.0:9090" {
		t.Errorf("after reload listen: expected 9090, got %s", m.Get().Proxy.Listen)
	}
}

func TestDefaults(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "config.json")
	cfgContent := `{}`
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := NewManager(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	cfg := m.Get()
	if cfg.Proxy.Listen != "127.0.0.1:38457" {
		t.Errorf("default proxy listen: expected 127.0.0.1:38457, got %s", cfg.Proxy.Listen)
	}
	if cfg.UI.Listen != "127.0.0.1:38458" {
		t.Errorf("default ui listen: expected 127.0.0.1:38458, got %s", cfg.UI.Listen)
	}
	if cfg.CertDir != "cacert" {
		t.Errorf("default cert_dir: expected cacert, got %s", cfg.CertDir)
	}
	if len(cfg.DNS.Upstreams) != 2 {
		t.Errorf("default dns upstreams: expected 2, got %d", len(cfg.DNS.Upstreams))
	}
	if cfg.SpeedTest.Concurrent != 10 {
		t.Errorf("default speed_test concurrent: expected 10, got %d", cfg.SpeedTest.Concurrent)
	}
}
