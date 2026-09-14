package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"time"

	"github.com/creazyboyone/fastgithub/internal/config"
	"github.com/creazyboyone/fastgithub/internal/flow"
	"github.com/creazyboyone/fastgithub/internal/logger"
	"github.com/creazyboyone/fastgithub/internal/speed"
	tlscert "github.com/creazyboyone/fastgithub/internal/tls"
)

// Server HTTP/HTTPS 代理服务器
type Server struct {
	cfg     *config.Manager
	tester  *speed.Tester
	certMgr *tlscert.CertManager
	flow    *flow.Analyzer
	logBuf  *logger.Buffer
	server  *http.Server
	proxy   *httputil.ReverseProxy
}

// NewServer 创建代理服务器
func NewServer(cfg *config.Manager, tester *speed.Tester,
	certMgr *tlscert.CertManager, flow *flow.Analyzer, logBuf *logger.Buffer) *Server {

	s := &Server{
		cfg:     cfg,
		tester:  tester,
		certMgr: certMgr,
		flow:    flow,
		logBuf:  logBuf,
	}

	transport := &http.Transport{
		DialContext:            s.dialContext,
		DialTLSContext:         s.dialTLSContext,
		MaxIdleConns:           100,
		IdleConnTimeout:        90 * time.Second,
		TLSHandshakeTimeout:    10 * time.Second,
		ExpectContinueTimeout:  1 * time.Second,
		DisableCompression:     false,
	}

	s.proxy = &httputil.ReverseProxy{
		Director:  s.director,
		Transport: transport,
		ErrorLog:  log.New(&proxyLogWriter{buf: logBuf, prefix: "[proxy] "}, "", 0),
	}

	s.server = &http.Server{
		Handler:      s,
		IdleTimeout:  120 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		ConnContext:  s.connContext,
	}
	return s
}

// Run 启动代理服务器
func (s *Server) Run(addr string) error {
	s.server.Addr = addr
	s.logBuf.Info("[proxy] listening on %s", addr)
	return s.server.ListenAndServe()
}

// Stop 停止代理服务器
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

// connContext 跟踪新连接
func (s *Server) connContext(ctx context.Context, c net.Conn) context.Context {
	s.flow.AddConn()
	return ctx
}

// ServeHTTP 处理所有代理请求
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		s.handleConnect(w, r)
		return
	}
	s.proxy.ServeHTTP(w, r)
}

// handleConnect 处理 HTTPS 隧道（透明代理，不解密流量）
func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if host == "" {
		http.Error(w, "missing host", http.StatusBadRequest)
		return
	}

	serverName := hostOnly(host)
	port := portOnly(host)
	if port == "" {
		port = "443"
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}

	// 建立到目标服务器的连接（配置域名逐个候选 IP 重试，失败才直连）
	targetAddr := net.JoinHostPort(serverName, port)
	var d net.Dialer
	targetConn, err := s.dialTarget(r.Context(), serverName, port)
	if err != nil {
		// 非代理域名 / 候选 IP 全部失败时回退为直连
		targetConn, err = d.DialContext(r.Context(), "tcp", targetAddr)
	}
	if err != nil {
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		clientConn.Close()
		s.logBuf.Error("[proxy] connect %s -> %s error: %v", host, targetAddr, err)
		return
	}

	// 告诉客户端连接已建立
	if _, err := clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		clientConn.Close()
		targetConn.Close()
		return
	}

	s.logBuf.Debug("[proxy] tunnel established: %s -> %s", host, targetAddr)

	// 双向转发数据并统计流量
	s.tunnelWithFlow(clientConn, targetConn)
	s.flow.DecConn()
}

