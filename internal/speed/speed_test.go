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

	"github.com/creazyboyone/fastgithub/internal/dns"
)

func TestNewTester(t *testing.T) {
	resolver := dns.NewResolver([]string{"8.8.8.8:53"})
	tester := NewTester(resolver)
	if tester == nil {
		t.Fatal("NewTester returned nil")
	}
}

func TestClearCache(t *testing.T) {
	resolver := dns.NewResolver([]string{"8.8.8.8:53"})
	tester := NewTester(resolver)

	tester.mu.Lock()
	tester.cache["test.com"] = &cacheItem{}
	tester.mu.Unlock()

	tester.ClearCache()

	tester.mu.RLock()
	count := len(tester.cache)
	tester.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected empty cache, got %d entries", count)
	}
}

func TestMeasureBestIntegration(t *testing.T) {
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
	host, port, _ := net.SplitHostPort(addr)
	ip := net.ParseIP(host)

	resolver := dns.NewResolver([]string{"8.8.8.8:53"})
	tester := NewTester(resolver)

	best, err := tester.measureBest("localhost", []net.IP{ip})
	if err != nil {
		t.Fatalf("measureBest error: %v", err)
	}
	if best == nil {
		t.Error("measureBest returned nil")
	}
	if !best.Equal(ip) {
		t.Errorf("expected best IP %s, got %s", ip, best)
	}
	t.Logf("best IP: %s, port: %s", best, port)
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

func TestProbeIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	resolver := dns.NewResolver([]string{"8.8.8.8:53"})
	tester := NewTester(resolver)

	ips, err := resolver.Resolve("example.com")
	if err != nil {
		t.Skipf("resolve example.com failed: %v", err)
	}
	if len(ips) == 0 {
		t.Skip("no IPs for example.com")
	}

	rtt, err := tester.probe("example.com", ips[0])
	if err != nil {
		t.Logf("probe error (may be network issue): %v", err)
		return
	}
	if rtt <= 0 {
		t.Error("probe returned zero or negative RTT")
	}
	if rtt > 30*time.Second {
		t.Errorf("probe RTT too long: %v", rtt)
	}
	t.Logf("probe RTT: %v", rtt)
}
