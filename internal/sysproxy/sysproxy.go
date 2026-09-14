// Package sysproxy 提供系统代理（PAC 自动配置）的开启与关闭控制。
// 启动程序时自动开启系统代理（仅代理配置的 GitHub 相关域名），
// 关闭或异常退出时自动关闭（恢复原状）。
package sysproxy

import "strings"

// BuildPAC 生成 PAC 自动配置脚本内容。
// 只有主机名匹配 patterns 中的任一模式时才走 proxyAddr 代理，其余请求直连（DIRECT）。
func BuildPAC(proxyAddr string, patterns []string) string {
	var conds []string
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		conds = append(conds, `shExpMatch(host, "`+p+`")`)
	}

	var rule string
	if len(conds) > 0 {
		rule = `    if (` + strings.Join(conds, " ||\n        ") + `) {
        return "PROXY ` + proxyAddr + `";
    }
    return "DIRECT";`
	} else {
		rule = `    return "DIRECT";`
	}

	return `function FindProxyForURL(url, host) {
    host = host.toLowerCase();
` + rule + `
}`
}