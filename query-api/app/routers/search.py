"""全文检索."""

from __future__ import annotations

from datetime import datetime

from fastapi import APIRouter, Depends, HTTPException, Query, status

from app.database.clickhouse import ClickHouseClient
from app.deps import get_ch, require_token
from app.schemas import SearchHit, SearchResponse
from app.services import search_service

router = APIRouter(prefix="/api/v1", tags=["search"], dependencies=[Depends(require_token)])


@router.get("/search", response_model=SearchResponse)
async def search(
    q: str = Query(..., min_length=2, description="检索关键词 (建议 >= 4 字符)"),
    field: str = Query(default="request", pattern="^(request|response)$"),
    provider: str | None = Query(default=None),
    client_type: str | None = Query(default=None),
    user_id: str | None = Query(default=None),
    start: datetime | None = Query(default=None),
    end: datetime | None = Query(default=None),
    limit: int = Query(default=100, ge=1, le=1000),
    ch: ClickHouseClient = Depends(get_ch),
) -> SearchResponse:
    if len(q) < 2:
        raise HTTPException(status_code=status.HTTP_400_BAD_REQUEST, detail="q 至少 2 字符")

    rows = await search_service.full_text_search(
        ch, query=q, field=field,
        start=start, end=end, provider=provider,
        client_type=client_type, user_id=user_id,
        limit=limit,
    )
    return SearchResponse(
        items=[SearchHit(**r) for r in rows],
        total=len(rows),
        query=q,
        field=field,
    )
