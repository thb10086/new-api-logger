-- ============================================================
-- LLM Logger - 005_client_type.sql
--
-- 增强客户端识别能力, 支持 Claude Code / 各类 SDK / 浏览器区分:
--   1. llm_call_logs 加 client_type + client_app_version 字段
--   2. 加 set(20) 索引加速按客户端类型过滤
--   3. 新增用户画像逻辑视图 v_user_profiles
--
-- 使用 IF NOT EXISTS 保证幂等, 可多次执行
-- ============================================================

USE llm_logger;

-- ============ 1. 主表加字段 ============
-- 注: LowCardinality + ZSTD 适合枚举型小字符串, 压缩率极高 (<50 字节/行)
ALTER TABLE llm_call_logs
    ADD COLUMN IF NOT EXISTS client_type LowCardinality(String) DEFAULT ''
        CODEC(ZSTD(3)) AFTER client_user_agent;

ALTER TABLE llm_call_logs
    ADD COLUMN IF NOT EXISTS client_app_version LowCardinality(String) DEFAULT ''
        CODEC(ZSTD(3)) AFTER client_type;

-- ============ 2. 加索引 (加速按客户端类型过滤) ============
-- set(20) 适合低基数枚举 (claude_code/openai_sdk/cherry_studio/browser/unknown 等)
ALTER TABLE llm_call_logs
    ADD INDEX IF NOT EXISTS idx_client_type (client_type) TYPE set(20) GRANULARITY 4;

-- 若需要对历史数据生效, 手动执行 (耗时, 可选):
--   ALTER TABLE llm_call_logs MATERIALIZE INDEX idx_client_type;

-- ============ 3. 用户画像逻辑视图 ============
-- 直接查主表 (7-30 天时间窗, 走主键 + bloom filter 毫秒级),
-- 数据量更大时再迁移到物化视图.
CREATE OR REPLACE VIEW v_user_profiles AS
SELECT
    user_id,
    client_type,
    any(api_key_prefix)                  AS api_key_prefix,
    any(client_user_agent)               AS user_agent,
    count()                              AS total_requests,
    countIf(is_error)                    AS total_errors,
    sum(prompt_tokens)                   AS prompt_tokens,
    sum(completion_tokens)               AS completion_tokens,
    sum(total_tokens)                    AS total_tokens,
    sum(cached_tokens)                   AS cached_tokens,
    sum(reasoning_tokens)                AS reasoning_tokens,
    sum(request_size_bytes)              AS request_bytes,
    sum(response_size_bytes)             AS response_bytes,
    groupUniqArrayIf(model, model != '') AS models_used,
    groupUniqArray(provider)             AS providers_used,
    min(created_at)                      AS first_seen,
    max(created_at)                      AS last_seen,
    round(avg(duration_ms), 1)           AS avg_duration_ms
FROM llm_call_logs
WHERE created_at >= now() - INTERVAL 30 DAY
  AND user_id != ''
GROUP BY user_id, client_type;

-- ============ 4. Claude Code 专属视图 (高频查询预设) ============
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

INSERT INTO schema_versions (version, description)
VALUES (5, 'add client_type + client_app_version + user profile views');
