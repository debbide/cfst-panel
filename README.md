# CFST Panel

Linux 上的 Cloudflare 优选 IP 测速与自动 DNS 解析面板。

- 单文件运行，前端已内嵌
- 内置 CloudflareST 测速核心
- Web 面板完整配置
- 默认解析前 6 个优选 IP
- 支持定时任务

## 一键安装

自动识别 `amd64 / arm64`，下载最新 Release，并安装 systemd 服务。

```bash
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | sudo bash
```

装完后打开：

```text
http://服务器IP:8787
账号: admin
密码: admin123
```

首次登录后请立刻改密码。

配置文件位置：

```text
/opt/cfst-panel/panel.json
```

程序**只会读安装目录下的 `panel.json`**，不会自动去找你以前目录里的配置。\n如果旧配置里还写着 `/root/CloudflareST` 这类路径，新版会在启动时自动改到安装目录（避免 `permission denied`）。  
迁移旧配置：

```bash
# 方式 1：安装时直接导入
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | \
  sudo env PANEL_JSON=/旧路径/panel.json bash

# 方式 2：手动拷贝后重启
sudo systemctl stop cfst-panel
sudo cp /旧路径/panel.json /opt/cfst-panel/panel.json
sudo chown cfst:cfst /opt/cfst-panel/panel.json
sudo systemctl start cfst-panel
```

## 可选参数

```bash
# 指定目录 / 端口 / 版本
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | \
  sudo env INSTALL_DIR=/opt/cfst-panel LISTEN_ADDR=0.0.0.0:8787 VERSION=latest bash

# 指定加速源：ghfast / ghproxy / moeyy / direct
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | \
  sudo env MIRROR=ghfast bash

# 只装二进制，不装 systemd
curl -fsSL https://ghfast.top/https://raw.githubusercontent.com/debbide/cfst-panel/main/scripts/install.sh | \
  sudo env NO_SERVICE=1 bash
```

## 面板怎么用

1. 填 Cloudflare `API Token` 和 `Zone ID`
2. 添加要更新的域名，例如 `cf.example.com`
3. 默认会拉取优选 IP 列表再本机复测
4. 默认只测 IPv4，并把前 6 个 IP 同步到 DNS
5. 点“立即测速并更新”

## 常用命令

```bash
systemctl status cfst-panel
systemctl restart cfst-panel
journalctl -u cfst-panel -f
```

## 手动运行

```bash
# amd64
chmod +x ./cfst-panel
./cfst-panel --addr 0.0.0.0:8787

# arm64
chmod +x ./cfst-panel-arm64
./cfst-panel-arm64 --addr 0.0.0.0:8787
```

数据默认写在可执行文件同目录。

## Telegram 通知

面板原生支持 Telegram。Workers 只做 `api.telegram.org` 中转，不处理业务逻辑。

### 通知格式

```text
[OK] CFST Panel 任务通知
状态: success
触发: manual
优选IP: 1.2.3.4
延迟: 20.12 ms
速度: 35.60 MB/s
丢包: 0.00%
DNS更新: 6
说明: selected 1.2.3.4, updated 6 record(s)
时间: 2026-07-18 12:00:00
版本: 0.3.5
```

### 1. 创建 Bot

1. Telegram 找 @BotFather 创建 bot，拿到 token
2. 给 bot 发一条消息，获取 chat_id

### 2. 部署 API 中转 Worker

示例文件：`scripts/cf-worker-tg.js`

Cloudflare Dashboard 新建 Worker，粘贴该文件并部署，得到：

```text
https://tg-api.xxx.workers.dev
```

### 3. 面板填写

- 启用 Telegram 通知：开
- Bot Token：你的 token
- Chat ID：你的 chat id
- API 中转地址：`https://tg-api.xxx.workers.dev`
  - 国外服务器可留空，默认直连 `https://api.telegram.org`

点“发送测试”，收到测试消息就说明配置正确。

