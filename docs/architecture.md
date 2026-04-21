# 架构设计

## 1. 总览

LLM Logger 是一个**反向代理 + 上下文归档**系统, 挂在 `newapi` 前面透明转发
所有 LLM 请求, 同时旁路完整捕获请求 / 响应上下文 (包含 SSE 流式), 入列存数据库
和对象存储, 为亿级调用量的用户画像、用量统计、错误诊断、全文审计提供一站式能力.

```text
┌─────────┐   HTTPS    ┌────────────┐   HTTPS   ┌────────┐   HTTPS  ┌──────────┐
│ Clients │ ─────────▶ │  Gateway   │ ────────▶ │ newapi │ ───────▶ │ Upstream │
│ (任意   │            │  (Go)      │           │        │          │ (OpenAI/ │
│  SDK)   │ ◀───────── │            │ ◀──────── │        │ ◀──────  │  Claude/ │
└─────────┘            └──────┬─────┘           └────────┘          │  Gemini) │
                              │ tee 旁路                            └──────────┘
                ┌─────────────┼──────────────┐
                │             │              │
                ▼             ▼              ▼
          ┌──────────┐  ┌──────────┐  ┌────────────┐
          │ClickHouse│  │  COS/S3  │  │ Disk Spool │
          │ 元数据 + │  │ 大 body  │  │ (CH 不可用 │
          │ 小 body  │  │  zstd    │  │  时兜底)   │
          └────┬─────┘  └────┬─────┘  └─────┬──────┘
               │             │              │
               └──────┬──────┘   Replayer 回放 (CH 恢复后)
                      ▼
            ┌───────────────────────┐
            │ Query API + Admin UI  │
            └───────────────────────┘
```

---

## 2. 组件职责

### 2.1 Gateway (Go)

Gateway 是整个系统的唯一数据入口. 所有 LLM 流量都经它转发 + 采集, 一旦它停
止数据就停止采集, 因此可靠性设计是第一优先级.

| 模块 | 文件 | 职责 |
| --- | --- | --- |
| 入口 | `gateway/cmd/gateway/main.go` | 启动 HTTP/metrics server, 装配 pipeline |
| 配置 | `gateway/internal/config/config.go` | 从 `LLMLOG_*` 环境变量加载 (12-Factor) |
| 反向代理 | `gateway/internal/proxy/forwarder.go` | 自己实现 (非 `httputil.ReverseProxy`), 精确控制 SSE tee |
| 限大小 buffer | `gateway/internal/proxy/buffer.go` | `LimitedBuffer` 到上限静默截断, 防止 OOM |
| SSE 解析 | `gateway/internal/sse/parser.go` | `data: ` 行切分为 events |
| 协议解析 | `gateway/internal/protocol/*.go` | 4 个 parser 实现 `ParseRequest` / `ParseResponse` / `ParseStreamResponse` |
| 客户端识别 | `gateway/internal/handlers/client_classify.go` | UA → `client_type` 枚举, 同时提取 `client_app_version` |
| Handler | `gateway/internal/handlers/handlers.go` | 串联转发、解析、采集 |
| Capture 管道 | `gateway/internal/capture/pipeline.go` | 异步 worker 池, COS 上传 + CH 批量写 |
| ClickHouse sink | `gateway/internal/sink/clickhouse.go` | async_insert HTTP 直写 + 重试 + 落盘兜底 |
| COS sink | `gateway/internal/sink/cos.go` | S3 兼容客户端, 大 body 上传 + zstd 压缩 |
| 磁盘兜底队列 | `gateway/internal/sink/spool.go` | CH 不可用时 batch 落盘为 NDJSON, replayer 定期扫描回放 |

### 2.2 ClickHouse Schema

主事实表 `llm_call_logs` + 3 个物化视图 + 6 个逻辑视图. 详见 §3.

### 2.3 COS / S3 对象存储

