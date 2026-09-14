//go:build !systray

package tray

import (
	"github.com/creazyboyone/fastgithub/internal/flow"
	"github.com/creazyboyone/fastgithub/internal/logger"
)

// Manager 系统托盘管理器（无托盘存根）
type Manager struct{}

// New 创建托盘管理器（无托盘存根）
func New(flow *flow.Analyzer, logBuf *logger.Buffer, proxyAddr, uiAddr string) *Manager {
	return &Manager{}
}

// SetOnQuit 设置退出回调（无托盘存根）
func (m *Manager) SetOnQuit(fn func()) {}

// SetOnCheckUpdate 设置检查更新回调（无托盘存根）
func (m *Manager) SetOnCheckUpdate(fn func()) {}

// Run 运行托盘（无托盘存根，直接返回）
func (m *Manager) Run() {}

// Quit 退出托盘（无托盘存根）
func (m *Manager) Quit() {}

// Available 返回托盘是否可用
func Available() bool { return false }
