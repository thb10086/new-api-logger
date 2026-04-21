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
    client_type: str | None = None,
    user_id: str | None = None,
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
    if client_type:
        where.append("client_type = %(client_type)s")
        params["client_type"] = client_type
    if user_id:
        where.append("user_id = %(user_id)s")
        params["user_id"] = user_id

    where_clause = " WHERE " + " AND ".join(where)

    sql = f"""
        SELECT
            request_id,
            created_at,
            provider,
            model,
            user_id,
            client_type,
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
    client_type: str | None = None,
    upstream_status: int | None = None,
    limit: int = 100,
    offset: int = 0,
) -> tuple[list[dict[str, Any]], int]:
    """从 llm_errors_recent (近 30 天错误专表) 查询.

    若专表不存在 (003 migration TTL bug 可能导致建表失败), 或者需要
    client_type 过滤 (专表不含该字段), 自动回退到主表 is_error=1 查询.
    """
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

    # 尝试专表, 但专表没有 client_type 字段; 需要按 client_type 筛选时走主表
    table = "llm_errors_recent"
    force_main = client_type is not None
    if not force_main:
        try:
            await ch.query_scalar(f"SELECT 1 FROM {table} LIMIT 0")
        except Exception:
            force_main = True
    if force_main:
        table = ch.table
        if "is_error = 1" not in where:
            where.insert(0, "is_error = 1")
        if client_type:
            where.append("client_type = %(client_type)s")
            params["client_type"] = client_type

    where_clause = " WHERE " + " AND ".join(where) if where else ""

    # 主表带 client_type 输出, 专表不带 (前端默认空串)
    client_type_col = "client_type" if force_main else "'' AS client_type"
    sql = f"""
        SELECT
            request_id, created_at, provider, model, user_id,
            {client_type_col},
            upstream_status, error_type, error_message,
            duration_ms, request_text_preview
        FROM {table}
        {where_clause}
        ORDER BY created_at DESC
        LIMIT %(limit)s OFFSET %(offset)s
    """
    rows = await ch.query(sql, params)
    count_params = {k: v for k, v in params.items() if k not in ("limit", "offset")}
    total = await ch.query_scalar(
        f"SELECT count() FROM {table} {where_clause}",
        count_params,
    )
    return rows, int(total or 0)
