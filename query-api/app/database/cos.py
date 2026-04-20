"""对象存储 (COS / S3 兼容) 异步 client.

负责按 cos_key 拉取原始 body 并 zstd 解压.
"""

from __future__ import annotations

import logging
from functools import lru_cache

import aioboto3
import zstandard

from app.config import get_settings

log = logging.getLogger(__name__)


class COSClient:
    """异步 S3 兼容 client."""

    def __init__(self, bucket: str, prefix: str = "") -> None:
        s = get_settings()
        self._bucket = bucket
        self._prefix = prefix.strip("/")
        self._endpoint = s.s3_endpoint
        self._region = s.s3_region
        self._access_key = s.s3_access_key
        self._secret_key = s.s3_secret_key
        self._use_path_style = s.s3_use_path_style
        self._session = aioboto3.Session()

    def _full_key(self, key: str) -> str:
        key = key.lstrip("/")
        if self._prefix and not key.startswith(self._prefix + "/"):
            key = f"{self._prefix}/{key}"
        if not key.endswith(".zst"):
            key = f"{key}.zst"
        return key

    async def fetch(self, key: str) -> bytes:
        """拉取并解压一个对象, 返回原始字节."""
        full_key = self._full_key(key)
        async with self._session.client(
            "s3",
            endpoint_url=self._endpoint,
            region_name=self._region,
            aws_access_key_id=self._access_key,
            aws_secret_access_key=self._secret_key,
            config=aioboto3_config(self._use_path_style),
        ) as s3:
            resp = await s3.get_object(Bucket=self._bucket, Key=full_key)
            data = await resp["Body"].read()
        return zstandard.ZstdDecompressor().decompress(data)

    async def fetch_text(self, key: str) -> str:
        return (await self.fetch(key)).decode("utf-8", errors="replace")


def aioboto3_config(use_path_style: bool):
    from botocore.config import Config

    return Config(
        signature_version="s3v4",
        s3={"addressing_style": "path" if use_path_style else "virtual"},
    )


_cos_client: COSClient | None = None


@lru_cache
def get_cos_client() -> COSClient:
    global _cos_client
    if _cos_client is not None:
        return _cos_client
    s = get_settings()
    _cos_client = COSClient(bucket=s.s3_bucket, prefix=s.s3_key_prefix)
    log.info("COS client 创建完成 (bucket=%s prefix=%s)", s.s3_bucket, s.s3_key_prefix)
    return _cos_client
