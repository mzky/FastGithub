package speed

import (
	"crypto/tls"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/creazyboyone/fastgithub/internal/dns"
)

// Tester IP 测速器
type Tester struct {
	resolver *dns.Resolver
	mu       sync.RWMutex
	cache    map[string]*cacheItem
}

type cacheItem struct {
	ip      net.IP
	expires time.Time
}

type speedResult struct {
	ip   net.IP
	rtt  time.Duration
	err  error
}

// NewTester 创建测速器
func NewTester(resolver *dns.Resolver) *Tester {
	return &Tester{
		resolver: resolver,
		cache:    make(map[string]*cacheItem),
	}
}

// BestIP 返回域名对应的最优 IP
func (t *Tester) BestIP(domain string) (net.IP, error) {
	domain = normalizeDomain(domain)

	t.mu.RLock()
	if item, ok := t.cache[domain]; ok && time.Now().Before(item.expires) {
		ip := make(net.IP, len(item.ip))
		copy(ip, item.ip)
		t.mu.RUnlock()
		return ip, nil
	}
	t.mu.RUnlock()

	ips, err := t.resolver.Resolve(domain)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", domain, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no ip for %s", domain)
	}

	best, err := t.measureBest(domain, ips)
	if err != nil {
		return nil, err
	}

	t.mu.Lock()
	t.cache[domain] = &cacheItem{ip: best, expires: time.Now().Add(5 * time.Minute)}
	t.mu.Unlock()

	return best, nil
}

// measureBest 对候选 IP 测速并返回最快的一个
func (t *Tester) measureBest(domain string, ips []net.IP) (net.IP, error) {
	results := make(chan speedResult, len(ips))
	var wg sync.WaitGroup

	for _, ip := range ips {
		wg.Add(1)
		go func(addr net.IP) {
			defer wg.Done()
			rtt, err := t.probe(domain, addr)
			results <- speedResult{ip: addr, rtt: rtt, err: err}
		}(ip)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var valid []speedResult
	for r := range results {
		if r.err == nil {
			valid = append(valid, r)
		}
	}

	if len(valid) == 0 {
		return nil, fmt.Errorf("all ip probes failed for %s", domain)
	}

	sort.Slice(valid, func(i, j int) bool {
		return valid[i].rtt < valid[j].rtt
	})

	return valid[0].ip, nil
}

// probe 探测单个 IP 的连接+握手延迟
func (t *Tester) probe(domain string, ip net.IP) (time.Duration, error) {
	addr := net.JoinHostPort(ip.String(), "443")
	start := time.Now()

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true,
	})
	if err := tlsConn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	if err := tlsConn.Handshake(); err != nil {
		return 0, err
	}
	_ = tlsConn.Close()

	return time.Since(start), nil
}

// ClearCache 清空缓存
func (t *Tester) ClearCache() {
	t.mu.Lock()
	t.cache = make(map[string]*cacheItem)
	t.mu.Unlock()
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}
