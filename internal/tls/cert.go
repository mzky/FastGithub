package tls

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	caCertFile = "fastgithub.cer"
	caKeyFile  = "fastgithub.key"
)

// CertManager 管理自签 CA 与动态证书
type CertManager struct {
	certDir string
	caCert  *x509.Certificate
	caKey   *rsa.PrivateKey
	mu      sync.Mutex
}

// NewCertManager 创建证书管理器
func NewCertManager(certDir string) (*CertManager, error) {
	if err := os.MkdirAll(certDir, 0755); err != nil {
		return nil, fmt.Errorf("create cert dir: %w", err)
	}

	m := &CertManager{certDir: certDir}
	if err := m.loadOrCreateCA(); err != nil {
		return nil, err
	}
	return m, nil
}

// CACertPath 返回 CA 证书路径
func (m *CertManager) CACertPath() string {
	return filepath.Join(m.certDir, caCertFile)
}

// loadOrCreateCA 加载或创建 CA
func (m *CertManager) loadOrCreateCA() error {
	certPath := filepath.Join(m.certDir, caCertFile)
	keyPath := filepath.Join(m.certDir, caKeyFile)

	if _, err := os.Stat(certPath); err == nil {
		certPEM, err := os.ReadFile(certPath)
		if err != nil {
			return err
		}
		keyPEM, err := os.ReadFile(keyPath)
		if err != nil {
			return err
		}

		certBlock, _ := pem.Decode(certPEM)
		if certBlock == nil {
			return fmt.Errorf("invalid ca cert pem")
		}
		caCert, err := x509.ParseCertificate(certBlock.Bytes)
		if err != nil {
			return err
		}

		keyBlock, _ := pem.Decode(keyPEM)
		if keyBlock == nil {
			return fmt.Errorf("invalid ca key pem")
		}
		caKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
		if err != nil {
			return err
		}

		m.caCert = caCert
		m.caKey = caKey
		return nil
	}

	return m.createCA(certPath, keyPath)
}

// createCA 生成新的 CA
func (m *CertManager) createCA(certPath, keyPath string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "FastGithub CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return err
	}

	certFile, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer certFile.Close()
	if err := pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: certDER}); err != nil {
		return err
	}

	keyFile, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyFile.Close()
	if err := pem.Encode(keyFile, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}); err != nil {
		return err
	}

	m.caCert = cert
	m.caKey = key
	return nil
}

// GetCertificate 实现 tls.Config.GetCertificate，为 SNI 动态签发证书
func (m *CertManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if hello.ServerName == "" {
		return nil, fmt.Errorf("no server name in client hello")
	}
	return m.IssueCert(hello.ServerName)
}

// IssueCert 为指定域名签发证书
func (m *CertManager) IssueCert(serverName string) (*tls.Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject: pkix.Name{
			CommonName: serverName,
		},
		DNSNames:              []string{serverName},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, m.caCert, &key.PublicKey, m.caKey)
	if err != nil {
		return nil, err
	}

	cert := tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}
	return &cert, nil
}
