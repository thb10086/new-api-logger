-- ============================================================
-- LLM Logger - ClickHouse schema 初始化 (第 2 步: 物化视图)
--
-- 物化视图在 INSERT 到主表时自动触发, 把 raw rows 预聚合到更小、
-- 更适合 OLAP 查询的表. 查询面板 / Grafana 直接查 MV, 亚秒级返回.
--
-- 三张 MV:
--   1) llm_usage_hourly  -- 小时 × provider × model × user × api_key 的累加量
--   2) llm_usage_daily   -- 日 × provider × model 的长期趋势 + unique users
--   3) llm_errors_recent -- 近 30 天错误专表 (加速错误诊断面板)
-- ============================================================

USE llm_logger;

-- ============================================================
-- 1. 小时级聚合 (SummingMergeTree, 永久保留)
-- ============================================================
CREATE TABLE IF NOT EXISTS llm_usage_hourly
(
    hour                    DateTime                           CODEC(Delta, ZSTD(3)),
    provider                LowCardinality(String),
    model                   LowCardinality(String),
    user_id                 String,
    api_key_hash            FixedString(64),
    organization_id         String DEFAULT '',

    -- 计数
    request_count           UInt64,
    error_count             UInt64,
    stream_count            UInt64,

    -- Token (累加)
    prompt_tokens           UInt64,
    completion_tokens       UInt64,
    total_tokens            UInt64,
    cached_tokens           UInt64,
    reasoning_tokens        UInt64,

    -- 流量 (字节)
    request_bytes           UInt64,
    response_bytes          UInt64,

    -- 性能 (累加, 求平均: total_duration_ms / request_count)
    total_duration_ms       UInt64,
    total_ttft_ms           UInt64
)
ENGINE = SummingMergeTree
PARTITION BY toYYYYMM(hour)
ORDER BY (hour, provider, model, user_id, api_key_hash, organization_id)
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_llm_usage_hourly
TO llm_usage_hourly
AS SELECT
    toStartOfHour(created_at)   AS hour,
    provider,
    model,
    user_id,
    api_key_hash,
    organization_id,
    1                           AS request_count,
    if(is_error, 1, 0)          AS error_count,
    if(is_stream, 1, 0)         AS stream_count,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    cached_tokens,
    reasoning_tokens,
    request_size_bytes          AS request_bytes,
    response_size_bytes         AS response_bytes,
    duration_ms                 AS total_duration_ms,
    ttft_ms                     AS total_ttft_ms
FROM llm_call_logs;

-- ============================================================
-- 2. 日级聚合 (AggregatingMergeTree, 含 unique_users 状态)
-- ============================================================
CREATE TABLE IF NOT EXISTS llm_usage_daily
(
    day                     Date                               CODEC(Delta, ZSTD(3)),
    provider                LowCardinality(String),
    model                   LowCardinality(String),
    organization_id         String DEFAULT '',

    request_count           UInt64,
    error_count             UInt64,
    unique_users            AggregateFunction(uniq, String),
    prompt_tokens           UInt64,
    completion_tokens       UInt64,
    total_tokens            UInt64,
    request_bytes           UInt64,
    response_bytes          UInt64,
    total_duration_ms       UInt64
)
ENGINE = AggregatingMergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (day, provider, model, organization_id)
SETTINGS index_granularity = 8192;

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_llm_usage_daily
TO llm_usage_daily
AS SELECT
    toDate(created_at)          AS day,
    provider,
    model,
    organization_id,
    sum(1)                      AS request_count,
    sumIf(1, is_error)          AS error_count,
    uniqState(user_id)          AS unique_users,
    sum(prompt_tokens)          AS prompt_tokens,
    sum(completion_tokens)      AS completion_tokens,
    sum(total_tokens)           AS total_tokens,
    sum(request_size_bytes)     AS request_bytes,
    sum(response_size_bytes)    AS response_bytes,
    sum(duration_ms)            AS total_duration_ms
FROM llm_call_logs
GROUP BY day, provider, model, organization_id;

-- ============================================================
-- 3. 错误专表 (近 30 天错误日志, 加速错误诊断面板)
--
-- 注: 专表不含 client_type 字段. 需要按 client_type 过滤错误时,
-- query-api 会自动回退到主表 (is_error = 1) 查询.
-- ============================================================
CREATE TABLE IF NOT EXISTS llm_errors_recent
(
    request_id              String                             CODEC(ZSTD(3)),
    created_at              DateTime64(3, 'UTC')               CODEC(Delta, ZSTD(3)),
    provider                LowCardinality(String),
    model                   LowCardinality(String),
    user_id                 String                             CODEC(ZSTD(3)),
    upstream_status         UInt16,
    error_type              LowCardinality(String),
    error_message           String                             CODEC(ZSTD(3)),
    duration_ms             UInt32,
    request_text_preview    String                             CODEC(ZSTD(3))
)
ENGINE = MergeTree
PARTITION BY toYYYYMMDD(created_at)
ORDER BY (created_at, provider, upstream_status)
TTL toDateTime(created_at) + INTERVAL 30 DAY DELETE
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
