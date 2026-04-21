"""Pydantic schemas - 全部集中在此, 便于客户端直接复用."""

from __future__ import annotations

from datetime import datetime

from pydantic import BaseModel, Field


# ============ Trace ============


class TraceSummary(BaseModel):
    """trace 列表的简化模型."""

    request_id: str
    created_at: datetime
    provider: str
    endpoint: str
    model: str
    is_stream: bool
    user_id: str = ""
    api_key_prefix: str = ""
    client_type: str = ""
    client_app_version: str = ""

    upstream_status: int
    is_error: bool
    error_message: str = ""

    duration_ms: int
    ttft_ms: int

    prompt_tokens: int
    completion_tokens: int
    total_tokens: int

    request_text_preview: str = ""
    response_text_preview: str = ""


class TraceDetail(TraceSummary):
    """单 trace 详情, 含原文 (可选)."""

    parent_request_id: str = ""
    completed_at: datetime | None = None
    client_ip: str = ""
    client_user_agent: str = ""
    api_key_hash: str = ""
    organization_id: str = ""

    http_method: str
    model_normalized: str = ""

    request_message_count: int = 0
    request_has_image: bool = False
    request_has_audio: bool = False
    request_has_video: bool = False
    request_has_tool: bool = False
    request_temperature: float | None = None
    request_top_p: float | None = None
    request_max_tokens: int | None = None

    response_finish_reason: str = ""
    response_chunk_count: int = 0
    cached_tokens: int = 0
    reasoning_tokens: int = 0

    error_type: str = ""
    request_size_bytes: int = 0
    response_size_bytes: int = 0

    # 原文 (默认不返回, 通过 ?include_raw=true 拉取)
    request_json: str | None = None
    response_json: str | None = None
    tool_calls_json: str | None = None
    request_cos_key: str = ""
    response_cos_key: str = ""


class TraceListResponse(BaseModel):
    items: list[TraceSummary]
    total: int = Field(description="符合条件的总数 (估算, 大数据集可能为 -1 表示未知)")
    limit: int
    offset: int


# ============ Stats ============


class UsageRow(BaseModel):
    bucket: str = Field(description="时间桶 (day 或 hour, 取决于 group_by)")
    provider: str
    model: str
    request_count: int
    error_count: int
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    cached_tokens: int = 0
    reasoning_tokens: int = 0
    avg_latency_ms: float
    request_bytes: int = 0
    response_bytes: int = 0


class UsageStatsResponse(BaseModel):
    items: list[UsageRow]
    group_by: str
    start: datetime
    end: datetime


class ProviderHealth(BaseModel):
    provider: str
    requests: int
    errors: int
    error_rate_pct: float
    avg_latency_ms: float
    avg_ttft_ms: float | None = None
    tokens: int


class HealthStatsResponse(BaseModel):
    items: list[ProviderHealth]
    window: str


# ============ Search ============


class SearchHit(BaseModel):
    request_id: str
    created_at: datetime
    provider: str
    model: str
    user_id: str = ""
    client_type: str = ""
    snippet: str
    is_error: bool


class SearchResponse(BaseModel):
    items: list[SearchHit]
    total: int
    query: str
    field: str


# ============ Errors ============


class ErrorRow(BaseModel):
    request_id: str
    created_at: datetime
    provider: str
    model: str
    user_id: str = ""
    client_type: str = ""
    upstream_status: int
    error_type: str
    error_message: str
    duration_ms: int
    request_text_preview: str = ""


class ErrorListResponse(BaseModel):
    items: list[ErrorRow]
    total: int


# ============ User Profile ============


class UserProfile(BaseModel):
    """用户画像 (来自 llm_call_logs 近 30 天聚合)."""

    user_id: str
    client_type: str = ""
    api_key_prefix: str = ""
    user_agent: str = ""
    total_requests: int
    total_errors: int
    prompt_tokens: int
    completion_tokens: int
    total_tokens: int
    cached_tokens: int = 0
    reasoning_tokens: int = 0
    request_bytes: int = 0
    response_bytes: int = 0
    models_used: list[str] = Field(default_factory=list)
    providers_used: list[str] = Field(default_factory=list)
    first_seen: datetime
    last_seen: datetime
    avg_duration_ms: float = 0


class UserListResponse(BaseModel):
    items: list[UserProfile]
    total: int
