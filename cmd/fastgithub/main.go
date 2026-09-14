package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/creazyboyone/fastgithub/internal/config"
	"github.com/creazyboyone/fastgithub/internal/dns"
	"github.com/creazyboyone/fastgithub/internal/flow"
	"github.com/creazyboyone/fastgithub/internal/logger"
	"github.com/creazyboyone/fastgithub/internal/proxy"
	"github.com/creazyboyone/fastgithub/internal/speed"
	"github.com/creazyboyone/fastgithub/internal/sysproxy"
	"github.com/creazyboyone/fastgithub/internal/tls"
	"github.com/creazyboyone/fastgithub/internal/tray"
	"github.com/creazyboyone/fastgithub/internal/ui"
	"github.com/creazyboyone/fastgithub/internal/updater"
	"github.com/creazyboyone/fastgithub/internal/version"
	"strings"
)

func main() {
	configPath := flag.String("c", "config.json", "配置文件路径")
	versionFlag := flag.Bool("v", false, "显示版本信息")
	consoleFlag := flag.Bool("console", false, "显示控制台窗口（前台运行）")
	noTray := flag.Bool("no-tray", false, "禁用系统托盘")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("FastGithub v%s (build %s) %s/%s\n",
			version.Version, version.BuildTime, version.GitCommit, version.GoVersion)
		return
	}

	// 静默模式：默认不输出到控制台
	logBuf := logger.NewBuffer(1000)
	if *consoleFlag {
		logBuf.SetConsoleOutput(true)
	}

	// 1. 加载配置
	cfg, err := config.LoadManager(*configPath)
	if err != nil {
		logBuf.Error("[main] 加载配置失败: %v", err)
		os.Exit(1)
	}
	logBuf.Info("[main] 配置加载成功，共 %d 个域名规则", len(cfg.Get().Domains))

	// 2. 初始化 CA 证书
	certDir := cfg.CertBasePath()
	certMgr, err := tls.NewCertManager(certDir)
	if err != nil {
		logBuf.Error("[main] 初始化证书管理器失败: %v", err)
		os.Exit(1)
	}
	logBuf.Info("[main] CA 证书加载成功 (路径: %s)", certDir)

	// 3. 初始化 DNS 解析器（自动识别上游类型）
	dnsUpstreams := buildDNSUpstreams(cfg.Get().DNS.Upstreams)
	resolver := dns.NewResolverWithUpstreams(dnsUpstreams)
	logBuf.Info("[main] DNS 解析器初始化完成（%d 个上游）", len(dnsUpstreams))

	// 4. 初始化测速器
	tester := speed.NewTester(resolver)
	logBuf.Info("[main] IP 测速器初始化完成")

	// 5. 初始化流量分析器
	flowAnalyzer := flow.NewAnalyzer()
	flowAnalyzer.Start()
	logBuf.Info("[main] 流量统计模块启动")

	// 6. 启动代理服务器
	proxyAddr := cfg.Get().Proxy.Listen
	proxyServer := proxy.NewServer(cfg, resolver, tester, certMgr, flowAnalyzer, logBuf)

	proxyErr := make(chan error, 1)
	go func() {
		proxyErr <- proxyServer.Run(proxyAddr)
	}()

	// 7. 启动 Web UI
	uiAddr := cfg.Get().UI.Listen
	uiServer := ui.NewServer(flowAnalyzer, logBuf)
	uiServer.SetPACSource(cfg, proxyAddr)

	uiErr := make(chan error, 1)
	go func() {
		uiErr <- uiServer.Run(uiAddr)
	}()

	// 7.1 自动开启系统代理（可选）：通过 PAC 仅代理 GitHub 相关域名
	if cfg.Get().Proxy.SystemProxyEnabled() {
		pacURL := "http://" + uiAddr + "/proxy.pac"
		if err := sysproxy.Enable(proxyAddr, pacURL); err != nil {
			logBuf.Error("[main] 开启系统代理失败: %v", err)
		} else {
			logBuf.Info("[main] 已自动开启系统代理 (PAC: %s，仅代理 GitHub 域名)", pacURL)
		}
	}

	// 8. 初始化自动更新
	upd := updater.New(cfg.Get().Update.Repo)
	if cfg.Get().Update.AutoCheck {
		go func() {
			interval := cfg.Get().Update.CheckInterval
			if interval == 0 {
				interval = 24 * time.Hour
			}
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				if err := upd.CheckAndUpdate(); err != nil {
					logBuf.Error("[update] 自动更新检查失败: %v", err)
				}
			}
		}()
	}

	logBuf.Info("[main] ========================================")
	logBuf.Info("[main] FastGithub v%s 启动成功", version.Version)
	logBuf.Info("[main] 代理地址: http://%s", proxyAddr)
	logBuf.Info("[main] 状态面板: http://%s", uiAddr)

	// 9. 系统托盘（注意：systray 必须在主 goroutine 运行）
	useTray := !*noTray && tray.Available()
	if useTray {
		logBuf.Info("[main] 系统托盘: 已启用")
	} else if !*noTray && !tray.Available() {
		logBuf.Info("[main] 系统托盘: 当前构建不支持")
	}
	logBuf.Info("[main] ========================================")

	if useTray {
		// 托盘模式：systray.Run 必须在主 goroutine 中运行（阻塞）
		trayMgr := tray.New(flowAnalyzer, logBuf, proxyAddr, uiAddr)
		trayMgr.SetOnQuit(func() {
			shutdown(proxyServer, uiServer, flowAnalyzer)
			os.Exit(0)
		})
		trayMgr.SetOnCheckUpdate(func() {
			if err := upd.CheckAndUpdate(); err != nil {
				logBuf.Error("[update] 检查更新失败: %v", err)
			} else {
				logBuf.Info("[update] 更新检查完成")
			}
		})
		// 阻塞运行托盘消息循环（必须在主 goroutine）
		trayMgr.Run()
	} else {
		// 无托盘模式：等待信号或服务退出
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

		select {
		case err := <-proxyErr:
			logBuf.Error("[main] 代理服务器退出: %v", err)
		case err := <-uiErr:
			logBuf.Error("[main] Web UI 退出: %v", err)
		case sig := <-sigCh:
			logBuf.Info("[main] 收到信号 %v，正在关闭...", sig)
		}

		shutdown(proxyServer, uiServer, flowAnalyzer)
	}

	logBuf.Info("[main] 已退出")
}

