package speed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// githubPoolIPs 是 GitHub 官方各段网段中的前端 IP（覆盖全部 GitHub 官方 IP 段）。
// 参考 .NET FastGithub 的"GitHub 官方 IP 池"思路：不使用外部 DNS，
// 而是对这批 GitHub 官方 IP 逐个做连通性 + TLS 握手探测，
// 从中选出当前网络真正可达且最快的一个，并定时刷新。
var githubPoolIPs = []string{
	// github.com / api.github.com / gist.github.com / codeload.github.com
	// （140.82.112.0/20 美国段）
	"140.82.112.3", "140.82.112.4", "140.82.112.5", "140.82.112.6",
	"140.82.113.3", "140.82.113.4", "140.82.113.5", "140.82.113.6",
	"140.82.114.3", "140.82.114.4", "140.82.114.5", "140.82.114.6",
	"140.82.115.3", "140.82.115.4", "140.82.115.5", "140.82.115.6",
	"140.82.116.3", "140.82.116.4", "140.82.116.5", "140.82.116.6",
	"140.82.117.3", "140.82.117.4", "140.82.117.5", "140.82.117.6",
	"140.82.118.3", "140.82.118.4", "140.82.118.5", "140.82.118.6",
	"140.82.119.3", "140.82.119.4", "140.82.119.5", "140.82.119.6",
	"140.82.120.3", "140.82.120.4", "140.82.120.5", "140.82.120.6",
	"140.82.121.3", "140.82.121.4", "140.82.121.5", "140.82.121.6",
	// github.com（143.55.64.0/20 旧段）
	"143.55.64.3", "143.55.64.4", "143.55.65.3", "143.55.65.4",
	"143.55.66.3", "143.55.66.4", "143.55.67.3", "143.55.67.4",
	// github.com（192.30.252.0/22 与 192.30.253.0/24 旧段）
	"192.30.252.153", "192.30.252.154",
	"192.30.253.112", "192.30.253.113", "192.30.253.114",
	"192.30.255.112", "192.30.255.113", "192.30.255.116",
	// github.com（20.27 / 20.200 / 20.201 / 20.205 / 20.207 亚洲/Azure 段）
	"20.27.177.113", "20.200.245.247", "20.201.28.151",
	"20.205.243.166", "20.207.73.82", "20.201.28.152",
	// 静态资源 / raw.githubusercontent.com / githubassets.com / *.githubusercontent.com
	// （Fastly 185.199.108.0/22，覆盖 avatars / objects / release-assets 等）
	"185.199.108.133", "185.199.108.154", "185.199.108.153",
	"185.199.109.133", "185.199.109.154", "185.199.109.153",
	"185.199.110.133", "185.199.110.154", "185.199.110.153",
	"185.199.111.133", "185.199.111.154", "185.199.111.153",
	// GitHub 亚洲/DNS 段（202.5.35.x）
	"202.5.35.29", "202.5.35.167",
}

// domainGroup 域族群：一组域名共用同一个代表性 SNI 来探测并维护各自的最优 IP 列表。
// GitHub 前端按 SNI 虚拟主机路由：核心网页走 GitHub 自身边缘（140.82.x 等），
// 静态资源（raw/avatar/objects/release-assets）走 Fastly 等 CDN 边缘（185.199.x 等）。
// 若所有域名共用同一份最优 IP，会把静态域名错误路由到核心边缘，触发 "invalid request"。
type domainGroup struct {
	probeSNI string   // 探测所用 SNI（虚拟主机），决定该组 IP 归属
	domains  []string // 该组域名
}

// domainGroups 后台需要预热测量的 GitHub 域名分组。
var domainGroups = []domainGroup{
	{probeSNI: "github.com", domains: []string{
		"github.com",
		"www.github.com",
		"api.github.com",
		"gist.github.com",
		"codeload.github.com",
	}},
	{probeSNI: "raw.githubusercontent.com", domains: []string{
		"raw.githubusercontent.com",
		"githubusercontent.com",
		"objects.githubusercontent.com",
		"release-assets.githubusercontent.com",
		"avatars.githubusercontent.com",
	}},
	{probeSNI: "github.githubassets.com", domains: []string{
		"github.githubassets.com",
		"githubassets.com",
	}},
}

var (
	poolMu       sync.Mutex
	poolIPsCache = parsePoolIPs() // 初始为内置池，可由 FetchGitHubMetaIPs 增补
)

// parsePoolIPs 解析并去重候选池 IP
func parsePoolIPs() []net.IP {
	out := make([]net.IP, 0, len(githubPoolIPs))
	seen := make(map[string]struct{})
	for _, s := range githubPoolIPs {
		ip := net.ParseIP(strings.TrimSpace(s))
		if ip == nil {
			continue
		}
		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ip)
	}
	return out
}

// poolIPs 返回候选池 IP 列表（内部使用）
func poolIPs() []net.IP {
	poolMu.Lock()
	defer poolMu.Unlock()
	return poolIPsCache
}

