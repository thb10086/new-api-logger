# 架构设计

## 总览

LLM Logger 是一个**反向代理 + 上下文归档**系统,挡在 newapi 前面,
透明转发 LLM 请求的同时**完整捕获请求/响应上下文**到列存数据库 + 对象存储,
为千万至亿级数据量的查询/统计/审计提供基础.

```text
┌─────────┐     HTTPS    ┌────────────┐    HTTPS    ┌────────┐    HTTPS   ┌──────────┐
│ Clients │ ───────────▶ │  Gateway   │ ──────────▶ │ newapi │ ─────────▶ │ Upstream │
│ (任意   │              │  (Go)      │             │        │            │ (OpenAI/ │
│  SDK)   │ ◀─────────── │            │ ◀────────── │        │ ◀────────  │ Claude/  │
└─────────┘              └─────┬──────┘             └────────┘            │ Gemini)  │
                               │ tee                                       └──────────┘
                  ┌────────────┼─────────────┐
                  │            │             │
                  ▼            ▼             ▼
            ┌──────────┐ ┌──────────┐ ┌─────────────┐
            │ClickHouse│ │   COS    │ │ (兜底文件,   │
            │ (元数据 +│ │ (大 body, │ │ 可选)       │
            │ 短 body) │ │ zstd)    │ └─────────────┘
            └────┬─────┘ └────┬─────┘
                 │            │
                 └─────┬──────┘
                       ▼
              ┌─────────────────┐
              │ Query API + UI  │
              └─────────────────┘
```

## 组件职责

### Gateway (Go)

- **协议无关的反向代理** (`internal/proxy/forwarder.go`): 不依赖 `httputil.ReverseProxy`,自己实现以精确控制 SSE tee.
- **4 协议解析器** (`internal/protocol/`): OpenAI Chat / OpenAI Responses / Anthropic / Gemini, 各自实现 `ParseRequest` / `ParseResponse` / `ParseStreamResponse`.
- **采集 pipeline** (`internal/capture/pipeline.go`): 异步 worker 池, 大 body offload 到 COS, 元数据写 CH.
- **Sink** (`internal/sink/`): ClickHouse async_insert HTTP 直写 + S3 兼容上传.

### ClickHouse Schema

#### 主事实表 `llm_call_logs`

- 引擎: `MergeTree`
- 分区: `PARTITION BY toYYYYMM(created_at)`
- 排序: `ORDER BY (toStartOfHour(created_at), user_id, request_id)`
- 主键: `(toStartOfHour(created_at), user_id)` — 同时优化时间扫描和用户检索
- TTL 三段: `7d` 热盘 → `90d` 温盘 → `365d` 冷盘 (S3) → 删除
- 索引:
  - `ngrambf_v1(4)` on `request_text_preview` / `response_text_preview` (中文友好全文初筛)
  - `bloom_filter` on `user_id` / `api_key_hash` / `organization_id`
  - `set` on `model` / `provider`
  - `minmax` on `is_error,upstream_status` / `duration_ms`
- 关键列编码:
  - `LowCardinality(String)` for enum 类字段
  - `CODEC(ZSTD(3))` for 短文本
  - `CODEC(ZSTD(6))` for `request_json` / `response_json` (高压缩长文本)
  - `CODEC(Delta, ZSTD(3))` for 时间戳

#### 物化视图

- `llm_usage_hourly` (SummingMergeTree): 按 `(hour, provider, model, user_id, api_key_hash, organization_id)` 聚合 token / 字节 / 计数.
- `llm_usage_daily` (AggregatingMergeTree): 按日聚合, 含 `uniqState(user_id)`.
- `llm_errors_recent` (MergeTree, TTL 30d): 错误专表加速诊断.

#### 逻辑视图 (无物化, 仅 SQL alias)

- `v_user_recent_traces` 用户最近 7 天的请求
- `v_model_top_users` 模型 × 用户的总用量
- `v_provider_health` provider 实时健康指标
- `v_token_cost_daily` 按日 token 用量

### COS / S3

- **路径**: `{prefix}/{yyyy}/{mm}/{dd}/{hh}/{request_id}-{request|response}.json.zst`
- **压缩**: zstd level 3 (LLM 上下文压缩比 4-8x)
- **生命周期** (生产建议在 COS 控制台配置):
  - 0-90d: 标准存储
  - 91-365d: 低频/归档存储
  - >365d: 删除