- **路径布局**: `{prefix}/{yyyy}/{mm}/{dd}/{hh}/{request_id}-{request|response}.json.zst`
- **压缩**: zstd level 3 (LLM 上下文压缩比 4-8 倍)
- **生命周期** (生产建议在 COS 控制台配置):
  - 0 - 90 天: 标准存储
  - 91 - 365 天: 低频 / 归档
  - > 365 天: 删除

### 2.4 Query API (Python FastAPI)

屏蔽 ClickHouse + COS 细节, 对外提供统一的 REST. Bearer Token 鉴权.
端点详见 `docs/api.md`.

| 模块 | 路径 |
| --- | --- |
| 入口 | `query-api/app/main.py` |
| 路由 | `query-api/app/routers/{traces,users,stats,search,errors,export,health}.py` |
| 服务层 | `query-api/app/services/{trace,user,stats,search}_service.py` |
| Pydantic schemas | `query-api/app/schemas/__init__.py` |
| CH 驱动封装 | `query-api/app/database/clickhouse.py` (`clickhouse-connect` 异步 wrapper) |
| COS 驱动封装 | `query-api/app/database/cos.py` (`aioboto3`) |

### 2.5 Admin UI (React + Vite + Tailwind)

最小化单页应用, 通过 Query API 查询并呈现. 8 个页面: Traces / TraceDetail /
Users / UserDetail / Search / Errors / Stats / Health. 支持按 provider / model /
user_id / client_type / 错误态过滤, 以及按当前过滤条件一键导出 JSONL/CSV.

---

## 3. ClickHouse Schema 详解

### 3.1 主事实表 `llm_call_logs`

- **引擎**: `MergeTree`
- **分区**: `PARTITION BY toYYYYMM(created_at)` — 以月为 TTL 迁移粒度
- **排序键**: `(toStartOfHour(created_at), user_id, request_id)`
- **主键**: `(toStartOfHour(created_at), user_id)` — 同时优化时间扫描 + 用户时间线
- **TTL 三段**: `7d` 热盘 → `90d` 温盘 → `365d` 冷盘 (S3) → 删除 (见 `deploy/clickhouse/config.d/storage.xml`)

#### 索引一览

| Index | 列 | 类型 | 用途 |
| --- | --- | --- | --- |
| idx_request_text | request_text_preview | ngrambf_v1(4, 65536, 3, 0) | 请求内容全文初筛 (中英文友好) |
| idx_response_text | response_text_preview | ngrambf_v1(4, 65536, 3, 0) | 响应内容全文初筛 |
| idx_user | user_id | bloom_filter(0.01) | 按用户查询加速 |
| idx_api_key | api_key_hash | bloom_filter(0.01) | 按 api key 查询加速 |
| idx_org | organization_id | bloom_filter(0.01) | 多租户过滤 |
| idx_model | model | set(200) | 枚举过滤 |
| idx_provider | provider | set(20) | 枚举过滤 |
| idx_client_type | client_type | set(20) | 客户端类型过滤 |
| idx_error | is_error, upstream_status | minmax | 错误过滤 |
| idx_duration | duration_ms | minmax | 延时过滤 |

#### 关键列编码

| 编码 | 适用 |
| --- | --- |
| `LowCardinality(String)` | 低基数枚举 (provider / model / client_type / ...) |
| `CODEC(ZSTD(3))` | 短文本、标识 |
| `CODEC(ZSTD(6))` | `request_json` / `response_json` (长文本高压缩) |
| `CODEC(ZSTD(9))` | `response_stream_chunks` (最高压缩, 仅 debug 用) |
| `CODEC(Delta, ZSTD(3))` | 时间戳 |

### 3.2 物化视图 (MV)

| MV | 引擎 | 聚合维度 | 用途 |
| --- | --- | --- | --- |
| `llm_usage_hourly` | SummingMergeTree | hour × provider × model × user × api_key × org | 用量看板、计费、小时级监控 |
| `llm_usage_daily` | AggregatingMergeTree (含 `uniqState(user_id)`) | day × provider × model × org | 长期趋势、按日对账 |
| `llm_errors_recent` | MergeTree (TTL 30d) | — | 错误专表, 加速诊断 |

