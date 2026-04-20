"""配置加载 (环境变量 LLMLOG_* 前缀)."""

from functools import lru_cache

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Query API 全部配置."""

    model_config = SettingsConfigDict(
        env_prefix="LLMLOG_",
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    # ============ HTTP server ============
    api_host: str = Field(default="0.0.0.0", description="监听地址")
    api_port: int = Field(default=8000, description="监听端口")
    api_token: str = Field(default="dev-token-change-me", description="REST API Bearer Token")
    api_cors_origins: str = Field(
        default="http://localhost:5173,http://localhost:3001",
        description="CORS 允许来源 (逗号分隔)",
    )

    # ============ ClickHouse ============
    ch_host: str = Field(default="localhost")
    ch_port: int = Field(default=8123)
    ch_database: str = Field(default="llm_logger")
    ch_user: str = Field(default="default")
    ch_password: str = Field(default="")
    ch_table: str = Field(default="llm_call_logs")
    ch_query_timeout: int = Field(default=30, description="查询超时 (秒)")

    # ============ 对象存储 (S3 兼容) ============
    s3_endpoint: str = Field(default="http://localhost:9000")
    s3_region: str = Field(default="us-east-1")
    s3_bucket: str = Field(default="llm-logs")
    s3_access_key: str = Field(default="minioadmin")
    s3_secret_key: str = Field(default="minioadmin")
    s3_use_path_style: bool = Field(default=True)
    s3_key_prefix: str = Field(default="llm-logs")

    # ============ 查询限制 ============
    max_query_limit: int = Field(default=1000, description="单次查询最多返回行数")
    default_limit: int = Field(default=50, description="默认 limit")

    # ============ 日志 ============
    log_level: str = Field(default="info")

    @property
    def cors_origins_list(self) -> list[str]:
        return [o.strip() for o in self.api_cors_origins.split(",") if o.strip()]


@lru_cache
def get_settings() -> Settings:
    """单例配置 (lru_cache)."""
    return Settings()
