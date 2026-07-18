# CFST Panel

在 Ubuntu / Linux 上运行的 Cloudflare 优选 IP 测速与自动解析面板。

通过 Web 面板完成全部配置：Cloudflare 账号、DNS 记录、CloudflareST 参数、优选策略、定时任务、通知与运行日志。

**开箱即用：**
- 单文件二进制（前端已内嵌）
- 内置官方 CloudflareST v2.3.5（linux/amd64、linux/arm64）
- 数据默认写在可执行文件同目录

## 功能

- 完整前端配置面板（无需手改配置文件）
- 内置测速核心，无需再单独下载 CloudflareST
- 支持拉取他人已测优选 IP 列表，再在本机复测
- 手动 / 定时测速
- 按延迟、速度、丢包等策略优选 IP
- 默认把前 6 个优选 IP 同步到同一域名（可配置 1-20）
- 自动更新 Cloudflare DNS 记录（DNS-only，不走橙云代理）
- 任务历史、运行日志、Webhook 通知
- Dry-run 预演模式
- 默认仅 IPv4；IPv6 / 双栈可选，按需更新 A / AAAA 记录

## 最快用法（服务器）

把对应架构的单文件上传到 Ubuntu / Linux：

```bash
# x86_64
chmod +x ./cfst-panel
./cfst-panel --addr 0.0.0.0:8787

# arm64
chmod +x ./cfst-panel-arm64
./cfst-panel-arm64 --addr 0.0.0.0:8787
```

浏览器打开：`http://服务器IP:8787`

默认账号：
- 用户名：`admin`
- 密码：`admin123`

首次登录后请立刻在“完整配置”里修改密码。

启动后程序会自动在同目录释放：
- `CloudflareST`（测速核心）
- `ip.txt` / `ipv6.txt`
- `panel.json`（配置与任务数据）
- `result.csv`（测速结果）

## 一键安装（推荐）

自动识别 `amd64/arm64`，下载 GitHub Release 最新版，并安装 systemd 服务。

```bash
# 推荐：走加速镜像拉安装脚本
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | sudo bash

# 或直连 GitHub
curl -fsSL https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | sudo bash
```

可选参数：

```bash
# 指定安装目录 / 监听端口 / 版本
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | \
  sudo env INSTALL_DIR=/opt/cfst-panel LISTEN_ADDR=0.0.0.0:8787 VERSION=latest bash

# 强制使用某个加速源：ghfast / ghproxy / moeyy / direct
sudo env MIRROR=ghfast bash install.sh

# 只装二进制，不装 systemd
sudo env NO_SERVICE=1 bash install.sh
```

安装后默认：
- 目录：`/opt/cfst-panel`
- 面板：`http://服务器IP:8787`
- 账号：`admin / admin123`


## 面板里怎么配

1. **Cloudflare**：填 API Token、Zone ID
2. **DNS 记录**：新增要自动解析的域名（如 `cf.example.com`）
3. **测速引擎**：
   - 默认使用优选 IP 列表 URL，例如 `https://cf.090227.xyz/ct?ips=20`
   - 默认只测 IPv4；没有 IPv6 就别开
4. **DNS 记录**：默认加 A 记录；只有要 v6 时再加 AAAA
5. **优选策略**：
   - 延迟 / 速度 / 丢包阈值
   - **解析 IP 数量**默认 `6`
6. **定时任务**：可选开启 Cron（如 `0 */2 * * *` 每 2 小时）
7. 点“立即测速并更新”

建议第一次先勾选 **Dry-run**，确认优选 IP 无误后再关闭并正式更新 DNS。

## 现成产物

```text
dist/cfst-panel        # linux/amd64 单文件（内置 CFST）
dist/cfst-panel-arm64  # linux/arm64 单文件（内置 CFST）
```

## 自己编译

```bash
# Linux amd64 + arm64
bash scripts/build-linux.sh

# 或手动
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -o dist/cfst-panel ./cmd/server
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -o dist/cfst-panel-arm64 ./cmd/server
```

常用参数：
- `--addr 0.0.0.0:8787` 监听地址
- `--data /path/to/data` 数据目录（默认=可执行文件同目录）
- `--web /path/to/web` 可选外置前端；留空使用内嵌 UI

## systemd（可选）

```bash
sudo bash scripts/deploy-ubuntu.sh /opt/cfst-panel
# 或手动参考 deploy/cfst-panel.service
```

## 目录结构

```text
cmd/server           入口
internal/api         HTTP API
internal/cfst        CloudflareST 调用封装
internal/cfstbin     内置 CloudflareST 资源
internal/cloudflare  Cloudflare DNS 客户端
internal/config      运行配置
internal/model       数据模型
internal/scheduler   定时任务
internal/service     业务逻辑
internal/store       本地 JSON 存储
internal/webui       内嵌前端
web                  前端源码
scripts              构建 / 部署脚本
third_party          CloudflareST 上游包
```

## 版本

当前版本：`0.3.3`