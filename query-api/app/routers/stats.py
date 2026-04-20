"""用量统计 + provider 健康."""

from __future__ import annotations

from datetime import datetime, timedelta, timezone

from fastapi import APIRouter, Depends, HTTPException, Query, status

from app.database.clickhouse import ClickHouseClient
from app.deps import get_ch, require_token
from app.schemas import HealthStatsResponse, ProviderHealth, UsageRow, UsageStatsResponse
from app.services import stats_service

router = APIRouter(prefix="/api/v1/stats", tags=["stats"], dependencies=[Depends(require_token)])


@router.get("/usage", response_model=UsageStatsResponse)
async def usage(
    group_by: str = Query(default="day", pattern="^(day|hour)$"),
    start: datetime | None = Query(default=None),
    end: datetime | None = Query(default=None),
    provider: str | None = Query(default=None),
    model: str | None = Query(default=None),
    organization_id: str | None = Query(default=None),
    ch: ClickHouseClient = Depends(get_ch),
) -> UsageStatsResponse:
    if not end:
        end = datetime.now(timezone.utc)
    if not start:
        start = end - (timedelta(days=30) if group_by == "day" else timedelta(days=2))
    if start >= end:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="start 必须早于 end")

    rows = await stats_service.usage_stats(
        ch,
        group_by=group_by,
        start=start,
        end=end,
        provider=provider,
        model=model,
        organization_id=organization_id,
    )
    return UsageStatsResponse(
        items=[UsageRow(**r) for r in rows],
        group_by=group_by,
        start=start,
        end=end,
    )


@router.get("/health", response_model=HealthStatsResponse)
async def health(
    window_hours: int = Query(default=1, ge=1, le=24, description="回溯小时数"),
    ch: ClickHouseClient = Depends(get_ch),
) -> HealthStatsResponse:
    rows = await stats_service.provider_health(ch, window_hours=window_hours)
    return HealthStatsResponse(
        items=[ProviderHealth(**r) for r in rows],
        window=f"{window_hours}h",
    )
