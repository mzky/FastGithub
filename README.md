# FastGithub Go

GitHub 加速工具的 Go 语言实现 MVP 版本。通过纯净 DNS 解析、IP 测速选优和 TLS 连接优化，提升 GitHub 访问速度。

## 功能特性

### 核心功能
- **域名纯净 IP 解析**：通过指定 DNS 上游解析域名，获取真实 IP 列表
- **IP 测速选优**：对候选 IP 进行 TCP+TLS 握手测速，自动选择延迟最低的 IP
- **HTTPS MITM 代理**：支持 HTTP/HTTPS 透明代理，动态签发域名证书
- **SNI 自定义**：可配置域名的 SNI 覆盖值，绕过 SNI 检测
- **CA 证书管理**：自动生成/加载 CA 根证书

### 增强功能
- **系统托盘**：最小化到系统托盘，支持状态面板、复制代理地址、检查更新、退出（Windows 构建需 `-tags systray`）
- **系统代理开关**：启动时自动开启系统代理（通过 PAC 自动配置，**仅代理配置的 GitHub 相关域名**，其余请求直连，不影响其他网络访问），退出/异常关闭时自动关闭并恢复原状，可通过配置关闭
- **流量统计**：实时统计上传/下载速率、累计流量、活跃连接数
- **日志缓冲**：环形日志缓冲，支持 Web UI 查看
- **Web 状态面板**：内置 HTTP 服务，实时展示流量统计和运行日志
- **静默启动**：默认不显示控制台窗口，`-console` 参数启用前台输出
- **在线更新**：支持从 GitHub Releases 自动检查并应用更新

## 项目结构

```
cmd/fastgithub/       # 主入口
internal/
  config/             # 配置管理（支持热重载）
  dns/                # DNS 解析（miekg/dns）
  speed/              # IP 测速选优
  tls/                # CA 证书与动态证书签发
  proxy/              # HTTP/HTTPS 代理核心
  updater/            # 在线更新（GitHub Releases）
  flow/               # 流量统计
  logger/             # 日志缓冲
  ui/                 # Web UI 状态面板
  tray/               # 系统托盘（可选，build tag: systray）
  version/            # 版本信息
config.json           # 配置文件
Makefile              # 构建脚本
```

## 快速开始

### 编译

```bash
# 默认构建（无系统托盘，纯 Go，支持交叉编译）
go build -o fastgithub.exe ./cmd/fastgithub

# 带系统托盘构建（Windows 需 CGO）
go build -tags systray -o fastgithub.exe ./cmd/fastgithub

# 全平台交叉编译
make build-all
```

### 运行

```bash
# 静默启动（默认，无控制台输出）
.\fastgithub.exe

# 前台运行（显示日志）
.\fastgithub.exe -console

# 禁用系统托盘
.\fastgithub.exe -no-tray

# 查看版本
.\fastgithub.exe -v

# 指定配置文件
.\fastgithub.exe -c myconfig.json
```

### 使用

1. 启动程序后，代理默认监听 `127.0.0.1:38457`
2. 状态面板访问地址：`http://127.0.0.1:38458`
3. 在浏览器/系统中设置 HTTP 代理为 `127.0.0.1:38457`
4. 首次使用需安装 CA 证书（位于 `cacert/fastgithub.cer`）到受信任的根证书颁发机构

## 配置说明

```json
{
  "proxy": {
    "listen": "127.0.0.1:38457",
    "enable_system_proxy": true
  },
  "ui": { "listen": "127.0.0.1:38458" },
  "dns": {
    "server": "223.5.5.5:53",
    "upstreams": ["8.8.8.8:53", "1.1.1.1:53"]
  },
  "speed_test": {
    "concurrent": 10,
    "timeout": 2000000000,
    "cache_ttl": 300000000000
  },
  "update": {
    "auto_check": true,
    "check_interval": 86400000000000,
    "repo": "creazyboyone/fastgithub",
    "prerelease": false
  },
  "cert_dir": "cacert",
  "domains": [
    {
      "name": "GitHub",
      "patterns": ["github.com", "*.github.com"],
      "sni_patterns": ["github.com"]
    }
  ]
}
```

## 命令行参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `-c` | 配置文件路径 | `config.json` |
| `-v` | 显示版本信息 | - |
| `-console` | 启用控制台输出 | `false` |
| `-no-tray` | 禁用系统托盘 | `false` |

## Web UI API

| 路径 | 说明 |
|------|------|
| `/` | 状态面板首页 |
| `/api/stats` | 流量统计（JSON） |
| `/api/logs` | 最近日志（JSON） |
| `/api/version` | 版本信息（JSON） |

## 测试

```bash
# 单元测试
make test

# 完整测试（含网络集成测试）
make test-full
```

## 支持平台

- Windows (amd64, arm64)
- Linux (amd64, arm64)
- macOS (amd64, arm64)

> 系统托盘功能需使用 `-tags systray` 构建，Windows 下需 CGO 支持。
