# LLM Logger

> 挂在 newapi 前方的反向代理 + 上下文归档系统. 透明转发 LLM 请求的同时
> 完整捕获请求/响应上下文 (含 SSE 流式), 为亿级调用量提供审计、用户画像、
> 用量统计、错误诊断的一站式能力.

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.22+-00ADD8.svg)](https://golang.org)
[![Python](https://img.shields.io/badge/python-3.11+-3776AB.svg)](https://python.org)
[![ClickHouse](https://img.shields.io/badge/clickhouse-24.8+-FFCC01.svg)](https://clickhouse.com)

---

## 监听范围

| 协议 | 路径 | 流式 |
| --- | --- | --- |
| OpenAI Chat | `POST /v1/chat/completions` | SSE |
| OpenAI Responses | `POST /v1/responses` | SSE |
| Anthropic Messages | `POST /v1/messages` | SSE |
| Gemini | `POST /v1beta/models/{model}:generateContent` / `:streamGenerateContent` | SSE |

Deepseek / Qwen / Moonshot / Zhipu 等 OpenAI 兼容模型走 `/v1/chat/completions`, 自动覆盖.

---

## 架构总览

```text
                     ┌──────────────────────┐
                     │  Clients (任意 SDK)   │
                     └──────────┬───────────┘
                                │ HTTPS
                                ▼
            ┌─────────────────────────────────────┐
            │ Gateway (Go)                        │
            │   反向代理 + tee 旁路采集             │
            │   UA 客户端识别 / 磁盘兜底队列         │
            └────────┬────────────────────┬───────┘
                     │                    │
             ClickHouse ◀───────  元数据 + 小 body
                (列存)
                     │
                     │   大 body (> 32KB, zstd) ───▶ COS / S3 兼容对象存储
                     │
                     ▼
            ┌─────────────────────────────────────┐
            │ Query API (Python FastAPI)          │
            │   + Admin UI (React + Tailwind)     │
            └─────────────────────────────────────┘
```

---

## 仓库结构

```text
llm-logger/
├── gateway/                 # Go 反向代理 + 采集管道
├── query-api/               # Python FastAPI 查询服务
├── admin-ui/                # React + Vite + Tailwind 管理面板
├── migrations/clickhouse/   # 3 个 ClickHouse DDL 文件 (初始化一次)
├── deploy/
│   ├── clickhouse/          # CH 配置 (config/users/storage_policy)
│   ├── minio/               # MinIO 初始化脚本 (S3 mock)
│   └── scripts/             # server-install.sh 一键部署脚本
├── docs/
│   ├── architecture.md      # 架构、schema、数据流、性能、安全
│   ├── deployment.md        # 一次性部署 + 运维 + 故障排查
│   └── api.md               # REST API 参考 + 客户端类型枚举
├── docker-compose.yml       # 默认全栈 (clickhouse + minio + gateway + query-api + admin-ui)
├── docker-compose.prod.yml  # 生产 override (绑 127.0.0.1 + 资源限制)
├── Makefile                 # deploy / stop / restart / status / logs 封装
├── .env.example             # 开发环境模板
└── .env.prod.example        # 生产环境模板 (随机密码占位符)
```

---

## 快速开始

### 一键部署到 Linux 服务器

```bash
curl -fsSL https://raw.githubusercontent.com/thb10086/new-api-logger/main/deploy/scripts/server-install.sh \
  | sudo bash
```

脚本自动完成: 装 Docker → clone 仓库 → 生成随机密码 `.env` → 构建镜像 → 启动全栈 → 初始化 schema/bucket → 打印登录信息.

### 本地 Docker Compose

```bash
git clone https://github.com/thb10086/new-api-logger.git llm-logger && cd llm-logger
cp .env.example .env
docker compose up -d
docker compose ps
```

启动后 (默认端口):

- Admin UI:  <http://localhost:28888>   (用 `.env` 的 `LLMLOG_API_TOKEN` 登录)
- Gateway :  <http://localhost:28080>   (把客户端 `base_url` 改为此地址)
- MinIO   :  <http://localhost:29001>

### 本地热重载开发

```bash
cp .env.example .env
docker compose up -d clickhouse minio minio-init   # 仅基础设施

# 开 3 个终端
make gateway-run       # Go 后端
make query-api-run     # Python 查询 API
make admin-ui-dev      # Vite HMR
```

### 冒烟测试

```bash
# 1. 发一条请求穿过 Gateway
curl -N http://localhost:28080/v1/chat/completions \
  -H "Authorization: Bearer sk-test" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'

# 2. 面板查看 trace
open http://localhost:28888

# 3. 或 REST 查询
TOKEN=$(grep LLMLOG_API_TOKEN .env | cut -d= -f2)
curl -H "Authorization: Bearer $TOKEN" http://localhost:28888/api/v1/traces?limit=10
```

---

## 核心特性

- **零侵入 newapi** — 反向代理前置, newapi 无需任何改动
- **SSE 流式透传** — `io.TeeReader` 边流边采, 首 token 额外延迟 < 1ms
- **统一归档** — 4 种 LLM 协议归一到单张表, 新增协议只加 parser
- **冷热分层** — ClickHouse 三级存储 (NVMe → SSD → S3 兼容对象存储)
- **大 body 离线存储** — > 32KB 自动 offload 到 COS, CH 只存引用键
- **全文初筛** — `ngrambf_v1(4)` skip index 支持中英文混合检索
- **物化视图聚合** — 按小时 × 模型 × 用户预聚合, Grafana 亚秒级
- **TTL 自动管理** — 主表 7d/90d/365d 三段生命周期, 过期自动迁移和删除
- **零数据丢失** — CH 不可用时 batch 自动落盘, 恢复后 replayer 回放
- **用户画像** — 按 `user_id` × `client_type` 聚合 token 消耗、请求数、错误率
- **客户端识别** — 自动分类 Claude Code / 各 SDK / 桌面客户端 / 浏览器
- **流式导出** — `/api/v1/export/traces` 按过滤条件导出 JSONL / CSV

---

## 性能指标 (设计目标)

| 指标 | 目标 |
| --- | --- |
| 代理层延迟开销 | < 2ms (P99) |
| SSE 首 token 额外延迟 | < 1ms |
| 单代理实例吞吐 | 10,000+ QPS |
| ClickHouse 写入延迟 | < 100ms (async_insert) |
| 用户时间线查询 | < 200ms (近 30 天) |
| 用量聚合查询 | < 500ms (跨年) |
| 全文检索查询 | < 5s (亿级数据) |

---

## 数据生命周期

| 阶段 | 时长 | 存储位置 |
| --- | --- | --- |
| 热 | 0 - 7 天 | ClickHouse 本地 NVMe |
| 温 | 8 - 90 天 | ClickHouse SSD |
| 冷 | 91 - 365 天 | ClickHouse 挂载 S3/COS |
| 删除 | > 365 天 | 主表 + COS 原文均删除 |
| 聚合视图 | 永久 | ClickHouse (体积极小) |

---

## 文档

- [架构设计](docs/architecture.md) — 组件职责 / Schema 详解 / 数据流 / 安全 / 性能
- [部署指南](docs/deployment.md) — 前置依赖 / 生产部署 / 配置 / 运维 / 故障排查
- [API 参考](docs/api.md) — REST 端点 / 客户端类型枚举 / 错误码 / 客户端示例

---

## 常用命令

```bash
docker compose up -d                            # 启动全栈 (开发)
docker compose -f docker-compose.yml \
  -f docker-compose.prod.yml up -d              # 启动全栈 (生产)
docker compose ps                               # 服务状态
docker compose logs -f gateway                  # 尾随某服务日志
docker compose restart query-api                # 重启某服务
docker compose down                             # 停止并移除容器
docker compose down -v                          # 停止 + 删数据卷 (危险)
```

或用 Makefile:

```bash
make help            # 查看所有命令
make deploy          # docker compose up -d
make deploy-prod     # 生产形态
make status          # 服务状态
make logs            # 聚合日志
make stop | restart  # 停止 | 重启
```

---

## License

MIT
