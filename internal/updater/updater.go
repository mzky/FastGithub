package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/creazyboyone/fastgithub/internal/version"
)

// Updater 在线更新器
type Updater struct {
	repo    string
	current string
}

// release 表示 GitHub Release 的部分字段
type release struct {
	TagName string `json:"tag_name"`
	Assets  []asset `json:"assets"`
	Body    string `json:"body"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// New 创建更新器
func New(repo string) *Updater {
	return &Updater{
		repo:    repo,
		current: version.Version,
	}
}

// CheckAndUpdate 检查并执行更新
func (u *Updater) CheckAndUpdate() error {
	if u.repo == "" || u.current == "" || u.current == "dev" {
		log.Println("[updater] skip check: no repo or dev version")
		return nil
	}

	latest, err := u.fetchLatest()
	if err != nil {
		return fmt.Errorf("fetch latest: %w", err)
	}

	latestVersion := strings.TrimPrefix(latest.TagName, "v")
	currentVersion := strings.TrimPrefix(u.current, "v")

	latestSem, err := semver.NewVersion(latestVersion)
	if err != nil {
		return fmt.Errorf("parse latest version %s: %w", latestVersion, err)
	}
	currentSem, err := semver.NewVersion(currentVersion)
	if err != nil {
		return fmt.Errorf("parse current version %s: %w", currentVersion, err)
	}

	if !latestSem.GreaterThan(currentSem) {
		log.Printf("[updater] current %s is up to date (latest %s)", currentVersion, latestVersion)
		return nil
	}

	log.Printf("[updater] new version available: %s -> %s", currentVersion, latestVersion)

	assetName := u.assetName()
	var target *asset
	for i := range latest.Assets {
		if latest.Assets[i].Name == assetName {
			target = &latest.Assets[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("asset %s not found in release %s", assetName, latest.TagName)
	}

	tmpFile, err := u.download(target.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer os.Remove(tmpFile)

	if err := u.replace(tmpFile); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	log.Println("[updater] update completed, restarting...")
	return u.restart()
}

// fetchLatest 获取最新 release
func (u *Updater) fetchLatest() (*release, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", u.repo)
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api status %d", resp.StatusCode)
	}

	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// assetName 根据当前平台生成 asset 文件名
func (u *Updater) assetName() string {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return fmt.Sprintf("fastgithub_%s_%s%s", runtime.GOOS, runtime.GOARCH, ext)
}

// download 下载文件到临时路径
func (u *Updater) download(url string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "fastgithub-update-*")
	if err != nil {
		return "", err
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return "", err
	}

	if err := os.Chmod(tmp.Name(), 0755); err != nil {
		return "", err
	}
	return tmp.Name(), nil
}

// replace 用新二进制替换当前二进制
func (u *Updater) replace(newPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		oldPath := exe + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(exe, oldPath); err != nil {
			return fmt.Errorf("rename old binary: %w", err)
		}
		if err := os.Rename(newPath, exe); err != nil {
			_ = os.Rename(oldPath, exe)
			return fmt.Errorf("rename new binary: %w", err)
		}
		_ = os.Remove(oldPath)
		return nil
	}

	return os.Rename(newPath, exe)
}

// restart 重启进程
func (u *Updater) restart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		if err := cmd.Start(); err != nil {
			return err
		}
		os.Exit(0)
		return nil
	}

	return syscall.Exec(exe, os.Args, os.Environ())
}
