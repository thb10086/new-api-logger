"""健康检查端点 (无需鉴权)."""

from fastapi import APIRouter, Depends

from app.database.clickhouse import ClickHouseClient
from app.deps import get_ch

router = APIRouter(tags=["health"])


@router.get("/healthz")
async def healthz() -> dict[str, str]:
    return {"status": "ok"}


@router.get("/readyz")
async def readyz(ch: ClickHouseClient = Depends(get_ch)) -> dict[str, object]:
    ch_ok = await ch.ping()
    return {
        "status": "ready" if ch_ok else "degraded",
        "clickhouse": ch_ok,
    }
