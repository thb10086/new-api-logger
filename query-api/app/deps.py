"""FastAPI dependency 工厂."""

from collections.abc import AsyncGenerator

from fastapi import Depends, Header, HTTPException, status

from app.config import Settings, get_settings
from app.database.clickhouse import ClickHouseClient, get_ch_client
from app.database.cos import COSClient, get_cos_client


async def require_token(
    authorization: str | None = Header(default=None),
    settings: Settings = Depends(get_settings),
) -> None:
    """Bearer Token 鉴权."""
    if not authorization:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="缺少 Authorization header",
            headers={"WWW-Authenticate": "Bearer"},
        )
    scheme, _, token = authorization.partition(" ")
    if scheme.lower() != "bearer" or not token:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Authorization 格式应为 'Bearer <token>'",
            headers={"WWW-Authenticate": "Bearer"},
        )
    if token != settings.api_token:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="无效的 token",
        )


async def get_ch() -> AsyncGenerator[ClickHouseClient, None]:
    client = get_ch_client()
    yield client


async def get_cos() -> AsyncGenerator[COSClient, None]:
    client = get_cos_client()
    yield client
