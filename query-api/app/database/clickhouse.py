"""ClickHouse 异步 client.

使用 clickhouse-connect 的 async client.
"""

from __future__ import annotations

import asyncio
import ipaddress
import logging
from datetime import date, datetime
from decimal import Decimal
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

    @staticmethod
    def _sanitize_params(params: dict[str, Any] | None) -> dict[str, Any] | None:
        """将 Python 类型安全转为 clickhouse-connect 兼容格式."""
        if not params:
            return params
        out: dict[str, Any] = {}
        for k, v in params.items():
            if isinstance(v, datetime):
                out[k] = v.strftime("%Y-%m-%d %H:%M:%S")
            elif isinstance(v, date):
                out[k] = v.strftime("%Y-%m-%d")
            elif isinstance(v, bool):
                out[k] = 1 if v else 0
            else:
                out[k] = v
        return out

    @staticmethod
    def _sanitize_row(row: dict[str, Any]) -> dict[str, Any]:
        """将 CH 返回的特殊类型转为 Python 基础类型 (Pydantic 可序列化)."""
        for k, v in row.items():
            if isinstance(v, (ipaddress.IPv4Address, ipaddress.IPv6Address)):
                row[k] = str(v)
            elif isinstance(v, Decimal):
                row[k] = float(v)
            elif isinstance(v, bytes):
                row[k] = v.decode("utf-8", errors="replace")
            elif hasattr(v, "item"):  # numpy scalar
                row[k] = v.item()
        return row

    async def query(
        self,
        sql: str,
        parameters: dict[str, Any] | None = None,
    ) -> list[dict[str, Any]]:
        """执行查询, 返回 list of dict (列名为 key)."""
        safe_params = self._sanitize_params(parameters)
        result = await self._client.query(sql, parameters=safe_params)
        cols = result.column_names
        return [
            self._sanitize_row(dict(zip(cols, row, strict=False)))
            for row in result.result_rows
        ]

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
        safe_params = self._sanitize_params(parameters)
        result = await self._client.query(sql, parameters=safe_params)
        if not result.result_rows:
            return None
        v = result.result_rows[0][0]
        if hasattr(v, "item"):
            return v.item()
        return v

    async def query_iter(
        self,
        sql_with_limit_offset: str,
        parameters: dict[str, Any] | None = None,
        *,
        chunk_size: int = 5000,
        max_rows: int = 10_000_000,
    ):
        """分块迭代查询 - yield 单行 dict.

        适合导出场景, 不依赖 async stream API.
        调用方提供的 sql 必须含 %(_limit)s + %(_offset)s 占位符,
        本方法会反复调用直到返回空或达到 max_rows 上限.

        示例:
            sql = "SELECT ... FROM llm_call_logs WHERE ... LIMIT %(_limit)s OFFSET %(_offset)s"
            async for row in ch.query_iter(sql, params):
                yield row
        """
        params = dict(parameters or {})
        offset = 0
        fetched = 0
        while fetched < max_rows:
            params["_limit"] = min(chunk_size, max_rows - fetched)
            params["_offset"] = offset
            rows = await self.query(sql_with_limit_offset, params)
            if not rows:
                return
            for r in rows:
                yield r
                fetched += 1
                if fetched >= max_rows:
                    return
            if len(rows) < params["_limit"]:
                return
            offset += len(rows)

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
