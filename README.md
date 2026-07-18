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

程序**只会读安装目录下的 `panel.json`**，不会自动去找你以前目录里的配置。  
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