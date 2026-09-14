package dns

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Resolver 自定义 DNS 解析器
// 支持多种解析方式：UDP、TCP、DoT（DNS over TLS）、DoH（DNS over HTTPS）、系统 DNS
type Resolver struct {
	upstreams []Upstream
	mu        sync.RWMutex
	cache     map[string]*cacheItem
}

// Upstream DNS 上游配置
type Upstream struct {
	Addr   string // 地址，如 8.8.8.8:53, 1.1.1.1:853, https://dns.google/dns-query
	Mode   string // udp, tcp, dot, doh, system
	SNI    string // DoT 的 SNI
	Weight int    // 权重（预留）
}

type cacheItem struct {
	ips     []net.IP
	expires time.Time
}

// NewResolver 创建解析器（从简单地址列表创建，默认 UDP 模式）
func NewResolver(addrs []string) *Resolver {
	ups := make([]Upstream, 0, len(addrs))
	for _, addr := range addrs {
		ups = append(ups, Upstream{Addr: addr, Mode: "udp"})
	}
	return &Resolver{
		upstreams: ups,
		cache:     make(map[string]*cacheItem),
	}
}

// NewResolverWithUpstreams 创建解析器（完整配置）
func NewResolverWithUpstreams(ups []Upstream) *Resolver {
	return &Resolver{
		upstreams: ups,
		cache:     make(map[string]*cacheItem),
	}
}

// SetUpstreams 动态设置上游
func (r *Resolver) SetUpstreams(addrs []string) {
	ups := make([]Upstream, 0, len(addrs))
	for _, addr := range addrs {
		ups = append(ups, Upstream{Addr: addr, Mode: "udp"})
	}
	r.mu.Lock()
	r.upstreams = ups
	r.mu.Unlock()
}

// Resolve 解析域名，返回候选 IP 列表
func (r *Resolver) Resolve(domain string) ([]net.IP, error) {
	domain = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))

	r.mu.RLock()
	if item, ok := r.cache[domain]; ok && time.Now().Before(item.expires) {
		ips := make([]net.IP, len(item.ips))
		copy(ips, item.ips)
		r.mu.RUnlock()
		return ips, nil
	}
	r.mu.RUnlock()

	ips, ttl, err := r.query(domain)
	if err != nil {
		// 上游 DNS 全部失败时，尝试系统 DNS 作为最后兜底
		sysIPs, sysErr := net.DefaultResolver.LookupIP(context.Background(), "ip4", domain)
		if sysErr == nil && len(sysIPs) > 0 {
			r.mu.Lock()
			r.cache[domain] = &cacheItem{ips: sysIPs, expires: time.Now().Add(60 * time.Second)}
			r.mu.Unlock()
			return sysIPs, nil
		}
		return nil, err
	}

	if ttl < 60 {
		ttl = 60
	}
	if ttl > 600 {
		ttl = 600
	}

	r.mu.Lock()
	r.cache[domain] = &cacheItem{ips: ips, expires: time.Now().Add(time.Duration(ttl) * time.Second)}
	r.mu.Unlock()

	return ips, nil
}

