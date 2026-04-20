"""全文检索 + 错误查询服务."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from app.database.clickhouse import ClickHouseClient


async def full_text_search(
    ch: ClickHouseClient,
    *,
    query: str,
    field: str = "request",  # request | response
    start: datetime | None = None,
    end: datetime | None = None,
    provider: str | None = None,
    limit: int = 100,
) -> list[dict[str, Any]]:
    """全文初筛 prompt/response. 走 CH ngrambf_v1 skip index 加速.

    注意:
      - bloom filter 是初筛, 仍可能误匹配, LIKE 做精确过滤
      - 中文场景效果一般, 建议 query 长度 >= 4 字符
    """
    if field not in ("request", "response"):
        raise ValueError("field 必须为 'request' 或 'response'")

    col = "request_text_preview" if field == "request" else "response_text_preview"

    where = [f"{col} LIKE %(pattern)s"]
    params: dict[str, Any] = {"pattern": f"%{query}%", "limit": limit}
    if start:
        where.append("created_at >= %(start)s")
        params["start"] = start
    if end:
        where.append("created_at < %(end)s")
        params["end"] = end
    if provider:
        where.append("provider = %(provider)s")
        params["provider"] = provider

    where_clause = " WHERE " + " AND ".join(where)

    sql = f"""
        SELECT
            request_id,
            created_at,
            provider,
            model,
            user_id,
            substring({col}, 1, 500) AS snippet,
            is_error
        FROM {ch.table}
        {where_clause}
        ORDER BY created_at DESC
        LIMIT %(limit)s
    """
    return await ch.query(sql, params)


async def list_errors(
    ch: ClickHouseClient,
    *,
    start: datetime | None = None,
    end: datetime | None = None,
    provider: str | None = None,
    upstream_status: int | None = None,
    limit: int = 100,
    offset: int = 0,
) -> tuple[list[dict[str, Any]], int]:
    """从 llm_errors_recent (近 30 天错误专表) 查询."""
    where: list[str] = []
    params: dict[str, Any] = {"limit": limit, "offset": offset}
    if start:
        where.append("created_at >= %(start)s")
        params["start"] = start
    if end:
        where.append("created_at < %(end)s")
        params["end"] = end
    if provider:
        where.append("provider = %(provider)s")
        params["provider"] = provider
    if upstream_status:
        where.append("upstream_status = %(upstream_status)s")
        params["upstream_status"] = upstream_status

    where_clause = " WHERE " + " AND ".join(where) if where else ""

    sql = f"""
        SELECT
            request_id, created_at, provider, model, user_id,
            upstream_status, error_type, error_message,
            duration_ms, request_text_preview
        FROM llm_errors_recent
        {where_clause}
        ORDER BY created_at DESC
        LIMIT %(limit)s OFFSET %(offset)s
    """
    rows = await ch.query(sql, params)
    count_params = {k: v for k, v in params.items() if k not in ("limit", "offset")}
    total = await ch.query_scalar(
        f"SELECT count() FROM llm_errors_recent {where_clause}",
        count_params,
    )
    return rows, int(total or 0)
