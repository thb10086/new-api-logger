-- ============================================================
-- LLM Logger - 004_helper_views.sql
--
-- 便捷查询的逻辑视图 (非物化, 仅 SQL alias):
--   1. v_user_recent_traces  - 按用户查最近请求 (常用)
--   2. v_model_top_users     - 某模型最活跃的用户
--   3. v_provider_health     - 各 provider 实时健康指标 (近 1 小时)
--   4. v_token_cost_daily    - 按日的 token 用量 (用于成本对账)
-- ============================================================

USE llm_logger;

-- ============================================================
-- 1. 用户最近请求 (默认近 7 天, 调用方自行 LIMIT)
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
    sum(request_count)            AS requests,
    sum(total_tokens)             AS tokens,
    sum(error_count)              AS errors,
    round(sum(total_duration_ms) / nullIf(sum(request_count), 0), 1) AS avg_latency_ms
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
    sum(request_count)                                                   AS requests_1h,
    sum(error_count)                                                     AS errors_1h,
    round(sum(error_count) * 100.0 / nullIf(sum(request_count), 0), 2)   AS error_rate_pct,
    round(sum(total_duration_ms) / nullIf(sum(request_count), 0), 1)     AS avg_latency_ms,
    round(sum(total_ttft_ms) / nullIf(sum(stream_count), 0), 1)          AS avg_ttft_ms,
    sum(total_tokens)                                                    AS tokens_1h
FROM llm_usage_hourly
WHERE hour >= now() - INTERVAL 1 HOUR
GROUP BY provider;

-- ============================================================
-- 4. 按日成本 (token 用量, 配合外部价格表计算金额)
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
    sum(request_bytes) + sum(response_bytes)      AS bytes_total
FROM llm_usage_daily
GROUP BY day, provider, model
ORDER BY day DESC, total_tokens DESC;

INSERT INTO schema_versions (version, description) VALUES (4, 'create helper logical views');