func shutdown(proxyServer *proxy.Server, uiServer *ui.Server, flowAnalyzer *flow.Analyzer) {
	// 关闭系统代理（恢复正常网络）
	if err := sysproxy.Disable(); err != nil {
		// 关闭代理失败不影响程序退出
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	proxyServer.Stop(ctx)
	uiServer.Stop()
	flowAnalyzer.Stop()
}

// buildDNSUpstreams 从字符串地址列表构建 DNS 上游配置，自动识别模式
func buildDNSUpstreams(addrs []string) []dns.Upstream {
	ups := make([]dns.Upstream, 0, len(addrs))
	for _, addr := range addrs {
		up := dns.Upstream{Addr: addr}
		switch {
		case strings.HasPrefix(addr, "https://"):
			up.Mode = "doh"
		case strings.HasSuffix(addr, ":853"):
			up.Mode = "dot"
			// 尝试从地址中推断 SNI
			if strings.HasPrefix(addr, "223.5.5.5:") || strings.HasPrefix(addr, "223.6.6.6:") {
				up.SNI = "dns.alidns.com"
			} else if strings.HasPrefix(addr, "8.8.8.8:") || strings.HasPrefix(addr, "8.8.4.4:") {
				up.SNI = "dns.google"
			} else if strings.HasPrefix(addr, "1.1.1.1:") || strings.HasPrefix(addr, "1.0.0.1:") {
				up.SNI = "cloudflare-dns.com"
			}
		case addr == "system":
			up.Mode = "system"
		default:
			up.Mode = "udp"
		}
		ups = append(ups, up)
	}
	return ups
}
