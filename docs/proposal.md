# LLM Gateway 上下文采集系统 — Proposed Solution

> 研究阶段与方案探索阶段成果记录 (RIPER-5 RESEARCH + INNOVATE)
> 此文档为设计阶段产出, 实际实现见仓库代码.

---

## 1. 背景与需求

用户自部署了一个 `newapi` 作为 LLM 中转网关 (聚合转发到 OpenAI / Anthropic / Gemini / Deepseek 等上游),
但 `newapi` 本身**不存储请求的完整上下文** (请求 body、messages 原文、完整响应 body).
需要在其前方架设一套独立的**请求监听 + 上下文归档系统**, 记录所有 LLM 调用的完整上下文, 用于:

1. 按 `user` / `api_key` 重建历史对话时间线
2. 按 `model` / `channel` 聚合用量和成本
3. 全文检索 `prompt` / `response` 内容 (合规审查用途)

预期数据规模: **千万至上亿级别**, 平均上下文长度较长.

---

## 2. 监听范围 (LLM 接口清单)

基于 `newapi` 官方 docs 确认, 共 4 个核心 LLM 入口:

| 协议 | HTTP 方法 + 路径 | 流式 | 说明 |
|------|----------------|-----|------|
| OpenAI Chat | `POST /v1/chat/completions` | SSE | 最通用, Deepseek / Qwen / Moonshot / Zhipu 等均走此路径 |
| OpenAI Responses | `POST /v1/responses` | SSE | OpenAI 新一代 stateful API, 基于 Items |
| Anthropic Messages | `POST /v1/messages` | SSE | Claude 原生协议, 支持 tool_use / 多模态 content blocks |
| Gemini generateContent | `POST /v1beta/models/{model}:generateContent` 和 `:streamGenerateContent` | SSE | Google 原生协议, 结构差异显著 |

**不在本期范围**: OpenAI Realtime (WebSocket) / Embeddings / Rerank / Image / Audio / Music / Video.

---

## 3. 关键决策记录 (Decision Log)

| # | 决策项 | 选择 | 替代方案被放弃的原因 |
|---|-------|------|-------------------|
| 1 | 拦截点 | **反向代理前置层** | Nginx mirror 无法关联请求响应 + SSE 流难采; newapi 内嵌需改 Go 源码污染上游; SDK 层无法覆盖第三方客户端 |
| 2 | 主存储 | **ClickHouse (元数据) + COS (原文 body)** | MySQL 分库分表亿级聚合慢; MongoDB 聚合弱于 CH; ES 成本最高 |
| 3 | 项目形态 | **完全独立新仓库** | 避免拖累 PrismAI 主业务进程, 可独立部署扩缩容 |
| 4 | 监听协议 | **4 种全覆盖** | 遗漏任一都会导致采集不完整 |
| 5 | 目标规模 | **按最大规模设计预留扩展性** | 初期单节点跑, 预留水平扩展点 |
| 6 | 查询场景 | **用户时间线 + 模型聚合 + 全文检索** | 三场景并重, 决定 CH 主键 + 物化视图 + skip index 组合设计 |
| 7 | PrismAI 接入 | **不接入, 专注 newapi** | 范围最小, 避免冲击现有业务 |
| 8 | 代理语言 | **Go** | 原生 SSE + 单二进制 + 单机 10万+ QPS + 代理生态成熟 |
| 9 | 数据管道 | **代理直写 CH async_insert** (起步) | 零中间件, 简单; 后期可演进到 Vector/Kafka 不动代理 |
| 10 | 归一化策略 | **抽取关键字段 + 保留原始 JSON** (ZSTD) | 零信息损失, 协议演化免迁移 |
| 11 | 查询层 | **Python FastAPI + React 管理后台** | 与 PrismAI 同栈, 可复用经验 |

---

## 4. 八大技术挑战与应对

| # | 挑战 | 实现选择 |
|---|------|---------|
| 1 | SSE 流式不阻塞 | `io.TeeReader` 边流边采; 完整响应重组下沉到 capture pipeline 异步 worker |
| 2 | 大 body OOM 风险 | `MaxBodyBytes` 上限 + `LimitedBuffer` 静默截断 |
| 3 | 4 协议异构归一 | "抽取关键字段 + 保留原始 JSON" (ZSTD), 各协议独立 parser |
| 4 | 亿级 CH 主键选择 | 主表 `(toStartOfHour(created_at), user_id, request_id)` + 物化视图独立优化聚合 |
| 5 | COS list 爆炸 | 时间分层目录 `yyyy/mm/dd/hh/{request_id}-{kind}.json.zst` |
| 6 | 代理性能瓶颈 | Go std `net/http` (无重型框架) + zero-copy tee + async 批量写 |
| 7 | 中文全文检索 | `ngrambf_v1(4)` 4-gram bloom filter |
| 8 | 采集链路可靠性 | 代理直写 CH async_insert + 3 次重试; COS 失败退化到内联存储 |

---

## 5. 实际实现位置

| 概念 | 实现文件 |
|------|---------|
| Gateway 入口 | `gateway/cmd/gateway/main.go` |
| 配置 | `gateway/internal/config/config.go` |
| 反向代理 | `gateway/internal/proxy/forwarder.go` |
| 限大小 buffer | `gateway/internal/proxy/buffer.go` |
| SSE 解析 | `gateway/internal/sse/parser.go` |
| 协议解析 | `gateway/internal/protocol/{openai,openai_responses,anthropic,gemini}.go` |
| 4 个 handler | `gateway/internal/handlers/handlers.go` |
| 路由注册 | `gateway/internal/server/server.go` |
| Capture pipeline | `gateway/internal/capture/{record,pipeline}.go` |
| ClickHouse sink | `gateway/internal/sink/clickhouse.go` |
| COS sink | `gateway/internal/sink/cos.go` |
| CH schema | `migrations/clickhouse/00*.sql` |
| 三级存储策略 | `deploy/clickhouse/config.d/storage.xml` |
| async_insert 配置 | `deploy/clickhouse/users.d/async_insert.xml` |
| Query API 入口 | `query-api/app/main.py` |
| Query 服务层 | `query-api/app/services/{trace,stats,search}_service.py` |
| Query routers | `query-api/app/routers/*.py` |

---

## 6. 后续路线图 (实现外)

- **Phase 1 完成**: scaffold + Gateway 4 协议 + Sink + CH + COS + Query API
- **Phase 2** (TODO): Admin UI (React + shadcn/ui)
- **Phase 3** (TODO): Prometheus 业务指标 + Grafana dashboard 模板
- **Phase 4** (TODO): 本地 NDJSON 兜底 + 死信队列回放工具
- **Phase 5** (TODO): K8s Helm chart + CI/CD pipeline
- **Phase 6** (TODO): 嵌入向量字段 + 语义检索

---

## 7. 假设与限制

- **认证透传**: 假设客户端把 newapi base_url 改为 Gateway, 把 api_key 原样传 (Gateway 不改 header)
- **多租户**: 仅通过 `api_key_hash` / `organization_id` 列做软隔离, 没有数据库级别 RLS
- **采样**: 当前所有请求 100% 采集, 高 QPS 场景应在 Gateway 加采样配置
- **流式响应大小**: 单个流式响应受 `LLMLOG_MAX_BODY_BYTES` 限制 (默认 100MB)
