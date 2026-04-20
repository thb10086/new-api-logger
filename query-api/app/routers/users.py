"""按用户的时间线."""

from __future__ import annotations

from fastapi import APIRouter, Depends, Query

from app.database.clickhouse import ClickHouseClient
from app.deps import get_ch, require_token
from app.schemas import TraceListResponse, TraceSummary
from app.services import trace_service

router = APIRouter(prefix="/api/v1/users", tags=["users"], dependencies=[Depends(require_token)])


@router.get("/{user_id}/traces", response_model=TraceListResponse)
async def list_user_traces(
    user_id: str,
    limit: int = Query(default=50, ge=1, le=1000),
    offset: int = Query(default=0, ge=0),
    days: int = Query(default=30, ge=1, le=365, description="回溯天数"),
    ch: ClickHouseClient = Depends(get_ch),
) -> TraceListResponse:
    rows, total = await trace_service.list_user_traces(
        ch, user_id=user_id, limit=limit, offset=offset, days=days,
    )
    return TraceListResponse(
        items=[TraceSummary(**r) for r in rows],
        total=total,
        limit=limit,
        offset=offset,
    )
