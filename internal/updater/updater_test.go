package updater

import (
	"runtime"
	"testing"

	"github.com/creazyboyone/fastgithub/internal/version"
)

func TestNewUpdater(t *testing.T) {
	u := New("test/repo")
	if u == nil {
		t.Fatal("New returned nil")
	}
	if u.repo != "test/repo" {
		t.Errorf("expected repo test/repo, got %s", u.repo)
	}
	if u.current != version.Version {
		t.Errorf("expected current version %s, got %s", version.Version, u.current)
	}
}

func TestAssetName(t *testing.T) {
	u := New("test/repo")
	name := u.assetName()

	expectedOS := runtime.GOOS
	expectedArch := runtime.GOARCH

	// 验证文件名包含平台和架构信息
	if len(name) == 0 {
		t.Error("asset name is empty")
	}

	expected := "fastgithub_" + expectedOS + "_" + expectedArch
	if runtime.GOOS == "windows" {
		expected += ".exe"
	}

	if name != expected {
		t.Errorf("asset name mismatch: expected %s, got %s", expected, name)
	}
}

func TestCheckAndUpdateDevVersion(t *testing.T) {
	// dev 版本应跳过更新检查
	u := New("test/repo")
	u.current = "dev"

	err := u.CheckAndUpdate()
	if err != nil {
		t.Errorf("expected no error for dev version, got %v", err)
	}
}

func TestCheckAndUpdateEmptyRepo(t *testing.T) {
	u := New("")
	u.current = "1.0.0"

	err := u.CheckAndUpdate()
	if err != nil {
		t.Errorf("expected no error for empty repo, got %v", err)
	}
}

func TestFetchLatestIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	u := New("cli/cli") // 使用知名仓库测试
	rel, err := u.fetchLatest()
	if err != nil {
		t.Skipf("fetch latest failed (network issue?): %v", err)
	}

	if rel.TagName == "" {
		t.Error("release tag name is empty")
	}
	if len(rel.Assets) == 0 {
		t.Error("release has no assets")
	}

	t.Logf("latest release: %s (%d assets)", rel.TagName, len(rel.Assets))
}
