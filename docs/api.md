# Query API 参考

> 完整 OpenAPI 3.1 文档在服务运行后可访问 `http://<query-api-host>:8000/docs`.
> 本文档是对所有端点的紧凑索引.

Query API 是 Python FastAPI 服务, 屏蔽 ClickHouse + COS 的细节, 对外只暴露 REST.

---

## 1. 鉴权

所有 `/api/v1/*` 端点都要求 HTTP header:

```
Authorization: Bearer <LLMLOG_API_TOKEN>
```

Token 值取自服务启动时的环境变量 `LLMLOG_API_TOKEN`. 不带或错误的 token 返回
`401 Unauthorized`.

`/healthz` `/readyz` `/docs` `/openapi.json` 不需要鉴权.

```bash
export TOKEN=$(grep LLMLOG_API_TOKEN .env | cut -d= -f2)

curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8000/api/v1/traces?limit=5
```

---

## 2. 端点

### 2.1 `GET /api/v1/traces`

列出请求 trace, 支持多维度过滤.

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `provider` | string | - | `openai_chat` \| `openai_responses` \| `anthropic` \| `gemini` |
| `model` | string | - | 精确匹配 |
| `user_id` | string | - | 业务用户 ID |
| `api_key_hash` | string | - | api_key 的 SHA-256 hex |
| `api_key_prefix` | string | - | api_key 前缀 (显示用) |
| `client_type` | string | - | 见 §3 客户端类型枚举 |
| `is_error` | bool | - | 仅看错误 / 仅看成功 |
| `start` | datetime ISO8601 | - | 起始时间 |
| `end` | datetime ISO8601 | - | 结束时间 |
| `limit` | int | 50 | 1-1000 |
| `offset` | int | 0 | 分页偏移 |

**Response**: `TraceListResponse` (`{items, total, limit, offset}`)

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/traces?provider=anthropic&client_type=claude_code&limit=20"
```

---

### 2.2 `GET /api/v1/traces/{request_id}`

获取单个 trace 完整详情.

**Query parameters**:

- `include_raw=true` 同时拉取 COS 中的原始 request/response body (默认 false)

**Response**: `TraceDetail`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/traces/01H..AB?include_raw=true"
```

---

### 2.3 `GET /api/v1/users`

用户画像列表, 按 `user_id` × `client_type` 聚合最近 N 天的 token 消耗、请求数、错误率等.

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `client_type` | string | - | 按客户端类型过滤 |
| `min_requests` | int | 0 | 最少请求数过滤 |
| `days` | int | 30 | 回溯天数, 1-365 |
| `order_by` | string | `total_tokens` | `total_tokens` \| `total_requests` \| `last_seen` |
| `limit` | int | 100 | 1-1000 |
| `offset` | int | 0 | 分页偏移 |

**Response**: `UserListResponse`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/users?client_type=claude_code&days=7&limit=20"
```

单行示例:

```json
{
  "user_id": "alice@example.com",
  "client_type": "claude_code",
  "api_key_prefix": "sk-ant...abc",
  "user_agent": "claude-cli/0.2.17",
  "total_requests": 523,
  "total_errors": 3,
  "prompt_tokens": 1234567,
  "completion_tokens": 987654,
  "total_tokens": 2222221,
  "cached_tokens": 100000,
  "reasoning_tokens": 0,
  "request_bytes": 524288,
  "response_bytes": 1048576,
  "models_used": ["claude-3-5-sonnet-20241022"],
  "providers_used": ["anthropic"],
  "first_seen": "2026-04-15T10:23:01Z",
  "last_seen": "2026-04-21T09:12:33Z",
  "avg_duration_ms": 823.4
}
```

---

### 2.4 `GET /api/v1/users/{user_id}`

单个用户画像详情 (聚合值).

| 参数 | 类型 | 默认 |
| --- | --- | --- |
| `days` | int | 30 |

**Response**: `UserProfile` (与 2.3 `items[]` 结构相同, 单对象返回)

---

### 2.5 `GET /api/v1/users/{user_id}/traces`

某用户的请求时间线.

| 参数 | 类型 | 默认 |
| --- | --- | --- |
| `limit` | int | 50 |
| `offset` | int | 0 |
| `days` | int | 30 (回溯天数 1-365) |

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/users/u_12345/traces?days=7"
```

