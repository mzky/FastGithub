//go:build systray

package tray

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/getlantern/systray"
	"github.com/creazyboyone/fastgithub/internal/flow"
	"github.com/creazyboyone/fastgithub/internal/logger"
	"github.com/creazyboyone/fastgithub/internal/version"
	"golang.org/x/sys/windows/registry"
)

const (
	autoStartRunKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	autoStartValue    = "FastGithub"
)

// Manager 系统托盘管理器
type Manager struct {
	flow        *flow.Analyzer
	logBuf      *logger.Buffer
	uiAddr      string
	proxyAddr   string
	onQuit      func()
	onCheckUpdate func()
}

// New 创建托盘管理器
func New(flow *flow.Analyzer, logBuf *logger.Buffer, proxyAddr, uiAddr string) *Manager {
	return &Manager{
		flow:      flow,
		logBuf:    logBuf,
		uiAddr:    uiAddr,
		proxyAddr: proxyAddr,
	}
}

// SetOnQuit 设置退出回调
func (m *Manager) SetOnQuit(fn func()) {
	m.onQuit = fn
}

// SetOnCheckUpdate 设置检查更新回调
func (m *Manager) SetOnCheckUpdate(fn func()) {
	m.onCheckUpdate = fn
}

// Run 运行托盘（阻塞，应在 goroutine 中调用）
func (m *Manager) Run() {
	systray.Run(m.onReady, m.onExit)
}

// Quit 退出托盘
func (m *Manager) Quit() {
	systray.Quit()
}

func (m *Manager) onReady() {
	systray.SetIcon(iconData())
	systray.SetTitle("FastGithub")
	systray.SetTooltip(fmt.Sprintf("FastGithub v%s - 代理 %s", version.Version, m.proxyAddr))

	// 状态菜单项
	mStatus := systray.AddMenuItem("运行中", "FastGithub 运行状态")
	mStatus.Disable()

	// 系统代理设置
	mSysProxy := systray.AddMenuItem("系统代理设置", "打开系统的代理配置窗口")
	go func() {
		for range mSysProxy.ClickedCh {
			openProxySettings()
		}
	}()

	// 开机启动
	mAutoStart := systray.AddMenuItem("开机启动", "是否随 Windows 开机自动启动")
	if m.autoStartEnabled() {
		mAutoStart.Check()
	}
	go func() {
		for range mAutoStart.ClickedCh {
			enable := !mAutoStart.Checked()
			if enable {
				mAutoStart.Check()
			} else {
				mAutoStart.Uncheck()
			}
			m.setAutoStart(enable)
		}
	}()

	systray.AddSeparator()

	// 打开面板
	mOpenUI := systray.AddMenuItem("打开状态面板", "在浏览器中打开状态面板")
	go func() {
		for range mOpenUI.ClickedCh {
			openBrowser("http://" + m.uiAddr)
		}
	}()

	// 复制代理地址
	mCopyProxy := systray.AddMenuItem("复制代理地址", "复制代理地址到剪贴板")
	go func() {
		for range mCopyProxy.ClickedCh {
			copyToClipboard("http://" + m.proxyAddr)
			m.logBuf.Info("[tray] 代理地址已复制到剪贴板")
		}
	}()

	systray.AddSeparator()

	// 检查更新
	mUpdate := systray.AddMenuItem("检查更新", "检查最新版本")
	go func() {
		for range mUpdate.ClickedCh {
			if m.onCheckUpdate != nil {
				go m.onCheckUpdate()
			}
		}
	}()

	// 关于
	mAbout := systray.AddMenuItem("关于", "关于 FastGithub")
	go func() {
		for range mAbout.ClickedCh {
			m.logBuf.Info("[tray] FastGithub v%s (build %s) %s/%s",
				version.Version, version.BuildTime, runtime.GOOS, runtime.GOARCH)
		}
	}()

	systray.AddSeparator()

	// 退出
	mQuit := systray.AddMenuItem("退出", "退出 FastGithub")
	go func() {
		for range mQuit.ClickedCh {
			m.logBuf.Info("[tray] 正在退出...")
			if m.onQuit != nil {
				m.onQuit()
			}
			systray.Quit()
		}
	}()

	m.logBuf.Info("[tray] 系统托盘已启动")
}

func (m *Manager) onExit() {
	m.logBuf.Info("[tray] 系统托盘已退出")
}

// autoStartEnabled 查询当前是否已设置开机启动（仅 Windows，通过注册表 Run 键）
func (m *Manager) autoStartEnabled() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRunKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(autoStartValue)
	return err == nil
}

// setAutoStart 设置开机启动（Windows 下写入/删除 HKCU 注册表 Run 键）
func (m *Manager) setAutoStart(enable bool) {
	if runtime.GOOS != "windows" {
		return
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, autoStartRunKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		m.logBuf.Error("[tray] 打开开机启动注册表键失败: %v", err)
		return
	}
	defer k.Close()

	if enable {
		exe, err := os.Executable()
		if err != nil {
			m.logBuf.Error("[tray] 获取程序路径失败: %v", err)
			return
		}
		if err := k.SetStringValue(autoStartValue, `"`+exe+`"`); err != nil {
			m.logBuf.Error("[tray] 设置开机启动失败: %v", err)
			return
		}
		m.logBuf.Info("[tray] 已开启开机启动 (%s)", exe)
	} else {
		if err := k.DeleteValue(autoStartValue); err != nil {
			m.logBuf.Error("[tray] 关闭开机启动失败: %v", err)
			return
		}
		m.logBuf.Info("[tray] 已关闭开机启动")
	}
}

// openProxySettings 打开系统代理设置窗口（Windows）
func openProxySettings() {
	if runtime.GOOS == "windows" {
		// 优先使用现代设置界面（ms-settings:network-proxy）
		if err := exec.Command("cmd", "/c", "start ms-settings:network-proxy").Run(); err == nil {
			return
		}
		// 回退到旧的 Internet 选项（inetcpl.cpl，第 4 页为连接/代理设置）
		exec.Command("cmd", "/c", "start inetcpl.cpl,4").Start()
	}
}

// openBrowser 在浏览器中打开 URL
func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}

	exec.Command(cmd, args...).Start()
}

// copyToClipboard 复制文本到剪贴板
func copyToClipboard(text string) {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("cmd", "/c", "echo "+text+"| clip")
		cmd.Start()
	case "darwin":
		cmd := exec.Command("pbcopy")
		stdin, _ := cmd.StdinPipe()
		go func() {
			stdin.Write([]byte(text))
			stdin.Close()
		}()
		cmd.Start()
	}
}
