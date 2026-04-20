# LLM Logger — newapi 上下文采集与归档系统

> 一套高性能的 LLM 请求监听 + 上下文归档系统,作为反向代理挡在 newapi 前面,
> 完整捕获请求/响应上下文(含 SSE 流式),支持千万至亿级别数据规模。

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.22+-00ADD8.svg)](https://golang.org)
[![Python Version](https://img.shields.io/badge/python-3.11+-3776AB.svg)](https://python.org)
[![ClickHouse](https://img.shields.io/badge/clickhouse-23.8+-FFCC01.svg)](https://clickhouse.com)

---

## 监听范围

| 协议 | 路径 | 说明 |
|---|---|---|
| OpenAI Chat | `POST /v1/chat/completions` | 含 SSE,兼容 Deepseek/Qwen/Moonshot/Zhipu 等 |
| OpenAI Responses | `POST /v1/responses` | OpenAI 新一代 stateful API |
| Anthropic Messages | `POST /v1/messages` | Claude 原生协议 |
| Gemini | `POST /v1beta/models/{model}:generateContent` 与 `:streamGenerateContent` | Google 原生协议 |

## 架构总览

```text
                        ┌──────────────────────┐
                        │  Clients (any SDK)   │
                        └──────────┬───────────┘
                                   │ HTTPS
                                   ▼
                  ┌────────────────────────────────┐
                  │    Gateway Proxy (Go)          │
                  │  ┌────────────────────────┐    │
                  │  │ Reverse Proxy (透明转发)│    │
                  │  └──────────┬─────────────┘    │
                  │             │ tee 旁路           │
                  │             ▼                  │
                  │  ┌────────────────────────┐    │
                  │  │  Capture Pipeline      │    │
                  │  │  (4 协议解析 + 重组)    │    │
                  │  └──┬──────────────────┬──┘    │
                  └─────┼──────────────────┼───────┘
                        │                  │
              小 body ↓                ↓ 大 body
                  ┌─────────┐    ┌─────────────┐
                  │ClickHouse│   │   COS/S3    │
                  │ (元数据 +│   │ (zstd 压缩) │
                  │ 短body) │    │             │
                  └────┬────┘    └──────┬──────┘
                       └──────┬─────────┘
                              ▼
                ┌────────────────────────────┐
                │ Query API (Python FastAPI) │
                │  + Admin UI (React)        │
                └────────────────────────────┘
```

## 仓库结构

```text
llm-logger/
├── gateway/                 # Go 反向代理 + 采集服务
├── query-api/               # Python FastAPI 查询服务
├── admin-ui/                # React + Vite + Tailwind 管理面板
├── migrations/clickhouse/   # CH DDL 迁移脚本
├── deploy/
│   ├── clickhouse/          # CH 配置 (config/users/storage_policy)
│   ├── minio/               # MinIO 初始化脚本 (S3 mock)
│   └── scripts/             # 服务器安装脚本 (server-install.sh)
├── docs/                    # 架构 / 部署 / API 文档
├── docker-compose.yml       # 全栈一键部署 (5 服务)
├── docker-compose.prod.yml  # 生产覆盖 (绑 127.0.0.1 + 资源限制)
├── Makefile                 # deploy/stop/restart/status 等封装
├── .env.example             # 开发环境模板
└── .env.prod.example        # 生产环境模板 (含密码占位符)
```

## 快速开始

### 方式 1 - 服务器一键部署 (生产推荐)

```bash
# 全新 Linux VPS (Ubuntu/Debian/CentOS):
curl -fsSL https://raw.githubusercontent.com/<your-org>/llm-logger/main/deploy/scripts/server-install.sh \
  | sudo bash
```

脚本会自动装 Docker → clone 仓库 → 生成随机密码 `.env` → 构建镜像 → 启动全栈 → 初始化 schema → 打印面板地址.

### 方式 2 - 本地一键容器 (开发)

```bash
git clone <repo> llm-logger && cd llm-logger
cp .env.example .env
make deploy          # build + up + migrate + init-minio 一条龙
```

启动完成后:

- **Admin UI (面板)**: <http://localhost:8888> — 用 `.env` 里的 `LLMLOG_API_TOKEN` 登录
- **Gateway (代理)**: <http://localhost:8080> — 把 newapi 客户端的 `base_url` 改为这里
- **MinIO Console**: <http://localhost:9001> — minioadmin/minioadmin

### 方式 3 - 本地热重载 (改代码)

```bash
cp .env.example .env
make up-deps          # 只启动 CH + MinIO
make migrate && make init-minio

# 开 3 个终端
make gateway-run      # Go 后端
make query-api-run    # Python 查询 API
make admin-ui-dev     # Vite HMR
```

### 验证链路

```bash
# 1. 发一条测试请求穿过 Gateway
curl -N http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-test" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'

# 2. 浏览器打开面板查看 trace
#    http://localhost:8888

# 3. 或直接 curl Query API
TOKEN=$(grep LLMLOG_API_TOKEN .env | cut -d= -f2)
curl -H "Authorization: Bearer $TOKEN" http://localhost:8000/api/v1/traces?limit=10
```

## 核心特性

- **零侵入 newapi** — 反向代理前置,newapi 完全无感知
- **SSE 流式透传** — `io.TeeReader` 边流边采,首 token 延迟近乎为零
- **统一归档** — 4 种 LLM 协议归一到统一 schema,新增协议只加解析器
- **冷热分层** — ClickHouse 三级存储 (NVMe → SSD → S3 兼容对象存储)
- **大 body 离线存储** — > 32KB 自动 offload 到 COS,CH 只存 cos_key
- **全文初筛** — CH `ngrambf_v1` 索引支撑中文混合英文场景
- **物化视图聚合** — 按小时 × 模型 × 用户预聚合,Grafana 查询百毫秒级
- **TTL 自动管理** — 主表 7d/90d/365d 三段生命周期,过期自动迁移和删除

## 性能指标 (设计目标)

| 指标 | 目标 |
|---|---|
| 代理层延迟开销 | < 2ms (P99) |
| SSE 首 token 额外延迟 | < 1ms |
| 单代理实例吞吐 | 10,000+ QPS |
| ClickHouse 写入延迟 | < 100ms (async_insert) |
| 用户时间线查询 | < 200ms (近 30 天) |
| 用量聚合查询 | < 500ms (跨年) |
| 全文检索查询 | < 5s (亿级数据) |

## 数据生命周期

| 阶段 | 时长 | 存储位置 |
|---|---|---|
| 热数据 | 0-7 天 | ClickHouse 本地 NVMe |
| 温数据 | 8-90 天 | ClickHouse SSD |
| 冷数据 | 91-365 天 | ClickHouse 挂载 S3/COS |
| 删除 | > 365 天 | 主表 + COS 原文均删除 |
| 聚合视图 | 永久 | ClickHouse (体积极小) |

## 文档

- [架构设计](docs/architecture.md)
- [**部署指南 (含生产 / K8s / newapi 3 种连接模式)**](docs/deployment.md)
- [API 参考](docs/api.md)
- [Proposed Solution (设计阶段记录)](docs/proposal.md)

## 常用命令

```bash
make help            # 看所有命令
make deploy          # 一键全栈部署 (开发 / 快速测试)
make deploy-prod     # 生产部署 (使用 docker-compose.prod.yml)
make status          # 服务状态
make logs            # 日志
make stop/restart    # 停止/重启
make clean-data      # 清空所有数据卷 (危险)
```

## License

MIT