// tunnelWithFlow 双向转发数据并统计流量
// 上传：客户端 -> 服务端
// 下载：服务端 -> 客户端
func (s *Server) tunnelWithFlow(client, server net.Conn) {
	defer client.Close()
	defer server.Close()

	done := make(chan struct{}, 2)

	// 上传方向：客户端 -> 服务端
	go func() {
		buf := make([]byte, 32*1024)
		for {
			nr, err := client.Read(buf)
			if nr > 0 {
				s.flow.AddUpload(int64(nr))
				nw, werr := server.Write(buf[:nr])
				if werr != nil {
					break
				}
				if nw != nr {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}()

	// 下载方向：服务端 -> 客户端
	go func() {
		buf := make([]byte, 32*1024)
		for {
			nr, err := server.Read(buf)
			if nr > 0 {
				s.flow.AddDownload(int64(nr))
				nw, werr := client.Write(buf[:nr])
				if werr != nil {
					break
				}
				if nw != nr {
					break
				}
			}
			if err != nil {
				break
			}
		}
		done <- struct{}{}
	}()

	// 等待任意一端关闭
	<-done
}

// director 修改普通 HTTP 请求
func (s *Server) director(req *http.Request) {
	if req.URL.Scheme == "" {
		req.URL.Scheme = "http"
	}
	if req.URL.Host == "" {
		req.URL.Host = req.Host
	}
	req.Header.Del("Proxy-Connection")
}

// dialContext 自定义 TCP 拨号：对配置域名逐个候选 IP 重试
func (s *Server) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	conn, err := s.dialTarget(ctx, host, port)
	if err != nil {
		// 非代理域名 / 候选 IP 全部失败时回退为直连
		s.logBuf.Debug("[proxy] dial %s fallback: %v", addr, err)
		var d net.Dialer
		conn, err = d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
	}

	// 包装连接用于流量统计（HTTP 代理模式下）
	return &flowConn{Conn: conn, flow: s.flow, upload: false}, nil
}

// dialTLSContext 自定义 TLS 拨号：对配置域名逐个候选 IP 重试，并覆盖 SNI
func (s *Server) dialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	serverName := s.cfg.ResolveSNI(host)
	if serverName == "" {
		serverName = host
	}

	// 先建立 TCP 连接（配置域名逐个候选 IP 重试）
	conn, err := s.dialTarget(ctx, host, port)
	if err != nil {
		// 非代理域名 / 候选 IP 全部失败时回退为直连
		s.logBuf.Debug("[proxy] dial tls %s fallback: %v", addr, err)
		var d net.Dialer
		conn, err = d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
	}

	// 包装基础连接用于下载流量统计
	baseConn := &flowConn{Conn: conn, flow: s.flow, upload: false}

	tlsConn := tls.Client(baseConn, &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

// dialTarget 对配置域名逐个尝试候选 IP 建立真实 TCP 连接。
// 每失败一次都将该 IP 标记为失败并自动切换下一个候选，全部失败才返回错误。
// 注意：只用真实拨号结果判定，避免"TCP 预检通过、实际连接却被重置"的情况。
func (s *Server) dialTarget(ctx context.Context, host, port string) (net.Conn, error) {
	if s.cfg.FindDomain(host) == nil {
		return nil, fmt.Errorf("domain not configured")
	}

	var lastErr error
	tried := make(map[string]struct{}) // 本次调用已尝试过的 IP，避免重复拨号
	maxAttempts := s.resolveAttempts()
	for attempt := 0; attempt < maxAttempts; attempt++ {
		bestIP, err := s.tester.BestIP(host)
		if err != nil {
			return nil, err
		}

		key := bestIP.String()
		if _, done := tried[key]; done {
			// 候选列表已拨完且无新 IP，直接放弃本路，避免对同一批 IP 反复重拨
			break
		}
		tried[key] = struct{}{}

		target := net.JoinHostPort(key, port)
		dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		conn, err := new(net.Dialer).DialContext(dialCtx, "tcp", target)
		cancel()
		if err == nil {
			return conn, nil
		}

		// 该 IP 真实拨号失败，标记失败并切换下一个候选
		lastErr = err
		s.tester.MarkIPFailed(host, bestIP)
		s.logBuf.Debug("[proxy] %s ip %s unreachable (%v), switch to next", host, key, err)
	}
	return nil, lastErr
}

// resolveAttempts 返回每次连接最多尝试的候选 IP 数量（读配置，缺省 4）
func (s *Server) resolveAttempts() int {
	attempts := 4
	if c := s.cfg.Get().SpeedTest.ProbeCount; c > 0 {
		attempts = c
	}
	return attempts
}

// hostOnly 从 host:port 中提取 host
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// portOnly 从 host:port 中提取 port
func portOnly(host string) string {
	if _, p, err := net.SplitHostPort(host); err == nil {
		return p
	}
	return ""
}

// flowConn 包装 net.Conn 以统计流量（用于 HTTP 代理模式）
// 在隧道模式下不使用此包装，直接在 tunnelWithFlow 中统计
type flowConn struct {
	net.Conn
	flow   *flow.Analyzer
	upload bool // true=上传方向, false=下载方向
}

func (c *flowConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		if c.upload {
			c.flow.AddUpload(int64(n))
		} else {
			c.flow.AddDownload(int64(n))
		}
	}
	return n, err
}

func (c *flowConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	if n > 0 {
		if c.upload {
			c.flow.AddDownload(int64(n))
		} else {
			c.flow.AddUpload(int64(n))
		}
	}
	return n, err
}

// proxyLogWriter 适配 log 到 logger.Buffer
type proxyLogWriter struct {
	buf    *logger.Buffer
	prefix string
}

func (w *proxyLogWriter) Write(p []byte) (int, error) {
	msg := string(p)
	if len(msg) > 0 && msg[len(msg)-1] == '\n' {
		msg = msg[:len(msg)-1]
	}
	w.buf.Info("%s%s", w.prefix, msg)
	return len(p), nil
}
