"""ClickHouse 异步 client.

使用 clickhouse-connect 的 async client.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any

import clickhouse_connect
from clickhouse_connect.driver.asyncclient import AsyncClient

from app.config import get_settings

log = logging.getLogger(__name__)


class ClickHouseClient:
    """轻量包装, 提供常用查询方法."""

    def __init__(self, client: AsyncClient, table: str) -> None:
        self._client = client
        self._table = table

    @property
    def table(self) -> str:
        return self._table

    async def query(
        self,
        sql: str,
        parameters: dict[str, Any] | None = None,
    ) -> list[dict[str, Any]]:
        """执行查询, 返回 list of dict (列名为 key)."""
        result = await self._client.query(sql, parameters=parameters)
        cols = result.column_names
        return [dict(zip(cols, row, strict=False)) for row in result.result_rows]

    async def query_one(
        self,
        sql: str,
        parameters: dict[str, Any] | None = None,
    ) -> dict[str, Any] | None:
        rows = await self.query(sql, parameters)
        return rows[0] if rows else None

    async def query_scalar(
        self,
        sql: str,
        parameters: dict[str, Any] | None = None,
    ) -> Any:
        result = await self._client.query(sql, parameters=parameters)
        if not result.result_rows:
            return None
        return result.result_rows[0][0]

    async def ping(self) -> bool:
        try:
            await self._client.command("SELECT 1")
            return True
        except Exception as e:
            log.warning("ClickHouse ping failed: %s", e)
            return False

    async def close(self) -> None:
        try:
            await self._client.close()
        except Exception:
            pass


_client: ClickHouseClient | None = None
_init_lock: asyncio.Lock | None = None


async def get_ch_client() -> ClickHouseClient:
    """获取全局 ClickHouse client 单例 (延迟初始化 + 并发安全).

    clickhouse_connect.get_async_client 在新版本是 coroutine,
    必须 await 执行才能拿到实际的 AsyncClient 对象.
    """
    global _client, _init_lock
    if _client is not None:
        return _client
    if _init_lock is None:
        _init_lock = asyncio.Lock()
    async with _init_lock:
        if _client is not None:
            return _client
        s = get_settings()
        raw = await clickhouse_connect.get_async_client(
            host=s.ch_host,
            port=s.ch_port,
            username=s.ch_user,
            password=s.ch_password,
            database=s.ch_database,
            connect_timeout=10,
            send_receive_timeout=s.ch_query_timeout,
            compress=True,
        )
        _client = ClickHouseClient(raw, table=s.ch_table)
        log.info("ClickHouse client 创建完成 (host=%s db=%s)", s.ch_host, s.ch_database)
        return _client
