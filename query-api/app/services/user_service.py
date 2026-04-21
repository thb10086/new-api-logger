"""用户画像服务 - 基于 llm_call_logs 的聚合查询."""

from __future__ import annotations

from typing import Any

from app.database.clickhouse import ClickHouseClient


async def list_user_profiles(
    ch: ClickHouseClient,
    *,
    client_type: str | None = None,
    min_requests: int = 0,
    days: int = 30,
    limit: int = 100,
    offset: int = 0,
    order_by: str = "total_tokens",
) -> tuple[list[dict[str, Any]], int]:
    """按 user_id + client_type 聚合最近 N 天的画像."""
    order_col_map = {
        "total_tokens": "total_tokens",
        "total_requests": "total_requests",
        "last_seen": "last_seen",
    }
    order_col = order_col_map.get(order_by, "total_tokens")

    where = ["created_at >= now() - INTERVAL %(days)s DAY", "user_id != ''"]
    params: dict[str, Any] = {"days": days, "limit": limit, "offset": offset}
    if client_type:
        where.append("client_type = %(client_type)s")
        params["client_type"] = client_type

    having = ""
    if min_requests > 0:
        having = " HAVING total_requests >= %(min_requests)s"
        params["min_requests"] = min_requests

    where_clause = " WHERE " + " AND ".join(where)

    sql = f"""
        SELECT
            user_id,
            client_type,
            any(api_key_prefix)                                    AS api_key_prefix,
            any(client_user_agent)                                 AS user_agent,
            count()                                                AS total_requests,
            countIf(is_error)                                      AS total_errors,
            sum(prompt_tokens)                                     AS prompt_tokens,
            sum(completion_tokens)                                 AS completion_tokens,
            sum(total_tokens)                                      AS total_tokens,
            sum(cached_tokens)                                     AS cached_tokens,
            sum(reasoning_tokens)                                  AS reasoning_tokens,
            sum(request_size_bytes)                                AS request_bytes,
            sum(response_size_bytes)                               AS response_bytes,
            arraySlice(arrayDistinct(groupArray(model)), 1, 10)    AS models_used,
            arrayDistinct(groupArray(provider))                    AS providers_used,
            min(created_at)                                        AS first_seen,
            max(created_at)                                        AS last_seen,
            round(avg(duration_ms), 1)                             AS avg_duration_ms
        FROM {ch.table}
        {where_clause}
        GROUP BY user_id, client_type
        {having}
        ORDER BY {order_col} DESC
        LIMIT %(limit)s OFFSET %(offset)s
    """
    rows = await ch.query(sql, params)

    count_params = {k: v for k, v in params.items() if k not in ("limit", "offset")}
    count_sql = f"""
        SELECT count() FROM (
            SELECT user_id, client_type
            FROM {ch.table}
            {where_clause}
            GROUP BY user_id, client_type
            {having}
        )
    """
    total = await ch.query_scalar(count_sql, count_params)

    for r in rows:
        r["models_used"] = [m for m in r.get("models_used") or [] if m]
        r["providers_used"] = [p for p in r.get("providers_used") or [] if p]

    return rows, int(total or 0)


async def get_user_profile(
    ch: ClickHouseClient,
    *,
    user_id: str,
    days: int = 30,
) -> dict[str, Any] | None:
    """获取单个用户聚合画像."""
    where = ["created_at >= now() - INTERVAL %(days)s DAY", "user_id = %(user_id)s"]
    params: dict[str, Any] = {"days": days, "user_id": user_id}
    where_clause = " WHERE " + " AND ".join(where)
    sql = f"""
        SELECT
            user_id,
            any(client_type)                                       AS client_type,
            any(api_key_prefix)                                    AS api_key_prefix,
            any(client_user_agent)                                 AS user_agent,
            count()                                                AS total_requests,
            countIf(is_error)                                      AS total_errors,
            sum(prompt_tokens)                                     AS prompt_tokens,
            sum(completion_tokens)                                 AS completion_tokens,
            sum(total_tokens)                                      AS total_tokens,
            sum(cached_tokens)                                     AS cached_tokens,
            sum(reasoning_tokens)                                  AS reasoning_tokens,
            sum(request_size_bytes)                                AS request_bytes,
            sum(response_size_bytes)                               AS response_bytes,
            arraySlice(arrayDistinct(groupArray(model)), 1, 20)    AS models_used,
            arrayDistinct(groupArray(provider))                    AS providers_used,
            min(created_at)                                        AS first_seen,
            max(created_at)                                        AS last_seen,
            round(avg(duration_ms), 1)                             AS avg_duration_ms
        FROM {ch.table}
        {where_clause}
        GROUP BY user_id
        LIMIT 1
    """
    row = await ch.query_one(sql, params)
    if not row:
        return None
    row["models_used"] = [m for m in row.get("models_used") or [] if m]
    row["providers_used"] = [p for p in row.get("providers_used") or [] if p]
    return row
