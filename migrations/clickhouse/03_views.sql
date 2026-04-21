-- ============================================================
-- LLM Logger - ClickHouse schema 初始化 (第 3 步: 逻辑视图)
--
-- 非物化视图, 仅作为常用查询的 SQL alias, 方便人工排查 / Grafana
-- 面板 / ad-hoc 分析. 正式面板的高频查询走 query-api + MV.
-- ============================================================

USE llm_logger;

-- ============================================================
-- 1. 用户最近请求 (近 7 天, 调用方自行 LIMIT)
-- ============================================================
CREATE OR REPLACE VIEW v_user_recent_traces AS
SELECT
    request_id,
    created_at,
    provider,
    model,
    is_stream,
    duration_ms,
    ttft_ms,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    is_error,
    error_message,
    request_text_preview,
    response_text_preview
FROM llm_call_logs
WHERE created_at >= now() - INTERVAL 7 DAY
ORDER BY created_at DESC;

-- ============================================================
-- 2. 模型 × 用户的总用量 (近 30 天)
-- ============================================================
CREATE OR REPLACE VIEW v_model_top_users AS
SELECT
    model,
    user_id,
    sum(request_count)                                                AS requests,
    sum(total_tokens)                                                 AS tokens,
    sum(error_count)                                                  AS errors,
    round(sum(total_duration_ms) / nullIf(sum(request_count), 0), 1)  AS avg_latency_ms
FROM llm_usage_hourly
WHERE hour >= now() - INTERVAL 30 DAY
GROUP BY model, user_id
ORDER BY requests DESC;

-- ============================================================
-- 3. Provider 实时健康指标 (近 1 小时滑动窗口)
-- ============================================================
CREATE OR REPLACE VIEW v_provider_health AS
SELECT
    provider,
    sum(request_count)                                                  AS requests_1h,
    sum(error_count)                                                    AS errors_1h,
    round(sum(error_count) * 100.0 / nullIf(sum(request_count), 0), 2)  AS error_rate_pct,
    round(sum(total_duration_ms) / nullIf(sum(request_count), 0), 1)    AS avg_latency_ms,
    round(sum(total_ttft_ms) / nullIf(sum(stream_count), 0), 1)         AS avg_ttft_ms,
    sum(total_tokens)                                                   AS tokens_1h
FROM llm_usage_hourly
WHERE hour >= now() - INTERVAL 1 HOUR
GROUP BY provider;

-- ============================================================
-- 4. 按日的 token 用量 (配合外部价格表计算金额)
-- ============================================================
CREATE OR REPLACE VIEW v_token_cost_daily AS
SELECT
    day,
    provider,
    model,
    sum(request_count)                            AS requests,
    sum(prompt_tokens)                            AS prompt_tokens,
    sum(completion_tokens)                        AS completion_tokens,
    sum(total_tokens)                             AS total_tokens,
    sum(request_bytes) + sum(response_bytes)     AS bytes_total
FROM llm_usage_daily
GROUP BY day, provider, model
ORDER BY day DESC, total_tokens DESC;

-- ============================================================
-- 5. 用户画像 (近 30 天, 按 user_id × client_type 聚合)
--
-- 数据量更大时可改造为 AggregatingMergeTree 物化视图.
-- ============================================================
CREATE OR REPLACE VIEW v_user_profiles AS
SELECT
    user_id,
    client_type,
    any(api_key_prefix)                   AS api_key_prefix,
    any(client_user_agent)                AS user_agent,
    count()                               AS total_requests,
    countIf(is_error)                     AS total_errors,
    sum(prompt_tokens)                    AS prompt_tokens,
    sum(completion_tokens)                AS completion_tokens,
    sum(total_tokens)                     AS total_tokens,
    sum(cached_tokens)                    AS cached_tokens,
    sum(reasoning_tokens)                 AS reasoning_tokens,
    sum(request_size_bytes)               AS request_bytes,
    sum(response_size_bytes)              AS response_bytes,
    groupUniqArrayIf(model, model != '')  AS models_used,
    groupUniqArray(provider)              AS providers_used,
    min(created_at)                       AS first_seen,
    max(created_at)                       AS last_seen,
    round(avg(duration_ms), 1)            AS avg_duration_ms
FROM llm_call_logs
WHERE created_at >= now() - INTERVAL 30 DAY
  AND user_id != ''
GROUP BY user_id, client_type;

-- ============================================================
-- 6. Claude Code 专属时间线 (高频查询预设)
-- ============================================================
CREATE OR REPLACE VIEW v_claude_code_traces AS
SELECT
    request_id,
    created_at,
    user_id,
    api_key_prefix,
    client_app_version,
    model,
    is_stream,
    request_message_count,
    request_has_tool,
    prompt_tokens,
    completion_tokens,
    total_tokens,
    cached_tokens,
    duration_ms,
    is_error,
    error_message,
    request_text_preview,
    response_text_preview
FROM llm_call_logs
WHERE client_type = 'claude_code'
  AND created_at >= now() - INTERVAL 30 DAY
ORDER BY created_at DESC;
