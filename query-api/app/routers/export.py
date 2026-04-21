"""批量导出 - JSONL / CSV 流式下载.

设计:
  - 基于 query_iter 分块查询 (每 5000 行一 chunk), 全程不占用大内存
  - 默认 max_rows=100000 (轻量导出), ?deep=true 时最多 1000 万
  - 默认只导出元数据 + preview; ?include_body=true 时导出 request_json + response_json
"""

from __future__ import annotations

import csv
import io
import json
from datetime import datetime, timezone
from typing import Any, Literal

from fastapi import APIRouter, Depends, Query
from fastapi.responses import StreamingResponse

from app.database.clickhouse import ClickHouseClient
from app.deps import get_ch, require_token

router = APIRouter(prefix="/api/v1", tags=["export"], dependencies=[Depends(require_token)])


_META_COLS = [
    "request_id", "created_at", "provider", "endpoint", "model", "is_stream",
    "user_id", "api_key_prefix", "client_type", "client_app_version",
    "upstream_status", "is_error", "error_type", "error_message",
    "duration_ms", "ttft_ms",
    "prompt_tokens", "completion_tokens", "total_tokens",
    "cached_tokens", "reasoning_tokens",
    "request_has_image", "request_has_audio", "request_has_video", "request_has_tool",
    "request_message_count",
    "request_text_preview", "response_text_preview",
]

_BODY_COLS = ["request_json", "response_json", "tool_calls_json", "request_cos_key", "response_cos_key"]


def _build_where(
    start: datetime | None,
    end: datetime | None,
    provider: str | None,
    client_type: str | None,
    user_id: str | None,
    api_key_prefix: str | None,
    is_error: bool | None,
) -> tuple[str, dict[str, Any]]:
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
    if client_type:
        where.append("client_type = %(client_type)s")
        params["client_type"] = client_type
    if user_id:
        where.append("user_id = %(user_id)s")
        params["user_id"] = user_id
    if api_key_prefix:
        where.append("api_key_prefix = %(api_key_prefix)s")
        params["api_key_prefix"] = api_key_prefix
    if is_error is not None:
        where.append("is_error = %(is_error)s")
        params["is_error"] = 1 if is_error else 0
    clause = " WHERE " + " AND ".join(where) if where else ""
    return clause, params


@router.get("/export/traces")
async def export_traces(
    format: Literal["jsonl", "csv"] = Query(default="jsonl"),
    include_body: bool = Query(default=False, description="是否包含 request_json/response_json (体积大)"),
    provider: str | None = Query(default=None),
    client_type: str | None = Query(default=None),
    user_id: str | None = Query(default=None),
    api_key_prefix: str | None = Query(default=None),
    is_error: bool | None = Query(default=None),
    start: datetime | None = Query(default=None),
    end: datetime | None = Query(default=None),
    max_rows: int = Query(default=100_000, ge=1, le=10_000_000, description="最多导出行数"),
    ch: ClickHouseClient = Depends(get_ch),
):
    """流式导出 traces 为 JSONL / CSV.

    使用建议:
      - 元数据导出 (默认): 100 万行 < 500 MB, 用于训练集筛选
      - 含 body 导出: 体积 10x, 建议先用 max_rows=10000 小规模试
      - 再大规模请走异步导出任务 (未来版本)
    """
    cols = _META_COLS + (_BODY_COLS if include_body else [])
    cols_sql = ", ".join(cols)

    where_clause, params = _build_where(
        start, end, provider, client_type, user_id, api_key_prefix, is_error,
    )

    sql = f"""
        SELECT {cols_sql}
        FROM {ch.table}
        {where_clause}
        ORDER BY created_at DESC
        LIMIT %(_limit)s OFFSET %(_offset)s
    """

    ts = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
    filename = f"traces-{ts}.{format}"

    if format == "jsonl":
        async def gen_jsonl():
            async for row in ch.query_iter(sql, params, chunk_size=5000, max_rows=max_rows):
                # datetime → ISO 字符串
                if isinstance(row.get("created_at"), datetime):
                    row["created_at"] = row["created_at"].isoformat()
                yield (json.dumps(row, ensure_ascii=False, default=str) + "\n").encode("utf-8")

        return StreamingResponse(
            gen_jsonl(),
            media_type="application/x-ndjson",
            headers={"Content-Disposition": f'attachment; filename="{filename}"'},
        )

    # CSV: 先写 header, 再流式追加行
    async def gen_csv():
        buf = io.StringIO()
        writer = csv.DictWriter(buf, fieldnames=cols, extrasaction="ignore")
        writer.writeheader()
        yield buf.getvalue().encode("utf-8-sig")  # BOM, Excel 友好
        buf.seek(0)
        buf.truncate()

        async for row in ch.query_iter(sql, params, chunk_size=5000, max_rows=max_rows):
            if isinstance(row.get("created_at"), datetime):
                row["created_at"] = row["created_at"].isoformat()
            writer.writerow({k: row.get(k, "") for k in cols})
            yield buf.getvalue().encode("utf-8")
            buf.seek(0)
            buf.truncate()

    return StreamingResponse(
        gen_csv(),
        media_type="text/csv; charset=utf-8",
        headers={"Content-Disposition": f'attachment; filename="{filename}"'},
    )
