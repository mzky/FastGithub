//go:build !windows

package sysproxy

// Enable 非 Windows 平台无系统代理操作，仅返回 nil。
func Enable(_, _ string) error {
	return nil
}

// Disable 非 Windows 平台无系统代理操作，仅返回 nil。
func Disable() error {
	return nil
}