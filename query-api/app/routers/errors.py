"""错误诊断 (近 30 天错误专表)."""

from __future__ import annotations

from datetime import datetime

from fastapi import APIRouter, Depends, Query

from app.database.clickhouse import ClickHouseClient
from app.deps import get_ch, require_token
from app.schemas import ErrorListResponse, ErrorRow
from app.services import search_service

router = APIRouter(prefix="/api/v1", tags=["errors"], dependencies=[Depends(require_token)])


@router.get("/errors", response_model=ErrorListResponse)
async def list_errors(
    start: datetime | None = Query(default=None),
    end: datetime | None = Query(default=None),
    provider: str | None = Query(default=None),
    upstream_status: int | None = Query(default=None),
    limit: int = Query(default=100, ge=1, le=1000),
    offset: int = Query(default=0, ge=0),
    ch: ClickHouseClient = Depends(get_ch),
) -> ErrorListResponse:
    rows, total = await search_service.list_errors(
        ch,
        start=start, end=end, provider=provider,
        upstream_status=upstream_status, limit=limit, offset=offset,
    )
    return ErrorListResponse(
        items=[ErrorRow(**r) for r in rows],
        total=total,
    )
