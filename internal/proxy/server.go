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

	// 建立到目标服务器的连接（使用最优 IP）
	targetAddr, err := s.resolveTarget(serverName, port)
	if err != nil {
		s.logBuf.Debug("[proxy] connect %s fallback to direct: %v", host, err)
		targetAddr = net.JoinHostPort(serverName, port)
	}

	var d net.Dialer
	targetConn, err := d.DialContext(r.Context(), "tcp", targetAddr)
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

// dialContext 自定义 TCP 拨号：对配置域名使用最优 IP
func (s *Server) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	target, err := s.resolveTarget(host, port)
	if err != nil {
		s.logBuf.Debug("[proxy] dial %s fallback: %v", addr, err)
		target = addr
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, network, target)
	if err != nil {
		return nil, err
	}

	// 包装连接用于流量统计（HTTP 代理模式下）
	return &flowConn{Conn: conn, flow: s.flow, upload: false}, nil
}

// dialTLSContext 自定义 TLS 拨号：对配置域名使用最优 IP 与 SNI 覆盖
func (s *Server) dialTLSContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}

	target, err := s.resolveTarget(host, port)
	if err != nil {
		s.logBuf.Debug("[proxy] dial tls %s fallback: %v", addr, err)
		target = addr
	}

	serverName := s.cfg.ResolveSNI(host)
	if serverName == "" {
		serverName = host
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, network, target)
	if err != nil {
		return nil, err
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

// resolveTarget 对配置域名返回最优 IP:port
func (s *Server) resolveTarget(host, port string) (string, error) {
	if s.cfg.FindDomain(host) == nil {
		return "", fmt.Errorf("domain not configured")
	}

	bestIP, err := s.tester.BestIP(host)
	if err != nil {
		return "", err
	}

	target := net.JoinHostPort(bestIP.String(), port)
	s.logBuf.Debug("[proxy] %s -> %s", host, target)
	return target, nil
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
