# Query API 参考

> 完整 OpenAPI 3.1 文档: 启动 Query API 后访问 `http://localhost:8000/docs`

---

## 鉴权

所有 `/api/v1/*` 端点要求 `Authorization: Bearer <LLMLOG_API_TOKEN>`.

```bash
export TOKEN=dev-token-change-me

curl -H "Authorization: Bearer $TOKEN" http://localhost:8000/api/v1/traces?limit=5
```

`/healthz` `/readyz` `/docs` `/openapi.json` 不需要鉴权.

---

## 端点

### `GET /api/v1/traces`

列出请求 trace, 支持多维度过滤.

**Query parameters**:

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| provider | string | - | openai_chat / openai_responses / anthropic / gemini |
| model | string | - | 精确匹配 |
| user_id | string | - | 业务用户 ID |
| api_key_hash | string | - | api_key 的 SHA-256 hex |
| is_error | bool | - | 仅看错误 / 仅看成功 |
| start | datetime ISO8601 | - | 起始时间 |
| end | datetime ISO8601 | - | 结束时间 |
| limit | int | 50 | 1-1000 |
| offset | int | 0 | 分页偏移 |

**Response**: `TraceListResponse` (含 items + total + limit + offset)

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/traces?provider=anthropic&limit=20&start=2026-04-01T00:00:00Z"
```

---

### `GET /api/v1/traces/{request_id}`

获取单个 trace 完整详情.

**Query parameters**:

- `include_raw=true` 同时拉取 COS 中的原始 request/response body (默认 false)

**Response**: `TraceDetail`

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/traces/01H..AB?include_raw=true"
```

---

### `GET /api/v1/users/{user_id}/traces`

某用户的请求时间线.

**Query parameters**:

| 参数 | 类型 | 默认 |
|------|------|------|
| limit | int | 50 |
| offset | int | 0 |
| days | int | 30 (回溯天数 1-365) |

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/users/u_12345/traces?days=7"
```

---

### `GET /api/v1/stats/usage`

按时间桶 + provider + model 聚合用量, 走物化视图,亚秒级返回.

**Query parameters**:

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| group_by | string | day | day \| hour |
| start | datetime | end-30d | 起始 |
| end | datetime | now | 结束 |
| provider | string | - | 过滤 |
| model | string | - | 过滤 |
| organization_id | string | - | 多租户场景过滤 |

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/stats/usage?group_by=day&start=2026-04-01T00:00:00Z"
```

---

### `GET /api/v1/stats/health`

近 N 小时的 provider 实时健康指标.

**Query parameters**:

- `window_hours` int 默认 1 (1-24)

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/stats/health?window_hours=6"
```

返回:

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

### `GET /api/v1/search`

全文初筛 prompt/response. 走 CH `ngrambf_v1` skip index 加速,
但仍可能误匹配 (LIKE 做精确过滤).

**Query parameters**:

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| q | string | - | 检索关键词 (>= 2 字符, 建议 >= 4) |
| field | string | request | request \| response |
| start | datetime | - | 时间范围 |
| end | datetime | - | 时间范围 |
| provider | string | - | 过滤 |
| limit | int | 100 | 1-1000 |

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8000/api/v1/search?q=API+key&field=request&limit=20"
```

---

### `GET /api/v1/errors`

近 30 天错误日志 (走 `llm_errors_recent` 物化视图).

**Query parameters**:

| 参数 | 类型 | 默认 |
|------|------|------|
| start | datetime | - |
| end | datetime | - |
| provider | string | - |
| upstream_status | int | - |
| limit | int | 100 |
| offset | int | 0 |

---

### `GET /healthz`

服务存活检查. 返回 `{"status": "ok"}`.

### `GET /readyz`

就绪检查 (含 ClickHouse 连通性).

```json
{"status": "ready", "clickhouse": true}
```

---

## 错误码

| HTTP | 含义 |
|------|------|
| 200 | 成功 |
| 400 | 参数无效 (如 limit 超过最大值) |
| 401 | 缺少 Authorization header |
| 403 | token 无效 |
| 404 | trace 不存在 |
| 500 | 内部错误 (查 logger 日志) |
| 503 | 服务未就绪 (CH 不可达) |

---

## 客户端示例

### Python

```python
import httpx

client = httpx.Client(
    base_url="http://localhost:8000",
    headers={"Authorization": "Bearer dev-token-change-me"},
    timeout=30.0,
)

# 列出最近 100 个 trace
resp = client.get("/api/v1/traces", params={"limit": 100})
for t in resp.json()["items"]:
    print(t["request_id"], t["model"], t["total_tokens"])

# 拉取某个 trace 完整原文
detail = client.get("/api/v1/traces/01H..AB", params={"include_raw": "true"}).json()
print(detail["request_json"])
```

### TypeScript

```typescript
const r = await fetch(`http://localhost:8000/api/v1/stats/usage?group_by=hour`, {
  headers: { Authorization: `Bearer ${TOKEN}` },
});
const stats = await r.json();
```
