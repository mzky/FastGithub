package speed

import (
	"net"
	"strings"
)

// githubPoolIPs 是 GitHub 官方各段网段中常见的前端 IP。
// 参考 .NET FastGithub 的"GitHub 官方 IP 池"思路：不使用外部 DNS，
// 而是对这批 GitHub 官方 IP 逐个做连通性 + TLS 握手探测，
// 从中选出当前网络真正可达且最快的一个，并定时刷新。
var githubPoolIPs = []string{
	// github.com / api.github.com / gist.github.com / codeload.github.com
	// （140.82.112.0/20 美国段）
	"140.82.112.3", "140.82.112.4", "140.82.112.5", "140.82.112.6",
	"140.82.112.9", "140.82.112.10",
	"140.82.113.3", "140.82.113.4", "140.82.113.5", "140.82.113.6",
	"140.82.114.3", "140.82.114.4", "140.82.114.5", "140.82.114.6",
	"140.82.116.3", "140.82.116.4", "140.82.116.5", "140.82.116.6",
	"140.82.118.3", "140.82.118.4", "140.82.118.5", "140.82.118.6",
	"140.82.121.3", "140.82.121.4", "140.82.121.5", "140.82.121.6",
	// github.com（192.30.255.0/24 旧段）
	"192.30.252.153", "192.30.255.113",
	// github.com（20.20x.0.0 亚洲/Azure 段）
	"20.27.177.113", "20.201.28.151", "20.205.243.166", "20.207.73.82",
	// 静态资源 / raw.githubusercontent.com / githubassets.com / *.githubusercontent.com
	// （Fastly 185.199.108.0/22）
	"185.199.108.133", "185.199.109.133", "185.199.110.133", "185.199.111.133",
	"185.199.108.153", "185.199.109.153", "185.199.110.153", "185.199.111.153",
}

// githubDomains 后台需要预热测量的 GitHub 域名集合。
// 这些域名共用同一份 GitHub 官方前端 IP 池（GitHub 通过 SNI 做虚拟主机路由）。
var githubDomains = []string{
	"github.com",
	"www.github.com",
	"api.github.com",
	"gist.github.com",
	"codeload.github.com",
	"raw.githubusercontent.com",
	"githubusercontent.com",
	"githubassets.com",
	"objects.githubusercontent.com",
	"release-assets.githubusercontent.com",
	"avatars.githubusercontent.com",
	"github.githubassets.com",
}

var (
	parsedPoolIPs = parsePoolIPs()
	poolIPsCache  = parsedPoolIPs
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
	return poolIPsCache
}