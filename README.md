# Vless 流量审计系统 (vless-audit)

对 Vless（Xray-core）服务端流量进行审计，通过 Web Dashboard 实时展示。

## 一键安装

```bash
# Linux (需要 root)
curl -sSL https://your-release-url/install.sh | bash

# Windows
# 下载 install.bat，右键"以管理员身份运行"
```

安装脚本自动完成：下载 Xray-core → 下载 vless-audit → 生成配置 → 创建 systemd 服务 → 启动

安装后访问 `http://服务器IP:8080/app/`

## 架构

```
┌──────────┐  gRPC     ┌──────────────┐  HTTP/SSE  ┌──────────┐
│  Xray    │◄─────────│ vless-audit   │◄──────────│ 浏览器    │
│  API     │  Stats    │  (Go 单体)    │  REST      │ Dashboard│
│  :10085  │           │               │            │          │
├──────────┤           │ ┌───────────┐ │            │          │
│ access   │  tail     │ │ collector │ │            │          │
│ .log     │◄─────────│ │ + SQLite  │ │            │          │
└──────────┘           │ └───────────┘ │            │          │
                       └──────────────┘            └──────────┘
```

## 功能

- **实时流量监控** — 通过 Xray gRPC Stats API 采集上行/下行流量，按分钟聚合
- **连接审计** — 实时 tail 访问日志，解析每条代理连接的来源、目标、流量、耗时
- **用户排行** — 按用户统计流量 TopN
- **Web Dashboard** — Chart.js 折线图 + 实时连接表格，SSE 推送
- **单二进制部署** — 前后端一体，不依赖外部 Web 服务器

## 快速开始

### 1. 配置 Xray

在 Xray 的 `config.json` 中添加以下关键配置（完整示例见 `xray-config.example.json`）：

```json
{
  "log": { "access": "/var/log/xray/access.log" },
  "api": { "tag": "api", "services": ["StatsService"] },
  "stats": {},
  "policy": {
    "levels": { "0": { "statsUserUplink": true, "statsUserDownlink": true } }
  },
  "inbounds": [
    {
      "tag": "api-in",
      "listen": "127.0.0.1",
      "port": 10085,
      "protocol": "dokodemo-door",
      "settings": { "address": "127.0.0.1" }
    }
  ]
}
```

**关键说明：**

- `statsUserUplink/Downlink: true` — 必须开启，否则无法按用户统计流量
- API inbound 必须监听在 `127.0.0.1`（本地），外部不可达以保证安全
- 每个用户的 `email` 字段用于区分用户身份

> ⚠️ **Xray 版本兼容性**：Xray v26.x 的 gRPC API（StatsService）存在 bug，dokodemo-door 无法正确拦截 API 流量导致连接回环。如需获取上下行字节数和连接耗时，请使用 **Xray v1.8.x**（已验证 v1.8.21+ API 正常工作）。vless-audit 在 v26 下仍可采集连接日志的所有其他字段。

重启 Xray 使配置生效。

### 2. 下载 / 编译

```bash
# 编译
go build -buildvcs=false -o vless-audit ./cmd/vless-audit/

# 或直接运行
go run -buildvcs=false ./cmd/vless-audit/
```

### 3. 运行

```bash
./vless-audit -config config.json
```

首次运行会在当前目录生成 `config.json`（默认配置），可按需修改后重启。

### 4. 打开 Dashboard

浏览器访问 `http://localhost:8080/app/`

## 配置项

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `listen` | `:8080` | Web 服务监听地址 |
| `db_path` | `./vless-audit.db` | SQLite 数据库路径 |
| `xray_api` | (空 = 禁用) | Xray gRPC API 地址，留空则只采集日志 |
| `access_log` | `/var/log/xray/access.log` | Xray 访问日志路径 |
| `poll_interval_sec` | `10` | Stats 轮询间隔（秒） |
| `retention_days` | `30` | 数据保留天数 |

## API 端点

| 端点 | 说明 |
|------|------|
| `GET /api/stats/realtime?minutes=60` | 近 N 分钟流量趋势 |
| `GET /api/traffic/summary?period=24h` | 流量汇总 |
| `GET /api/traffic/top?period=24h&limit=10` | 用户流量排行 |
| `GET /api/connections?user=&limit=50&offset=0` | 连接记录分页 |
| `GET /api/users` | 已知用户列表 |
| `GET /api/events/stream` | SSE 实时推送 |

## 注意

- Xray 需启用 `StatsService` 并配置 API inbound
- 访问日志文件路径需要可读权限
- 首次启动时，日志文件可能还不存在（collector 会自动等待）
- Stats API 轮询到的计数器是**累计值**，程序自动计算增量
