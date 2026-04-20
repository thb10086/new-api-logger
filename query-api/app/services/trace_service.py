"""Trace 查询服务 - 列表 / 详情 / 用户时间线."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from app.database.clickhouse import ClickHouseClient
from app.database.cos import COSClient

# 列表查询: 仅 summary 字段, 不含原文
_SUMMARY_COLS = """
    request_id, created_at, provider, endpoint, model, is_stream,
    user_id, api_key_prefix,
    upstream_status, is_error, error_message,
    duration_ms, ttft_ms,
    prompt_tokens, completion_tokens, total_tokens,
    request_text_preview, response_text_preview
""".strip()

_DETAIL_COLS = """
    request_id, parent_request_id, created_at, completed_at,
    client_ip, client_user_agent, api_key_hash, api_key_prefix,
    user_id, organization_id,
    provider, endpoint, http_method, is_stream,
    model, model_normalized,
    request_message_count, request_has_image, request_has_audio,
    request_has_video, request_has_tool,
    request_temperature, request_top_p, request_max_tokens,
    request_size_bytes, response_size_bytes,
    response_finish_reason, response_chunk_count,
    prompt_tokens, completion_tokens, total_tokens,
    cached_tokens, reasoning_tokens,
    ttft_ms, duration_ms, upstream_status,
    is_error, error_type, error_message,
    request_text_preview, response_text_preview,
    request_json, response_json,
    tool_calls_json, request_cos_key, response_cos_key
""".strip()


async def list_traces(
    ch: ClickHouseClient,
    *,
    provider: str | None = None,
    model: str | None = None,
    user_id: str | None = None,
    api_key_hash: str | None = None,
    is_error: bool | None = None,
    start: datetime | None = None,
    end: datetime | None = None,
    limit: int = 50,
    offset: int = 0,
) -> tuple[list[dict[str, Any]], int]:
    """列出 trace, 返回 (rows, total_estimate)."""
    where: list[str] = []
    params: dict[str, Any] = {}

    if start:
        where.append("created_at >= %(start)s")
        params["start"] = start
    if end:
        where.append("created_at < %(end)s")
        params["end"] = end
    if provider:
        where.append("provider = %(provider)s")
        params["provider"] = provider
    if model:
        where.append("model = %(model)s")
        params["model"] = model
    if user_id:
        where.append("user_id = %(user_id)s")
        params["user_id"] = user_id
    if api_key_hash:
        where.append("api_key_hash = %(api_key_hash)s")
        params["api_key_hash"] = api_key_hash
    if is_error is not None:
        where.append("is_error = %(is_error)s")
        params["is_error"] = 1 if is_error else 0

    where_clause = " WHERE " + " AND ".join(where) if where else ""

    sql = f"""
        SELECT {_SUMMARY_COLS}
        FROM {ch.table}
        {where_clause}
        ORDER BY created_at DESC
        LIMIT %(limit)s OFFSET %(offset)s
    """
    params["limit"] = limit
    params["offset"] = offset

    rows = await ch.query(sql, params)

    # 估算总数 (亿级表 count(*) 也很快, 但加 LIMIT 1 避免长表全扫风险)
    count_sql = f"SELECT count() FROM {ch.table}{where_clause}"
    total = await ch.query_scalar(count_sql, {k: v for k, v in params.items() if k not in ("limit", "offset")})
    return rows, int(total or 0)


async def get_trace(
    ch: ClickHouseClient,
    cos: COSClient,
    *,
    request_id: str,
    include_raw: bool = False,
) -> dict[str, Any] | None:
    """获取单 trace 详情. include_raw=True 时拉取 COS 原文."""
    sql = f"""
        SELECT {_DETAIL_COLS}
        FROM {ch.table}
        WHERE request_id = %(request_id)s
        LIMIT 1
    """
    row = await ch.query_one(sql, {"request_id": request_id})
    if not row:
        return None

    if include_raw:
        # 优先内联, 退化到 COS
        if not row.get("request_json") and row.get("request_cos_key"):
            try:
                row["request_json"] = await cos.fetch_text(row["request_cos_key"])
            except Exception as e:  # noqa: BLE001
                row["request_json"] = f"<COS fetch failed: {e}>"
        if not row.get("response_json") and row.get("response_cos_key"):
            try:
                row["response_json"] = await cos.fetch_text(row["response_cos_key"])
            except Exception as e:  # noqa: BLE001
                row["response_json"] = f"<COS fetch failed: {e}>"
    else:
        # 不返回 raw, 节省带宽
        row["request_json"] = None
        row["response_json"] = None

    return row


async def list_user_traces(
    ch: ClickHouseClient,
    *,
    user_id: str,
    limit: int = 50,
    offset: int = 0,
    days: int = 30,
) -> tuple[list[dict[str, Any]], int]:
    sql = f"""
        SELECT {_SUMMARY_COLS}
        FROM {ch.table}
        WHERE user_id = %(user_id)s
          AND created_at >= now() - INTERVAL %(days)s DAY
        ORDER BY created_at DESC
        LIMIT %(limit)s OFFSET %(offset)s
    """
    params = {"user_id": user_id, "days": days, "limit": limit, "offset": offset}
    rows = await ch.query(sql, params)
    total = await ch.query_scalar(
        f"""
        SELECT count()
        FROM {ch.table}
        WHERE user_id = %(user_id)s
          AND created_at >= now() - INTERVAL %(days)s DAY
        """,
        {"user_id": user_id, "days": days},
    )
    return rows, int(total or 0)
