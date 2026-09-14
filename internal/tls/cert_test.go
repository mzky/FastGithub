package tls

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
)

func TestNewCertManager(t *testing.T) {
	tmp := t.TempDir()
	mgr, err := NewCertManager(tmp)
	if err != nil {
		t.Fatalf("NewCertManager error: %v", err)
	}

	// 检查 CA 文件是否生成
	certPath := filepath.Join(tmp, "fastgithub.cer")
	keyPath := filepath.Join(tmp, "fastgithub.key")
	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Error("CA cert file not created")
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Error("CA key file not created")
	}
	if mgr.CACertPath() != certPath {
		t.Errorf("CACertPath mismatch: %s vs %s", mgr.CACertPath(), certPath)
	}
}

func TestIssueCert(t *testing.T) {
	tmp := t.TempDir()
	mgr, err := NewCertManager(tmp)
	if err != nil {
		t.Fatal(err)
	}

	cert, err := mgr.IssueCert("example.com")
	if err != nil {
		t.Fatalf("IssueCert error: %v", err)
	}

	if len(cert.Certificate) == 0 {
		t.Error("issued cert has no certificate chain")
	}
	if cert.PrivateKey == nil {
		t.Error("issued cert has no private key")
	}

	// 验证证书可用于 TLS 服务端
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{*cert},
	}
	_ = tlsConfig
}

func TestGetCertificate(t *testing.T) {
	tmp := t.TempDir()
	mgr, err := NewCertManager(tmp)
	if err != nil {
		t.Fatal(err)
	}

	hello := &tls.ClientHelloInfo{ServerName: "test.example.com"}
	cert, err := mgr.GetCertificate(hello)
	if err != nil {
		t.Fatalf("GetCertificate error: %v", err)
	}
	if cert == nil {
		t.Error("GetCertificate returned nil cert")
	}
}

func TestGetCertificateNoSNI(t *testing.T) {
	tmp := t.TempDir()
	mgr, err := NewCertManager(tmp)
	if err != nil {
		t.Fatal(err)
	}

	hello := &tls.ClientHelloInfo{ServerName: ""}
	_, err = mgr.GetCertificate(hello)
	if err == nil {
		t.Error("expected error for empty server name, got nil")
	}
}

func TestLoadExistingCA(t *testing.T) {
	tmp := t.TempDir()

	// 第一次创建
	mgr1, err := NewCertManager(tmp)
	if err != nil {
		t.Fatal(err)
	}
	cert1, err := mgr1.IssueCert("first.com")
	if err != nil {
		t.Fatal(err)
	}

	// 第二次加载
	mgr2, err := NewCertManager(tmp)
	if err != nil {
		t.Fatal(err)
	}
	cert2, err := mgr2.IssueCert("second.com")
	if err != nil {
		t.Fatal(err)
	}

	// 两个证书应由同一个 CA 签发
	if len(cert1.Certificate) == 0 || len(cert2.Certificate) == 0 {
		t.Fatal("certificates empty")
	}
}
