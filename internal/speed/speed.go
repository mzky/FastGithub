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
	interval time.Duration        // 后台重测间隔
	timeout  time.Duration        // 单次候选 IP 探测超时
	cooldown time.Duration        // 失败 IP 冷却时长
	stopCh   chan struct{}
	wg       sync.WaitGroup
	sem      chan struct{} // 限制并发连接数，避免瞬时打开过多 socket
}

// Options 测速器可调参数
type Options struct {
	Concurrent int           // 候选 IP 探测并发上限
	Timeout    time.Duration // 单次候选 IP 探测超时
	Interval   time.Duration // 后台重测间隔
	CoolDown   time.Duration // 失败 IP 冷却时长
}

// defaultOptions 返回合理的默认参数（对应 config.json 未配置时的兜底值）
func defaultOptions() Options {
	return Options{
		Concurrent: 16,
		Timeout:    2 * time.Second,
		Interval:   5 * time.Minute,
		CoolDown:   30 * time.Second,
	}
}

// normalize 填充默认值
func (o *Options) normalize() {
	def := defaultOptions()
	if o.Concurrent <= 0 {
		o.Concurrent = def.Concurrent
	}
	if o.Timeout <= 0 {
		o.Timeout = def.Timeout
	}
	if o.Interval <= 0 {
		o.Interval = def.Interval
	}
	if o.CoolDown <= 0 {
		o.CoolDown = def.CoolDown
	}
}

// bestEntry 某个域名当前的可达 IP 列表（按延迟升序），以及失败 IP 冷却表
type bestEntry struct {
	ips     []net.IP
	updated time.Time
	failed  map[string]time.Time // IP.String() -> 冷却截止时间
}

type speedResult struct {
	ip  net.IP
	rtt time.Duration
	err error
}

// NewTester 创建测速器并启动后台测量。
// opt 可留空（零值），会自动填默认值。具体行为见 Options。
// 启动时先加载上次保存的官方 IP 候选（本地文件，即时返回），
// 再异步拉取 GitHub 官方公布的全部 IP 段（不阻塞启动，避免受限网络下卡住）。
func NewTester(opt Options) *Tester {
	opt.normalize()
	t := &Tester{
		best:     make(map[string]bestEntry),
		interval: opt.Interval,
		timeout:  opt.Timeout,
		cooldown: opt.CoolDown,
		stopCh:   make(chan struct{}),
		sem:      make(chan struct{}, opt.Concurrent),
	}
	loadSavedPool() // 复用上次保存的官方 IP 候选，避免依赖网络
	go t.fetchMetaAsync()
	t.wg.Add(1)
	go t.measureLoop()
	return t
}

// fetchMetaAsync 后台直连拉取官方 IP 段，合并进候选池并落盘，随后重新测量。
// 直连拉不到也不影响启动，稍后可由 WarmProxyPool 走代理兜底。
func (t *Tester) fetchMetaAsync() {
	if ips, err := FetchGitHubMetaIPs(); err == nil && len(ips) > 0 {
		AddPoolIPs(ips)
		_ = SavePool() // 直连拉取成功也落盘，供下次复用
		t.ClearCache() // 清空旧缓存，触发用扩充后的候选池重新测量
		t.measureAll()
	}
}

// WarmProxyPool 通过本地代理拉取官方 IP 段并扩充候选池，随后重新测量。
// 用于直连 api.github.com/meta 不通、但代理隧道已能到达 GitHub 的场景：
// 先把该接口通过代理打通拿到最新 IP 段，合并进候选池并保存，再刷新各域名最优 IP。
func (t *Tester) WarmProxyPool(proxyAddr string) {
	ips, err := FetchGitHubMetaIPsViaProxy(proxyAddr)
	if err != nil || len(ips) == 0 {
		return
	}
	AddPoolIPs(ips)
	_ = SavePool()
	t.ClearCache() // 清空旧的最优 IP 缓存，触发重新测量
	t.measureAll()
}

// BestIP 返回域名当前可用的最优 IP（失败 IP 处于冷却期时自动跳过）
func (t *Tester) BestIP(domain string) (net.IP, error) {
	domain = normalizeDomain(domain)

	t.mu.RLock()
	e, ok := t.best[domain]
	t.mu.RUnlock()

	// 缓存缺失或过期时，即时测量一次并缓存完整 IP 列表
	if !ok || time.Since(e.updated) >= t.interval {
		ips, err := t.measureDomain(domain, poolIPs())
		if err != nil {
			return nil, err
		}
		e = bestEntry{ips: ips, updated: time.Now(), failed: make(map[string]time.Time)}
		t.mu.Lock()
		t.best[domain] = e
		t.mu.Unlock()
	}

	now := time.Now()
	for _, ip := range e.ips {
		if coolUntil, bad := e.failed[ip.String()]; !bad || now.After(coolUntil) {
			return cloneIP(ip), nil
		}
	}

	// 全部 IP 都在冷却中，清空冷却后返回最快的那个
	e.failed = make(map[string]time.Time)
	t.mu.Lock()
	t.best[domain] = e
	t.mu.Unlock()
	return cloneIP(e.ips[0]), nil
}

// MarkIPFailed 将指定域名的某个 IP 标记为失败并进入冷却期
func (t *Tester) MarkIPFailed(domain string, ip net.IP) {
	domain = normalizeDomain(domain)
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.best[domain]
	if !ok {
		return
	}
	if e.failed == nil {
		e.failed = make(map[string]time.Time)
	}
	e.failed[ip.String()] = time.Now().Add(t.cooldown)
	t.best[domain] = e
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

// measureAll 按域族群分别探测官方 IP 池，并各自维护最优 IP 列表。
// GitHub 前端按 SNI 虚拟主机路由：同一 IP 可能只服务某一族群域名，
// 因此对每个族群用其代表 SNI 分别探测、过滤，只保留真正能服务的 IP，
// 避免把静态资源（raw/avatar）错误路由到核心边缘触发 "invalid request"。
func (t *Tester) measureAll() {
	type groupResult struct {
		g      domainGroup
		ranked []net.IP
	}
	var results []groupResult
	for _, g := range domainGroups {
		ranked, err := t.measureDomain(g.probeSNI, poolIPs())
		if err != nil || len(ranked) == 0 {
			continue
		}
		results = append(results, groupResult{g, ranked})
	}
	if len(results) == 0 {
		return
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range results {
		for _, d := range r.g.domains {
			t.best[d] = bestEntry{ips: r.ranked, updated: now, failed: make(map[string]time.Time)}
		}
	}
}

// Stop 停止后台测量
func (t *Tester) Stop() {
	close(t.stopCh)
	t.wg.Wait()
}

// measureDomain 并发探测候选 IP，返回全部连通且按延迟升序排序的 IP 列表
func (t *Tester) measureDomain(domain string, ips []net.IP) ([]net.IP, error) {
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

	out := make([]net.IP, 0, len(valid))
	for _, r := range valid {
		out = append(out, cloneIP(r.ip))
	}
	return out, nil
}

// probe 探测单个 IP 的连接 + TLS 握手延迟
func (t *Tester) probe(domain string, ip net.IP) (time.Duration, error) {
	// 通过信号量限制并发连接数
	t.sem <- struct{}{}
	defer func() { <-t.sem }()

	addr := net.JoinHostPort(ip.String(), "443")
	start := time.Now()

	conn, err := net.DialTimeout("tcp", addr, t.timeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         domain,
		InsecureSkipVerify: true,
	})
	if err := tlsConn.SetDeadline(time.Now().Add(t.timeout + 3*time.Second)); err != nil {
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