"""健康检查端点 (无需鉴权) + Token 验证端点."""

from fastapi import APIRouter, Depends

from app.database.clickhouse import ClickHouseClient
from app.deps import get_ch, require_token

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


@router.get("/api/v1/auth/verify")
async def verify_token(_: None = Depends(require_token)) -> dict[str, str]:
    """TokenGate 专用: 验证 Bearer token 是否有效."""
    return {"status": "ok"}
