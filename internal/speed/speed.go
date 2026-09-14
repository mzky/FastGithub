package speed

import (
	"crypto/tls"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
)

// Tester GitHub IP 池测速器
// 参考 .NET FastGithub：不使用外部 DNS，而是对 GitHub 官方 IP 池进行后台持续探测，
// 为 GitHub 相关域名维护"当前网络可达且最快"的 IP，并定时刷新。
type Tester struct {
	mu       sync.RWMutex
	best     map[string]bestEntry // domain -> 最优 IP
	interval time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
	sem      chan struct{} // 限制并发连接数，避免瞬时打开过多 socket
}

// bestEntry 某个域名当前的最优 IP
type bestEntry struct {
	ip      net.IP
	updated time.Time
}

// 默认后台测量间隔与并发上限
const (
	defaultMeasureInterval = 5 * time.Minute
	maxProbeConcurrent     = 64
)

type speedResult struct {
	ip  net.IP
	rtt time.Duration
	err error
}

// NewTester 创建测速器并启动后台测量
func NewTester() *Tester {
	t := &Tester{
		best:     make(map[string]bestEntry),
		interval: defaultMeasureInterval,
		stopCh:   make(chan struct{}),
		sem:      make(chan struct{}, maxProbeConcurrent),
	}
	t.wg.Add(1)
	go t.measureLoop()
	return t
}

// BestIP 返回域名当前最优的可用 IP（优先使用后台测量的缓存结果）
func (t *Tester) BestIP(domain string) (net.IP, error) {
	domain = normalizeDomain(domain)

	t.mu.RLock()
	e, ok := t.best[domain]
	t.mu.RUnlock()
	if ok && time.Since(e.updated) < t.interval {
		return cloneIP(e.ip), nil
	}

	// 缓存缺失或过期时，即时测量一次
	best, err := t.measureDomain(domain, poolIPs())
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	t.best[domain] = bestEntry{ip: best, updated: time.Now()}
	t.mu.Unlock()
	return best, nil
}

// measureLoop 后台定期测量整个 GitHub IP 池
func (t *Tester) measureLoop() {
	defer t.wg.Done()

	// 启动时先预热（异步，不阻塞消息循环与 Stop）
	go t.measureAll()

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			go t.measureAll()
		}
	}
}

// measureAll 对已知的 GitHub 域名并发测量并缓存最优 IP
func (t *Tester) measureAll() {
	var wg sync.WaitGroup
	for _, d := range githubDomains {
		wg.Add(1)
		go func(domain string) {
			defer wg.Done()
			if best, err := t.measureDomain(domain, poolIPs()); err == nil {
				t.mu.Lock()
				t.best[domain] = bestEntry{ip: best, updated: time.Now()}
				t.mu.Unlock()
			}
		}(d)
	}
	wg.Wait()
}

// Stop 停止后台测量
func (t *Tester) Stop() {
	close(t.stopCh)
	t.wg.Wait()
}

// measureDomain 并发探测候选 IP，返回连通且最快的一个
func (t *Tester) measureDomain(domain string, ips []net.IP) (net.IP, error) {
	if len(ips) == 0 {
		return nil, fmt.Errorf("no candidate ip for %s", domain)
	}

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

	return cloneIP(valid[0].ip), nil
}

// probe 探测单个 IP 的连接 + TLS 握手延迟
func (t *Tester) probe(domain string, ip net.IP) (time.Duration, error) {
	// 通过信号量限制并发连接数
	t.sem <- struct{}{}
	defer func() { <-t.sem }()

	addr := net.JoinHostPort(ip.String(), "443")
	start := time.Now()

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
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

// ClearCache 清空已测量的 IP 缓存
func (t *Tester) ClearCache() {
	t.mu.Lock()
	t.best = make(map[string]bestEntry)
	t.mu.Unlock()
}

func cloneIP(ip net.IP) net.IP {
	c := make(net.IP, len(ip))
	copy(c, ip)
	return c
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}