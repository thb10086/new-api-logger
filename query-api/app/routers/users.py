"""用户画像 + 按用户时间线."""

from __future__ import annotations

from fastapi import APIRouter, Depends, HTTPException, Query, status

from app.database.clickhouse import ClickHouseClient
from app.deps import get_ch, require_token
from app.schemas import (
    TraceListResponse,
    TraceSummary,
    UserListResponse,
    UserProfile,
)
from app.services import trace_service, user_service

router = APIRouter(prefix="/api/v1/users", tags=["users"], dependencies=[Depends(require_token)])


@router.get("", response_model=UserListResponse)
async def list_users(
    client_type: str | None = Query(default=None, description="claude_code|openai_sdk|..."),
    min_requests: int = Query(default=0, ge=0, description="最少请求数过滤"),
    days: int = Query(default=30, ge=1, le=365),
    order_by: str = Query(default="total_tokens", pattern="^(total_tokens|total_requests|last_seen)$"),
    limit: int = Query(default=100, ge=1, le=1000),
    offset: int = Query(default=0, ge=0),
    ch: ClickHouseClient = Depends(get_ch),
) -> UserListResponse:
    """用户画像列表 (按 token 消耗排序)."""
    rows, total = await user_service.list_user_profiles(
        ch,
        client_type=client_type,
        min_requests=min_requests,
        days=days,
        limit=limit,
        offset=offset,
        order_by=order_by,
    )
    return UserListResponse(
        items=[UserProfile(**r) for r in rows],
        total=total,
    )


@router.get("/{user_id}", response_model=UserProfile)
async def get_user_profile(
    user_id: str,
    days: int = Query(default=30, ge=1, le=365),
    ch: ClickHouseClient = Depends(get_ch),
) -> UserProfile:
    """单个用户画像详情."""
    row = await user_service.get_user_profile(ch, user_id=user_id, days=days)
    if not row:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="user not found")
    return UserProfile(**row)


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
