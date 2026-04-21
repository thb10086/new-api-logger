"""统计聚合服务 - 走物化视图."""

from __future__ import annotations

from datetime import datetime
from typing import Any

from app.database.clickhouse import ClickHouseClient


async def usage_stats(
    ch: ClickHouseClient,
    *,
    group_by: str = "day",  # day | hour
    start: datetime,
    end: datetime,
    provider: str | None = None,
    model: str | None = None,
    organization_id: str | None = None,
) -> list[dict[str, Any]]:
    """按时间桶 + provider + model 聚合用量."""
    if group_by not in ("day", "hour"):
        raise ValueError("group_by 必须为 'day' 或 'hour'")

    table = "llm_usage_daily" if group_by == "day" else "llm_usage_hourly"
    bucket_col = "day" if group_by == "day" else "hour"

    where = [f"{bucket_col} >= %(start)s", f"{bucket_col} < %(end)s"]
    params: dict[str, Any] = {"start": start, "end": end}
    if provider:
        where.append("provider = %(provider)s")
        params["provider"] = provider
    if model:
        where.append("model = %(model)s")
        params["model"] = model
    if organization_id:
        where.append("organization_id = %(organization_id)s")
        params["organization_id"] = organization_id

    where_clause = " WHERE " + " AND ".join(where)

    # CH 24.8+ 新 analyzer 要求别名不能与源列同名(否则 nullIf(sum(x),...) 里的 x 会被解析为外层别名,
    # 形成 sum(sum(x)) 嵌套聚合). 用子查询: 内层聚合加 _agg 后缀, 外层 rename 回契约字段名.
    if group_by == "day":
        sql = f"""
            SELECT
                bucket,
                provider,
                model,
                request_count_agg                                                       AS request_count,
                error_count_agg                                                         AS error_count,
                prompt_tokens_agg                                                       AS prompt_tokens,
                completion_tokens_agg                                                   AS completion_tokens,
                total_tokens_agg                                                        AS total_tokens,
                CAST(0 AS UInt64)                                                       AS cached_tokens,
                CAST(0 AS UInt64)                                                       AS reasoning_tokens,
                round(total_duration_ms_agg / nullIf(request_count_agg, 0), 1)          AS avg_latency_ms,
                request_bytes_agg                                                       AS request_bytes,
                response_bytes_agg                                                      AS response_bytes
            FROM (
                SELECT
                    toString({bucket_col}) AS bucket,
                    provider,
                    model,
                    sum(request_count)       AS request_count_agg,
                    sum(error_count)         AS error_count_agg,
                    sum(prompt_tokens)       AS prompt_tokens_agg,
                    sum(completion_tokens)   AS completion_tokens_agg,
                    sum(total_tokens)        AS total_tokens_agg,
                    sum(total_duration_ms)   AS total_duration_ms_agg,
                    sum(request_bytes)       AS request_bytes_agg,
                    sum(response_bytes)      AS response_bytes_agg
                FROM {table}
                {where_clause}
                GROUP BY bucket, provider, model
            )
            ORDER BY bucket DESC, total_tokens DESC
            LIMIT 1000
        """
    else:
        sql = f"""
            SELECT
                bucket,
                provider,
                model,
                request_count_agg                                                       AS request_count,
                error_count_agg                                                         AS error_count,
                prompt_tokens_agg                                                       AS prompt_tokens,
                completion_tokens_agg                                                   AS completion_tokens,
                total_tokens_agg                                                        AS total_tokens,
                cached_tokens_agg                                                       AS cached_tokens,
                reasoning_tokens_agg                                                    AS reasoning_tokens,
                round(total_duration_ms_agg / nullIf(request_count_agg, 0), 1)          AS avg_latency_ms,
                request_bytes_agg                                                       AS request_bytes,
                response_bytes_agg                                                      AS response_bytes
            FROM (
                SELECT
                    formatDateTime({bucket_col}, '%Y-%m-%d %H:00:00') AS bucket,
                    provider,
                    model,
                    sum(request_count)       AS request_count_agg,
                    sum(error_count)         AS error_count_agg,
                    sum(prompt_tokens)       AS prompt_tokens_agg,
                    sum(completion_tokens)   AS completion_tokens_agg,
                    sum(total_tokens)        AS total_tokens_agg,
                    sum(cached_tokens)       AS cached_tokens_agg,
                    sum(reasoning_tokens)    AS reasoning_tokens_agg,
                    sum(total_duration_ms)   AS total_duration_ms_agg,
                    sum(request_bytes)       AS request_bytes_agg,
                    sum(response_bytes)      AS response_bytes_agg
                FROM {table}
                {where_clause}
                GROUP BY bucket, provider, model
            )
            ORDER BY bucket DESC, total_tokens DESC
            LIMIT 5000
        """

    return await ch.query(sql, params)


async def provider_health(
    ch: ClickHouseClient,
    *,
    window_hours: int = 1,
) -> list[dict[str, Any]]:
    """近 N 小时的 provider 健康指标."""
    sql = """
        SELECT
            provider,
            sum(request_count)                                                          AS requests,
            sum(error_count)                                                            AS errors,
            round(sum(error_count) * 100.0 / nullIf(sum(request_count), 0), 2)          AS error_rate_pct,
            round(sum(total_duration_ms) / nullIf(sum(request_count), 0), 1)            AS avg_latency_ms,
            round(sum(total_ttft_ms) / nullIf(sum(stream_count), 0), 1)                 AS avg_ttft_ms,
            sum(total_tokens)                                                           AS tokens
        FROM llm_usage_hourly
        WHERE hour >= now() - INTERVAL %(window)s HOUR
        GROUP BY provider
        ORDER BY requests DESC
    """
    return await ch.query(sql, {"window": window_hours})