### Query API (Python FastAPI)

- 屏蔽 CH + COS, 提供统一 REST 接口
- Bearer Token 鉴权
- 端点详见 `docs/api.md`

## 数据流细节

### 一次非流式请求

1. Client `POST /v1/chat/completions` 到 Gateway
2. Handler 创建 `Record`, 提取 client IP / api_key hash
3. Forwarder 读完请求 body → 转发到 newapi → 读响应
4. 响应直接 io.Copy 给 Client
5. 同时 `LimitedBuffer` 收完整响应
6. Handler 调用 `OpenAIChat.ParseRequest` + `ParseResponse` 填充 token / preview / error
7. `Record` 提交到 capture pipeline
8. Pipeline worker:
   - body > 32KB → upload COS, 设置 `request_cos_key` / `response_cos_key`
   - body 较小 → 直接内联到 `request_json` / `response_json` (CH 内 ZSTD)
9. 提交到 `ClickHouseSink`
10. `ClickHouseSink` 内部 buffer 满 200 条或 500ms 超时 → HTTP POST `JSONEachRow` 到 CH
11. CH 端 `async_insert=1` 自动聚合多个 INSERT, 触发 part 合并

### 一次流式 SSE 请求

1-3. 同上
4. 检测 Content-Type 含 `text/event-stream`, 切换到 SSE chunk 循环:
   - 每读到一段 → io.Copy 给 Client → flusher.Flush() (实时推送)
   - 同时 LimitedBuffer 累积
5. 流结束后, `sse.Parse(buffer)` 切分为 events
6. `parser.ParseStreamResponse(events)` 重组 content / 提取 finish_reason / tokens
7. 后续同非流式步骤 6-11

## 性能与扩展

### 单机性能预期

- Gateway: 单实例 10,000+ QPS (取决于 body 大小)
- ClickHouse 单节点: 写入 100K+ rows/s, 查询亚秒级 (热数据)
- Query API: 1,000+ QPS (受 CH 查询性能制约)

### 水平扩展路径

1. **Gateway 多实例**: 无状态, 直接 K8s replica + L4 负载均衡
2. **ClickHouse 分片集群**: 主表加 `Distributed` 引擎, 按 `cityHash64(user_id)` 分片
3. **采集管道演进**:
   - 现状: 代理直写 CH async_insert (零中间件)
   - 进阶: 代理 → 本地 NDJSON → Vector → CH (生产环境鲁棒性更强)
   - 高阶: 代理 → Kafka → CH Kafka Engine (亿级 QPS)

## 安全与合规

- **api_key 不存明文**: 仅存 SHA-256 hash + 前后缀 (`sk-abcd...wxyz`)
- **Query API 鉴权**: Bearer Token, 生产环境必改 `LLMLOG_API_TOKEN`
- **传输**: 生产建议 Gateway 前置 TLS (Nginx/Caddy 终止)
- **COS 访问**: 用最小权限 IAM, 只授 PutObject / GetObject

## 故障与降级

| 故障 | Gateway 行为 |
|------|------------|
| CH 写入失败 | 重试 3 次,失败计入 `failed_records` 指标,数据丢失 (TODO: 本地兜底文件) |
| COS 上传失败 | 退化到内联存储 `request_json` / `response_json` (确保数据不丢) |
| 采集队列满 | 主链路不阻塞,丢弃记录并计入 `dropped` 指标 |
| 上游 newapi 不可达 | 直接返回 502 给客户端, 仍记录失败 trace |
| 客户端断连 | 立即停止读上游, 记录已收到的部分 |

## 未来演进方向

1. **Kafka 缓冲**: 流量增长后, 用 Kafka 做持久化缓冲, 解耦 Gateway 和 CH
2. **Vector/Fluent Bit**: 用 NDJSON 文件 + agent tail 替代直写
3. **嵌入向量检索**: 给 prompt 计算 embedding 存 CH Vector 列, 支持语义搜索 bad case
4. **采样**: 高 QPS 场景对成功请求按比例采样 (1/10), 错误请求 100% 采集
5. **DLQ (死信队列)**: 失败的 CH 写入落本地 NDJSON, 由 ops 工具回放
