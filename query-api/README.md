# LLM Logger - Query API

查询服务,对外提供 REST API 访问 ClickHouse 中采集的 LLM 调用元数据和 COS 中的原始 body.

## 启动

```bash
# 安装依赖 (推荐 uv)
uv sync

# 开发模式
uv run uvicorn app.main:app --reload --host 0.0.0.0 --port 8000

# 或用 python
python -m uvicorn app.main:app --reload
```

打开 <http://localhost:8000/docs> 查看 OpenAPI 文档.

## 鉴权

所有 `/api/v1/*` 端点要求 `Authorization: Bearer <LLMLOG_API_TOKEN>`.
`/healthz` `/readyz` `/docs` `/openapi.json` 不需要鉴权.

## 主要端点

| 端点 | 说明 |
|------|------|
| `GET /api/v1/traces` | 列出请求 trace, 支持多维度过滤 |
| `GET /api/v1/traces/{request_id}` | 单 trace 详情 (可选拉取 COS 原文) |
| `GET /api/v1/users/{user_id}/traces` | 按用户的时间线 |
| `GET /api/v1/stats/usage` | 用量聚合 (走物化视图) |
| `GET /api/v1/stats/health` | provider 实时健康指标 |
| `GET /api/v1/search` | 全文初筛 prompt/response |
| `GET /api/v1/errors` | 近期错误日志 |

## 配置

环境变量 (前缀 `LLMLOG_`) 全集见仓库根目录 `.env.example`.