### 3.3 逻辑视图 (非物化, 仅 SQL alias)

| 视图 | 说明 |
| --- | --- |
| `v_user_recent_traces` | 用户最近 7 天的请求 |
| `v_model_top_users` | 近 30 天模型 × 用户的总用量 |
| `v_provider_health` | 各 provider 近 1 小时健康指标 |
| `v_token_cost_daily` | 按日的 token 用量 (配外部价格表计算金额) |
| `v_user_profiles` | 用户画像 (近 30 天, 按 `user_id` × `client_type` 聚合) |
| `v_claude_code_traces` | Claude Code 客户端的专属时间线 (高频查询预设) |

---

## 4. 数据流细节

### 4.1 非流式请求

1. Client `POST /v1/chat/completions` 到 Gateway
2. `Handler.ServeHTTP` 创建 `Record`, 提取 `client_ip` / `api_key_hash` / UA → `client_type`
3. `Forwarder` 读完请求 body → 转发到 newapi → 读响应
4. 响应 `io.Copy` 给 Client, 同时 `LimitedBuffer` 累积
5. `protocol.ParseRequest` + `ParseResponse` 填充 tokens / preview / error
6. `Record` 提交到 capture pipeline
7. Pipeline worker:
   - body > `LLMLOG_COS_OFFLOAD_THRESHOLD` (默认 32KB) → 上传 COS, 设 `*_cos_key`
   - body ≤ 阈值 → 内联到 `request_json` / `response_json` (CH 内 ZSTD(6))
8. 提交到 `ClickHouseSink`
9. `ClickHouseSink` 内部 buffer 满 `CH_BATCH_SIZE` (默认 500) 或 `CH_BATCH_INTERVAL` (默认 300ms) 超时, HTTP POST `JSONEachRow`
10. CH 端 `async_insert=1` 自动聚合多个 INSERT, 触发 part 合并

### 4.2 流式 SSE 请求

步骤 1-3 同非流式.

1. 检测 `Content-Type` 含 `text/event-stream`, 切换到 SSE chunk 循环:
   每读到一段 → `io.Copy` 给 Client → `flusher.Flush()` (实时推送), 同时 `LimitedBuffer` 累积.
2. 流结束后, `sse.Parse(buffer)` 按 `data:` 行切分为 events.
3. `protocol.ParseStreamResponse(events)` 重组 content / 提取 `finish_reason` / tokens.
4. 后续步骤同非流式 6 - 10.

### 4.3 CH 不可用时的兜底链路 (零数据丢失)

```text
 batch ──▶ ClickHouseSink.sendWithRetry (3 次指数退避)
            │
            ├─ 成功 ──▶ flushedBatches++
            │
            └─ 连续失败 ──▶ DiskSpool.Write(batch)  (NDJSON 落盘)
                            │
                            └─ 每 LLMLOG_DISK_SPOOL_INTERVAL (默认 30s)
                                replayer 扫描 spool → 尝试 CH 回放
                                成功删除文件, 失败保留等下轮
```

- spool 容量上限 `LLMLOG_DISK_SPOOL_MAX_BYTES` (默认 10GB), 满了丢弃最老 batch
- `ctx` 取消 (例如关闭阶段) 会立即终止重试并落盘, 避免拖慢 shutdown
- 文件名形如 `batch-YYYYMMDD-HHMMSS-<n>.ndjson`, 可人工 `jq` 查看

---

## 5. 客户端类型识别

Gateway `ClassifyClient()` 按顺序匹配 UA, 首个命中的规则生效. 枚举值:

| client_type | 典型 UA 模式 |
| --- | --- |
| `claude_code` | 前缀 `claude-cli/` |
| `anthropic_sdk` | 前缀 `Anthropic/` / `anthropic-ai/` |
| `openai_sdk` | 前缀 `OpenAI/` / `openai-python/` |
| `langchain` | 前缀 `langchain/` |
| `cherry_studio` | 前缀 `CherryStudio/` |
| `chatbox` | 前缀 `chatbox/` |
| `lobechat` | 前缀 `LobeChat/` |
| `cursor` | 前缀 `Cursor/` |
| `continue` | 前缀 `Continue/` |
| `browser` | 含 `chrome` / `firefox` / `safari` / `edge` |
| `curl` | 前缀 `curl/` / `Postman/` / `wget/` |
| `unknown` | 其他 |