// query 向所有上游并发查询 A 记录
func (r *Resolver) query(domain string) ([]net.IP, uint32, error) {
	r.mu.RLock()
	upstreams := make([]Upstream, len(r.upstreams))
	copy(upstreams, r.upstreams)
	r.mu.RUnlock()

	if len(upstreams) == 0 {
		return nil, 0, fmt.Errorf("no dns upstream configured")
	}

	type result struct {
		ips []net.IP
		ttl uint32
		err error
	}

	results := make(chan result, len(upstreams))
	var wg sync.WaitGroup

	for _, up := range upstreams {
		wg.Add(1)
		go func(u Upstream) {
			defer wg.Done()
			ips, ttl, err := r.queryUpstream(u, domain)
			results <- result{ips: ips, ttl: ttl, err: err}
		}(up)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var minTTL uint32 = 600
	ipSet := make(map[string]net.IP)
	var errCount int
	for res := range results {
		if res.err != nil {
			errCount++
			continue
		}
		if res.ttl > 0 && res.ttl < minTTL {
			minTTL = res.ttl
		}
		for _, ip := range res.ips {
			ipSet[ip.String()] = ip
		}
	}

	if len(ipSet) == 0 {
		return nil, 0, fmt.Errorf("all dns upstreams failed (%d errors)", errCount)
	}

	ips := make([]net.IP, 0, len(ipSet))
	for _, ip := range ipSet {
		ips = append(ips, ip)
	}
	return ips, minTTL, nil
}

// queryUpstream 向单个上游查询
func (r *Resolver) queryUpstream(up Upstream, domain string) ([]net.IP, uint32, error) {
	switch up.Mode {
	case "udp", "tcp":
		return r.queryMiekg(up, domain)
	case "dot":
		return r.queryDoT(up, domain)
	case "doh":
		return r.queryDoH(up, domain)
	case "system":
		return r.querySystem(domain)
	default:
		return r.queryMiekg(up, domain)
	}
}

// queryMiekg 使用 miekg/dns 查询（UDP 或 TCP）
func (r *Resolver) queryMiekg(up Upstream, domain string) ([]net.IP, uint32, error) {
	c := new(dns.Client)
	c.Net = up.Mode
	c.Timeout = 5 * time.Second

	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	msg.RecursionDesired = true

	rr, _, err := c.Exchange(msg, up.Addr)
	if err != nil {
		return nil, 0, err
	}
	if rr.Rcode != dns.RcodeSuccess {
		return nil, 0, fmt.Errorf("dns rcode %d", rr.Rcode)
	}

	return extractIPs(rr)
}

// queryDoT 使用 DNS over TLS 查询
func (r *Resolver) queryDoT(up Upstream, domain string) ([]net.IP, uint32, error) {
	c := new(dns.Client)
	c.Net = "tcp-tls"
	c.Timeout = 5 * time.Second
	if up.SNI != "" {
		c.TLSConfig = &tls.Config{ServerName: up.SNI}
	}

	msg := new(dns.Msg)
	msg.SetQuestion(dns.Fqdn(domain), dns.TypeA)
	msg.RecursionDesired = true

	rr, _, err := c.Exchange(msg, up.Addr)
	if err != nil {
		return nil, 0, err
	}
	if rr.Rcode != dns.RcodeSuccess {
		return nil, 0, fmt.Errorf("dns rcode %d", rr.Rcode)
	}

	return extractIPs(rr)
}

// dohResponse DoH JSON 响应
type dohResponse struct {
	Status int  `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
		TTL  int    `json:"TTL"`
	} `json:"Answer"`
}

// queryDoH 使用 DNS over HTTPS 查询
func (r *Resolver) queryDoH(up Upstream, domain string) ([]net.IP, uint32, error) {
	url := up.Addr
	if !strings.Contains(url, "?") {
		url = url + "?name=" + domain + "&type=A"
	} else {
		url = url + "&name=" + domain + "&type=A"
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/dns-json")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}

	var result dohResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, 0, fmt.Errorf("parse doh response: %w", err)
	}

	if result.Status != 0 {
		return nil, 0, fmt.Errorf("doh status %d", result.Status)
	}

	var ips []net.IP
	var minTTL uint32 = 600
	for _, a := range result.Answer {
		if a.Type == 1 { // Type A
			ip := net.ParseIP(a.Data)
			if ip != nil {
				ips = append(ips, ip)
			}
			if a.TTL > 0 && uint32(a.TTL) < minTTL {
				minTTL = uint32(a.TTL)
			}
		}
	}

	if len(ips) == 0 {
		return nil, 0, fmt.Errorf("no A records in DoH response")
	}
	return ips, minTTL, nil
}

// querySystem 使用系统 DNS 查询
func (r *Resolver) querySystem(domain string) ([]net.IP, uint32, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", domain)
	if err != nil {
		return nil, 0, err
	}
	if len(ips) == 0 {
		return nil, 0, fmt.Errorf("no records")
	}
	return ips, 60, nil // 系统 DNS 没有 TTL，默认 60 秒
}

// extractIPs 从 DNS 响应中提取 IP 和 TTL
func extractIPs(rr *dns.Msg) ([]net.IP, uint32, error) {
	var ips []net.IP
	var ttl uint32
	for _, ans := range rr.Answer {
		switch v := ans.(type) {
		case *dns.A:
			ips = append(ips, v.A)
			if ttl == 0 || v.Hdr.Ttl < ttl {
				ttl = v.Hdr.Ttl
			}
		case *dns.AAAA:
			ips = append(ips, v.AAAA)
			if ttl == 0 || v.Hdr.Ttl < ttl {
				ttl = v.Hdr.Ttl
			}
		}
	}

	if len(ips) == 0 {
		return nil, 0, fmt.Errorf("no records")
	}
	return ips, ttl, nil
}

// ClearCache 清空缓存
func (r *Resolver) ClearCache() {
	r.mu.Lock()
	r.cache = make(map[string]*cacheItem)
	r.mu.Unlock()
}

// 保活 bytes 引用
var _ = bytes.NewReader
