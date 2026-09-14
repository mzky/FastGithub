//go:build windows

package sysproxy

import (
	"fmt"
	"sync"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	internetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	proxyEnableName     = `ProxyEnable`
	proxyServerName     = `ProxyServer`
	autoConfigURLName   = `AutoConfigURL`

	internetOptionSettingsChanged = 39 // INTERNET_OPTION_SETTINGS_CHANGED
	internetOptionRefresh         = 37 // INTERNET_OPTION_REFRESH
)

var (
	mu sync.Mutex

	// 开启前保存的系统代理原始状态，用于退出时恢复
	originalEnable   uint64
	originalServer   string
	originalAutoURL  string
	hadOriginalProxy bool
	hadOriginalAuto  bool
	enabled          bool

	module      = syscall.NewLazyDLL("wininet.dll")
	fnSetOption = module.NewProc("InternetSetOptionW")
)

// Enable 开启系统代理。通过 PAC 自动配置方式，将本地 HTTP 代理指向 proxyAddr，
// 以及 PAC 脚本地址 pacURL。仅配置域名走代理，其余请求直连。
// 开启前会保存原始代理状态，供 Disable 恢复。可重复调用，仅首次生效。
func Enable(proxyAddr, pacURL string) error {
	mu.Lock()
	defer mu.Unlock()

	if enabled {
		return nil
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open internet settings: %w", err)
	}
	defer k.Close()

	// 保存原始状态
	if orig, _, err := k.GetIntegerValue(proxyEnableName); err == nil {
		originalEnable = orig
		hadOriginalProxy = true
	}
	if orig, _, err := k.GetStringValue(proxyServerName); err == nil {
		originalServer = orig
	}
	if orig, _, err := k.GetStringValue(autoConfigURLName); err == nil {
		originalAutoURL = orig
		hadOriginalAuto = true
	}

	// 写入 PAC 自动配置地址并启用
	if err := k.SetStringValue(autoConfigURLName, pacURL); err != nil {
		return fmt.Errorf("set auto config url: %w", err)
	}
	if err := k.SetDWordValue(proxyEnableName, 1); err != nil {
		return fmt.Errorf("enable proxy: %w", err)
	}

	refreshSystemProxy()
	enabled = true
	return nil
}

// Disable 关闭系统代理，恢复开启前的原始状态。可重复调用，幂等。
func Disable() error {
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return nil
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open internet settings: %w", err)
	}
	defer k.Close()

	// 恢复原始 PAC 地址 / 代理服务器地址
	if hadOriginalAuto {
		if err := k.SetStringValue(autoConfigURLName, originalAutoURL); err != nil {
			return fmt.Errorf("restore auto config url: %w", err)
		}
	} else {
		k.DeleteValue(autoConfigURLName)
	}
	if hadOriginalProxy {
		if err := k.SetStringValue(proxyServerName, originalServer); err != nil {
			return fmt.Errorf("restore proxy server: %w", err)
		}
	}
	// 恢复原始启用标志
	if err := k.SetDWordValue(proxyEnableName, uint32(originalEnable)); err != nil {
		return fmt.Errorf("restore proxy enable: %w", err)
	}

	refreshSystemProxy()
	enabled = false
	return nil
}

// refreshSystemProxy 通知系统代理设置已变更
func refreshSystemProxy() {
	fnSetOption.Call(0, internetOptionSettingsChanged, 0, 0)
	fnSetOption.Call(0, internetOptionRefresh, 0, 0)
}