新增规则只需编辑 `gateway/internal/handlers/client_classify.go` 的 `ClassifyClient`
函数并重启 gateway. 老数据不回填, 只影响新请求.

---

## 6. 性能与扩展

### 6.1 单机性能预期

| 组件 | 能力 |
| --- | --- |
| Gateway 单实例 | 10,000+ QPS (取决于 body 大小) |
| ClickHouse 单节点 | 写入 100K+ rows/s, 热数据查询亚秒级 |
| Query API 单实例 | 1,000+ QPS (主要受 CH 查询性能制约) |

### 6.2 水平扩展路径

1. **Gateway 多实例**: 无状态, K8s replicas + L4 负载均衡即可
2. **ClickHouse 分片集群**: 主表加 `Distributed` 引擎, 按 `cityHash64(user_id)` 分片
3. **管道演进** (按需):
   - 现状: 代理直写 CH async_insert + 磁盘兜底 (零中间件)
   - 可选 Vector/Fluent Bit: 代理 → NDJSON → agent → CH (进一步解耦)
   - 可选 Kafka: 代理 → Kafka → CH Kafka Engine (十万级 QPS 及以上)

---

## 7. 安全与合规

- **api_key 不存明文**: 仅存 SHA-256 hash + 前缀 (如 `sk-abcd...`)
- **Query API 鉴权**: Bearer Token, 生产环境必改 `LLMLOG_API_TOKEN`
- **Token 不入 URL**: 导出端点只接受 Authorization header, 避免进 nginx access log
- **传输**: 生产建议 Gateway 与 Admin UI 前置 TLS (Nginx / Caddy 终止)
- **COS 访问**: 用最小权限 IAM, 只授 `PutObject` / `GetObject`
- **多租户**: 通过 `api_key_hash` / `organization_id` 软隔离, 无数据库级 RLS
- **UA 截断**: UTF-8 安全回退, 避免中文 / emoji 浏览器 UA 破坏 CH LowCardinality

---

## 8. 故障与降级矩阵

| 故障场景 | Gateway 行为 |
| --- | --- |
| CH 写入失败 | 重试 3 次 (200ms / 600ms / 1.8s) → 失败则落盘 spool → replayer 后续回放 |
| CH 长时间不可用 | spool 文件堆积, 达到 `DISK_SPOOL_MAX_BYTES` 后丢弃最老批次并计入 `failed_records` 指标 |
| COS 上传失败 | 退化到内联 `request_json` / `response_json` (确保数据不丢) |
| 采集队列满 | 主链路不阻塞, 丢弃记录并计入 `dropped` 指标 |
| 上游 newapi 不可达 | 直接返回 502 给客户端, 仍记录失败 trace (is_error=1) |
| 客户端主动断连 | 立即停止读上游, 记录已收到的部分 |
| 关闭阶段 (SIGTERM) | ctx 取消后剩余 batch 直接落盘, 下次启动 replayer 回放 |

---

## 9. 假设与限制

- **认证透传**: 假设客户端把 `base_url` 指向 Gateway, `api_key` 原样传递 (Gateway 不改 header).
- **多租户**: 仅通过 `api_key_hash` / `organization_id` 做软隔离, 不做 SQL RLS.
- **采样**: 默认 100% 采集; 高 QPS 场景可在 Gateway 加采样开关.
- **流式响应大小**: 单流式响应受 `LLMLOG_MAX_BODY_BYTES` 限制 (默认 100MB).
- **ClickHouse 版本**: 需 24.8+ (依赖新 analyzer 的 alias 语义).
- **Python 版本**: Query API 需 3.11+ (使用了 `|` 联合类型和 `from __future__ import annotations`).