---

### 2.6 `GET /api/v1/stats/usage`

按时间桶 + provider + model 聚合用量, 走物化视图, 亚秒级返回.

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `group_by` | string | `day` | `day` \| `hour` |
| `start` | datetime | `end - 30d` | 起始 |
| `end` | datetime | `now` | 结束 |
| `provider` | string | - | 过滤 |
| `model` | string | - | 过滤 |
| `organization_id` | string | - | 多租户场景过滤 |

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/stats/usage?group_by=day&start=2026-04-01T00:00:00Z"
```

---

### 2.7 `GET /api/v1/stats/health`

近 N 小时各 provider 的实时健康指标.

| 参数 | 类型 | 默认 |
| --- | --- | --- |
| `window_hours` | int | 1 (1-24) |

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/stats/health?window_hours=6"
```

返回示例:

```json
{
  "window": "6h",
  "items": [
    {
      "provider": "openai_chat",
      "requests": 12345,
      "errors": 23,
      "error_rate_pct": 0.19,
      "avg_latency_ms": 1234.5,
      "avg_ttft_ms": 245.1,
      "tokens": 9876543
    }
  ]
}
```

---

### 2.8 `GET /api/v1/search`

全文初筛 prompt / response. 走 CH `ngrambf_v1` skip index 加速,
建议 query 长度 >= 4 字符 (3-gram 单字的误匹配率高).

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `q` | string | 必填 | 检索关键词 (>= 2 字符) |
| `field` | string | `request` | `request` \| `response` |
| `start` | datetime | - | 时间范围 |
| `end` | datetime | - | 时间范围 |
| `provider` | string | - | 过滤 |
| `client_type` | string | - | 过滤 |
| `user_id` | string | - | 过滤 |
| `limit` | int | 100 | 1-1000 |

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/search?q=API+key&field=request&limit=20"
```

---

### 2.9 `GET /api/v1/errors`

近 30 天错误日志. 优先查 `llm_errors_recent` 物化视图;
当传入 `client_type` (专表不含该字段) 或专表不存在时, 自动回退到主表 `is_error=1` 全表查询.

| 参数 | 类型 | 默认 |
| --- | --- | --- |
| `start` | datetime | - |
| `end` | datetime | - |
| `provider` | string | - |
| `client_type` | string | - |
| `upstream_status` | int | - |
| `limit` | int | 100 |
| `offset` | int | 0 |

---

### 2.10 `GET /api/v1/export/traces`

流式导出 trace 为 JSONL 或 CSV. 按 ClickHouse `query_iter` 分块 (每 5000 行一 chunk),
全程不占大内存, 支持千万行级导出.

| 参数 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `format` | string | `jsonl` | `jsonl` \| `csv` |
| `include_body` | bool | false | 是否包含 `request_json`/`response_json` 原文 (体积大) |
| `max_rows` | int | 100000 | 最大行数, 1-10000000 |
| `provider` | string | - | 过滤 |
| `client_type` | string | - | 过滤 |
| `user_id` | string | - | 过滤 |
| `api_key_prefix` | string | - | 过滤 |
| `is_error` | bool | - | 过滤 |
| `start` | datetime | - | 过滤 |
| `end` | datetime | - | 过滤 |

**Response**: `StreamingResponse` (`Content-Disposition: attachment; filename=traces-YYYYMMDD-HHMMSS.{jsonl|csv}`)

```bash
# 导出某用户近 7 天的完整对话 (含原文)
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/export/traces?user_id=alice@example.com&include_body=true&max_rows=10000" \
  -o alice-traces.jsonl

# 按 Claude Code 客户端导出 CSV (仅元数据, 适合 Excel)
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/export/traces?client_type=claude_code&format=csv&max_rows=50000" \
  -o claude-code-stats.csv
