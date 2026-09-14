package speed

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"
)

func TestNewTester(t *testing.T) {
	tester := NewTester()
	defer tester.Stop()
	if tester == nil {
		t.Fatal("NewTester returned nil")
	}
}

func TestClearCache(t *testing.T) {
	tester := NewTester()
	defer tester.Stop()

	tester.mu.Lock()
	tester.best["test.com"] = bestEntry{ip: net.ParseIP("1.1.1.1"), updated: time.Now()}
	tester.mu.Unlock()

	tester.ClearCache()

	tester.mu.RLock()
	count := len(tester.best)
	tester.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected empty cache, got %d entries", count)
	}
}

func TestMeasureDomainIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{cert},
	})
	if err != nil {
		t.Skipf("start tls listener: %v", err)
	}
	defer listener.Close()

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) { c.Close() }(conn)
		}
	}()

	addr := listener.Addr().String()
	host, _, _ := net.SplitHostPort(addr)
	ip := net.ParseIP(host)

	tester := NewTester()
	defer tester.Stop()

	best, err := tester.measureDomain("localhost", []net.IP{ip})
	if err != nil {
		t.Fatalf("measureDomain error: %v", err)
	}
	if best == nil {
		t.Error("measureDomain returned nil")
	}
	if !best.Equal(ip) {
		t.Errorf("expected best IP %s, got %s", ip, best)
	}
	t.Logf("best IP: %s", best)
}

// generateSelfSignedCert 生成自签证书用于测试
func generateSelfSignedCert() (tls.Certificate, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  key,
	}, nil
}