// AddPoolIPs 将额外的 IP 去重合并进候选池（供启动时动态获取的官方 IP 段使用）
func AddPoolIPs(ips []net.IP) {
	poolMu.Lock()
	defer poolMu.Unlock()
	seen := make(map[string]struct{}, len(poolIPsCache))
	for _, ip := range poolIPsCache {
		seen[ip.String()] = struct{}{}
	}
	for _, ip := range ips {
		key := ip.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		poolIPsCache = append(poolIPsCache, ip)
	}
}

// metaEndpoint GitHub 官方 IP 段公布接口
const metaEndpoint = "https://api.github.com/meta"

// savedPoolFile 保存官方 IP 候选的文件名（存于 exe 同目录，供下次启动复用）
const savedPoolFile = "github_ips.json"

// metaResponse 解析 api.github.com/meta 的 IPv4 地址段。
// web/api/git/pages 是面向前端的可达地址，actions 等主要供 CI 使用，作为补充。
type metaResponse struct {
	Web     []string `json:"web"`
	API     []string `json:"api"`
	Git     []string `json:"git"`
	Pages   []string `json:"pages"`
	Actions []string `json:"actions"`
}

// FetchGitHubMetaIPs 直连拉取 GitHub 官方公布的全部前端 IP 段（尽力而为，直连不通时返回 error）。
func FetchGitHubMetaIPs() ([]net.IP, error) {
	return fetchMeta(http.DefaultClient)
}

// FetchGitHubMetaIPsViaProxy 通过本地代理（proxyAddr 如 127.0.0.1:38457）拉取官方 IP 段。
// 用于直连不通、但代理隧道已能到达 GitHub 的场景。
func FetchGitHubMetaIPsViaProxy(proxyAddr string) ([]net.IP, error) {
	u, err := url.Parse("http://" + proxyAddr)
	if err != nil {
		return nil, err
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			Proxy:          http.ProxyURL(u),
			ProxyConnectHeader: http.Header{},
		},
	}
	return fetchMeta(client)
}

func fetchMeta(client *http.Client) ([]net.IP, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaEndpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "FastGithub")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("meta http status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return parseMeta(body)
}

// parseMeta 解析 meta 响应并按需展开为代表性候选 IP
func parseMeta(body []byte) ([]net.IP, error) {
	var meta metaResponse
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, err
	}

	all := append([]string{}, meta.Web...)
	all = append(all, meta.API...)
	all = append(all, meta.Git...)
	all = append(all, meta.Pages...)

	seen := make(map[string]struct{})
	var out []net.IP
	for _, cidr := range all {
		for _, ip := range expandCIDR(cidr) {
			key := ip.String()
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, ip)
			if len(out) >= 96 { // 控制探测规模
				return out, nil
			}
		}
	}
	return out, nil
}

// loadSavedPool 从磁盘加载上次保存的官方 IP 候选并合并进池（下次启动无需网络即复用）
func loadSavedPool() {
	data, err := os.ReadFile(savedPoolPath())
	if err != nil {
		return
	}
	var list []string
	if json.Unmarshal(data, &list) != nil {
		return
	}
	var ips []net.IP
	for _, s := range list {
		if ip := net.ParseIP(strings.TrimSpace(s)); ip != nil {
			ips = append(ips, ip)
		}
	}
	if len(ips) > 0 {
		AddPoolIPs(ips)
	}
}

// SavePool 将当前候选池写盘，供下次启动复用
func SavePool() error {
	poolMu.Lock()
	list := make([]string, 0, len(poolIPsCache))
	for _, ip := range poolIPsCache {
		list = append(list, ip.String())
	}
	poolMu.Unlock()

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(savedPoolPath(), data, 0644)
}

func savedPoolPath() string {
	var dir string
	if exe, err := os.Executable(); err == nil {
		dir = filepath.Dir(exe)
	} else if wd, err := os.Getwd(); err == nil {
		dir = wd
	} else {
		dir = "."
	}
	return filepath.Join(dir, savedPoolFile)
}

// expandCIDR 将 CIDR 展开为若干个代表性 IP。宽网段（host 位 > 8）取 host 位 3、4、5；
// 窄网段直接枚举网段内可用地址。避免对 /16 级网段做全量枚举。
func expandCIDR(cidr string) []net.IP {
	ip, ipnet, err := net.ParseCIDR(strings.TrimSpace(cidr))
	if err != nil {
		return nil
	}
	ones, bits := ipnet.Mask.Size()
	hostBits := bits - ones
	if hostBits > 8 { // 网段过宽，只取少量代表 IP，避免候选爆炸
		var out []net.IP
		for _, host := range []int{3, 4, 5} {
			out = append(out, addOffset(ip, host))
		}
		return out
	}
	var out []net.IP
	for _, host := range []int{0, 1, 2, 3} {
		if host < (1 << hostBits) {
			out = append(out, addOffset(ip, host))
		}
	}
	return out
}

// addOffset 返回 ip 加上 offset 后的地址
func addOffset(ip net.IP, offset int) net.IP {
	v4 := ip.To4()
	if v4 == nil {
		return ip
	}
	out := make(net.IP, len(v4))
	copy(out, v4)
	out[3] += byte(offset)
	return out
}