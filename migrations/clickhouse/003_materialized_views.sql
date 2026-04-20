-- ============================================================
-- LLM Logger - 003_materialized_views.sql
--
-- 物化视图: 按"小时 × 模型 × 用户 × api_key"预聚合, 用于:
--   1. Grafana / Superset 用量看板 (亚秒级响应)
--   2. 计费 / 成本审计
--   3. 异常监控 (error_count / avg_latency)
--
-- 同时建立按"日"粒度的二级聚合, 用于跨年长期趋势分析.
-- ============================================================

USE llm_logger;

-- ============================================================
-- 1. 小时级聚合 (主聚合, 永久保留)
-- ============================================================
CREATE TABLE IF NOT EXISTS llm_usage_hourly
(
    hour                 DateTime CODEC(Delta, ZSTD(3)),
    provider             LowCardinality(String),
    model                LowCardinality(String),
    user_id              String,
    api_key_hash         FixedString(64),
    organization_id      String DEFAULT '',

    -- 计数
    request_count        UInt64,
    error_count          UInt64,
    stream_count         UInt64,

    -- Token (累加)
    prompt_tokens        UInt64,
    completion_tokens    UInt64,
    total_tokens         UInt64,
    cached_tokens        UInt64,
    reasoning_tokens     UInt64,

    -- 流量 (字节)
    request_bytes        UInt64,
    response_bytes       UInt64,

    -- 性能 (累加, 用于求平均: total_duration_ms / request_count)
    total_duration_ms    UInt64,
    total_ttft_ms        UInt64
)
ENGINE = SummingMergeTree
PARTITION BY toYYYYMM(hour)
ORDER BY (hour, provider, model, user_id, api_key_hash, organization_id)
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_llm_usage_hourly
TO llm_usage_hourly
AS SELECT
    toStartOfHour(created_at)              AS hour,
    provider,
    model,
    user_id,
    api_key_hash,
    organization_id,
    1                                      AS request_count,
    if(is_error, 1, 0)                     AS error_count,
    if(is_stream, 1, 0)                    AS stream_count,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    cached_tokens,
    reasoning_tokens,
    request_size_bytes                     AS request_bytes,
    response_size_bytes                    AS response_bytes,
    duration_ms                            AS total_duration_ms,
    ttft_ms                                AS total_ttft_ms
FROM llm_call_logs;

-- ============================================================
-- 2. 日级聚合 (二级聚合, 长期趋势分析)
-- ============================================================
CREATE TABLE IF NOT EXISTS llm_usage_daily
(
    day                  Date CODEC(Delta, ZSTD(3)),
    provider             LowCardinality(String),
    model                LowCardinality(String),
    organization_id      String DEFAULT '',

    request_count        UInt64,
    error_count          UInt64,
    unique_users         AggregateFunction(uniq, String),
    prompt_tokens        UInt64,
    completion_tokens    UInt64,
    total_tokens         UInt64,
    request_bytes        UInt64,
    response_bytes       UInt64,
    total_duration_ms    UInt64
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (day, provider, model, organization_id)
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_llm_usage_daily
TO llm_usage_daily
AS SELECT
    toDate(created_at)                     AS day,
    provider,
    model,
    organization_id,
    sum(1)                                 AS request_count,
    sumIf(1, is_error)                     AS error_count,
    uniqState(user_id)                     AS unique_users,
    sum(prompt_tokens)                     AS prompt_tokens,
    sum(completion_tokens)                 AS completion_tokens,
    sum(total_tokens)                      AS total_tokens,
    sum(request_size_bytes)                AS request_bytes,
    sum(response_size_bytes)               AS response_bytes,
    sum(duration_ms)                       AS total_duration_ms
FROM llm_call_logs
GROUP BY day, provider, model, organization_id;

-- ============================================================
-- 3. 错误专表 (近 30 天的错误日志, 加速错误诊断)
-- ============================================================
CREATE TABLE IF NOT EXISTS llm_errors_recent
(
    request_id           String CODEC(ZSTD(3)),
    created_at           DateTime64(3, 'UTC') CODEC(Delta, ZSTD(3)),
    provider             LowCardinality(String),
    model                LowCardinality(String),
    user_id              String CODEC(ZSTD(3)),
    upstream_status      UInt16,
    error_type           LowCardinality(String),
    error_message        String CODEC(ZSTD(3)),
    duration_ms          UInt32,
    request_text_preview String CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(created_at)
ORDER BY (created_at, provider, upstream_status)
TTL created_at + INTERVAL 30 DAY DELETE
SETTINGS index_granularity = 4096;

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_llm_errors_recent
TO llm_errors_recent
AS SELECT
    request_id,
    created_at,
    provider,
    model,
    user_id,
    upstream_status,
    error_type,
    error_message,
    duration_ms,
    request_text_preview
FROM llm_call_logs
WHERE is_error = 1;

INSERT INTO schema_versions (version, description) VALUES (3, 'create materialized views (hourly + daily + errors)');
