"""Trace 列表 / 详情 / 用户时间线."""

from __future__ import annotations

from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException, Query, status

from app.config import Settings, get_settings
from app.database.clickhouse import ClickHouseClient
from app.database.cos import COSClient
from app.deps import get_ch, get_cos, require_token
from app.schemas import TraceDetail, TraceListResponse, TraceSummary
from app.services import trace_service

router = APIRouter(prefix="/api/v1", tags=["traces"], dependencies=[Depends(require_token)])


@router.get("/traces", response_model=TraceListResponse)
async def list_traces(
    provider: str | None = Query(default=None),
    model: str | None = Query(default=None),
    user_id: str | None = Query(default=None),
    api_key_hash: str | None = Query(default=None),
    api_key_prefix: str | None = Query(default=None),
    client_type: str | None = Query(default=None, description="claude_code|openai_sdk|anthropic_sdk|..."),
    is_error: bool | None = Query(default=None),
    start: datetime | None = Query(default=None),
    end: datetime | None = Query(default=None),
    limit: int = Query(default=50, ge=1, le=1000),
    offset: int = Query(default=0, ge=0),
    ch: ClickHouseClient = Depends(get_ch),
    settings: Settings = Depends(get_settings),
) -> TraceListResponse:
    if limit > settings.max_query_limit:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail=f"limit 不能超过 {settings.max_query_limit}",
        )
    rows, total = await trace_service.list_traces(
        ch,
        provider=provider,
        model=model,
        user_id=user_id,
        api_key_hash=api_key_hash,
        api_key_prefix=api_key_prefix,
        client_type=client_type,
        is_error=is_error,
        start=start,
        end=end,
        limit=limit,
        offset=offset,
    )
    return TraceListResponse(
        items=[TraceSummary(**r) for r in rows],
        total=total,
        limit=limit,
        offset=offset,
    )


@router.get("/traces/{request_id}", response_model=TraceDetail)
async def get_trace(
    request_id: str,
    include_raw: bool = Query(default=False, description="是否拉取 COS 原文"),
    ch: ClickHouseClient = Depends(get_ch),
    cos: COSClient = Depends(get_cos),
) -> TraceDetail:
    row = await trace_service.get_trace(ch, cos, request_id=request_id, include_raw=include_raw)
    if not row:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="trace not found")
    return TraceDetail(**row)
