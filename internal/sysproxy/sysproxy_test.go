package sysproxy

import (
	"strings"
	"testing"
)

func TestBuildPACIncludesProxyAndGlobalDomains(t *testing.T) {
	pac := BuildPAC("127.0.0.1:38457", []string{"github.com", "*.github.com", "githubusercontent.com"})

	if !strings.Contains(pac, `"PROXY 127.0.0.1:38457"`) {
		t.Errorf("PAC 应包含 PROXY 代理地址，实际: %s", pac)
	}
	if !strings.Contains(pac, `shExpMatch(host, "github.com")`) {
		t.Errorf("PAC 应包含 github.com 匹配规则: %s", pac)
	}
	if !strings.Contains(pac, `shExpMatch(host, "*.github.com")`) {
		t.Errorf("PAC 应包含 *.github.com 匹配规则: %s", pac)
	}
	if !strings.Contains(pac, `return "DIRECT"`) {
		t.Errorf("PAC 对非匹配域名应返回 DIRECT: %s", pac)
	}
}

func TestBuildPACNoPatternsReturnsDirect(t *testing.T) {
	pac := BuildPAC("127.0.0.1:38457", nil)
	if strings.Contains(pac, "PROXY") {
		t.Errorf("无匹配规则时不应包含 PROXY: %s", pac)
	}
	if !strings.Contains(pac, `return "DIRECT"`) {
		t.Errorf("无匹配规则时应仅返回 DIRECT: %s", pac)
	}
}