```

**注意**: 不支持通过 URL 里的 `_token` 参数鉴权 (Token 会进入 nginx access log),
必须走 `Authorization` header.

---

### 2.11 `GET /healthz` / `GET /readyz`

- `/healthz`: 服务存活检查, 无依赖. 返回 `{"status": "ok"}`.
- `/readyz`: 就绪检查 (含 ClickHouse 连通性).

```json
{"status": "ready", "clickhouse": true}
```

---

## 3. 客户端类型枚举

`client_type` 字段由 Gateway 根据 `User-Agent` 自动识别. 取值与
`gateway/internal/handlers/client_classify.go` 的 `ClassifyClient` 函数保持同步.

| 值 | 说明 | 典型 UA 前缀 |
| --- | --- | --- |
| `claude_code` | Anthropic 官方 Claude Code CLI | `claude-cli/` |
| `anthropic_sdk` | anthropic-sdk-python / anthropic-sdk-typescript | `Anthropic/`, `anthropic-ai/` |
| `openai_sdk` | openai-python / openai-node | `OpenAI/`, `openai-python/` |
| `langchain` | LangChain / LangChain-Community | `langchain/` |
| `cherry_studio` | Cherry Studio 桌面客户端 | `CherryStudio/` |
| `chatbox` | Chatbox 桌面客户端 | `chatbox/` |
| `lobechat` | LobeChat Web | `LobeChat/` |
| `cursor` | Cursor IDE | `Cursor/` |
| `continue` | Continue.dev | `Continue/` |
| `browser` | 通用浏览器 | 含 `chrome` / `firefox` / `safari` / `edge` |
| `curl` | curl / Postman / 命令行工具 | `curl/`, `Postman/`, `wget/` |
| `unknown` | 未识别 | 其他 |

新增识别规则: 编辑 `gateway/internal/handlers/client_classify.go` 的 `ClassifyClient()`
函数并重启 gateway 即可生效. 历史数据不回填, 只影响新请求.

---

## 4. 错误码

| HTTP | 含义 |
| --- | --- |
| 200 | 成功 |
| 400 | 参数无效 (如 limit 超过最大值) |
| 401 | 缺少 / 格式错误的 Authorization header |
| 403 | token 无效 |
| 404 | trace / user 不存在 |
| 500 | 内部错误 (查日志) |
| 503 | 服务未就绪 (ClickHouse 不可达) |

---

## 5. 客户端示例

### 5.1 Python

```python
import httpx

client = httpx.Client(
    base_url="http://localhost:8000",
    headers={"Authorization": f"Bearer {TOKEN}"},
    timeout=30.0,
)

# 列出最近 100 个 trace
resp = client.get("/api/v1/traces", params={"limit": 100})
for t in resp.json()["items"]:
    print(t["request_id"], t["model"], t["total_tokens"])

# 拉取某个 trace 完整原文
detail = client.get("/api/v1/traces/01H..AB", params={"include_raw": "true"}).json()
print(detail["request_json"])

# 导出某用户近 30 天的完整对话到本地
with client.stream(
    "GET",
    "/api/v1/export/traces",
    params={"user_id": "alice@example.com", "include_body": "true", "max_rows": 50000},
) as r:
    with open("alice.jsonl", "wb") as f:
        for chunk in r.iter_bytes():
            f.write(chunk)
```

### 5.2 TypeScript

```typescript
const headers = { Authorization: `Bearer ${TOKEN}` }

const usage = await fetch(
  `http://localhost:8000/api/v1/stats/usage?group_by=hour&provider=anthropic`,
  { headers },
).then((r) => r.json())

const users = await fetch(
  `http://localhost:8000/api/v1/users?order_by=total_tokens&days=7&limit=20`,
  { headers },
).then((r) => r.json())
```

### 5.3 Shell (jq)

```bash
TOKEN=$(grep LLMLOG_API_TOKEN .env | cut -d= -f2)
BASE=http://localhost:8000

# 过去 1 小时各 provider 的错误率
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/stats/health?window_hours=1" \
  | jq '.items | map({provider, error_rate_pct, avg_latency_ms})'

# Top 10 token 消耗用户
curl -s -H "Authorization: Bearer $TOKEN" "$BASE/api/v1/users?order_by=total_tokens&limit=10" \
  | jq '.items | map({user_id, client_type, total_tokens})'
```
