-- ============================================================
-- LLM Logger - 002_main_table.sql
-- 主事实表 llm_call_logs - 存储每一次 LLM 请求的完整上下文
--
-- 设计要点:
--   - 列存 + ZSTD 压缩, 长文本压缩比可达 5-15 倍
--   - 主键: (toStartOfHour(created_at), user_id, request_id) 同时优化用户时间线和时间扫描
--   - 分区: PARTITION BY toYYYYMM(created_at)
--   - 三级 TTL: 7天热盘 → 90天温盘 → 365天后删除
--   - skip index: ngrambf_v1 全文初筛 + bloom_filter 等值查询加速
--   - 大 body 通过 cos_key 引用 COS, 小 body 内联 ZSTD(6) 压缩
-- ============================================================

USE llm_logger;

CREATE TABLE IF NOT EXISTS llm_call_logs
(
    -- ============ 标识 + 时间 ============
    request_id          String                              CODEC(ZSTD(3)),
    parent_request_id   String DEFAULT ''                   CODEC(ZSTD(3)),
    created_at          DateTime64(3, 'UTC')                CODEC(Delta, ZSTD(3)),
    completed_at        Nullable(DateTime64(3, 'UTC'))      CODEC(Delta, ZSTD(3)),

    -- ============ 客户端信息 ============
    client_ip           IPv6                                CODEC(ZSTD(3)),
    client_user_agent   LowCardinality(String)              CODEC(ZSTD(3)),

    -- 鉴权 (api_key 仅存 hash + 前后缀, 不存明文)
    api_key_hash        FixedString(64) DEFAULT ''          CODEC(ZSTD(3)),
    api_key_prefix      String DEFAULT ''                   CODEC(ZSTD(3)),

    -- 业务用户标识 (可由代理层从 header / token 解析填入)
    user_id             String DEFAULT ''                   CODEC(ZSTD(3)),
    organization_id     String DEFAULT ''                   CODEC(ZSTD(3)),

    -- ============ 协议和路径 ============
    -- provider: openai_chat | openai_responses | anthropic | gemini
    provider            LowCardinality(String)              CODEC(ZSTD(3)),
    endpoint            LowCardinality(String)              CODEC(ZSTD(3)),
    http_method         LowCardinality(String) DEFAULT 'POST',
    is_stream           Bool DEFAULT false,

    -- ============ 模型信息 ============
    model               LowCardinality(String)              CODEC(ZSTD(3)),
    model_normalized    LowCardinality(String) DEFAULT ''   CODEC(ZSTD(3)),

    -- ============ 请求结构元数据 ============
    request_message_count    UInt16 DEFAULT 0,
    request_has_image        Bool   DEFAULT false,
    request_has_audio        Bool   DEFAULT false,
    request_has_video        Bool   DEFAULT false,
    request_has_tool         Bool   DEFAULT false,
    request_temperature      Nullable(Float32),
    request_top_p            Nullable(Float32),
    request_max_tokens       Nullable(UInt32),
    request_size_bytes       UInt32 DEFAULT 0                 CODEC(ZSTD(3)),

    -- ============ 响应结构元数据 ============
    response_finish_reason   LowCardinality(String) DEFAULT '',
    response_size_bytes      UInt32 DEFAULT 0                 CODEC(ZSTD(3)),
    response_chunk_count     UInt32 DEFAULT 0,

    -- ============ Token 统计 ============
    prompt_tokens            UInt32 DEFAULT 0,
    completion_tokens        UInt32 DEFAULT 0,
    total_tokens             UInt32 DEFAULT 0,
    cached_tokens            UInt32 DEFAULT 0,
    reasoning_tokens         UInt32 DEFAULT 0,

    -- ============ 性能 ============
    ttft_ms                  UInt32 DEFAULT 0,
    duration_ms              UInt32 DEFAULT 0,
    upstream_status          UInt16 DEFAULT 0,

    -- ============ 错误信息 ============
    is_error                 Bool   DEFAULT false,
    error_type               LowCardinality(String) DEFAULT '',
    error_message            String DEFAULT ''                CODEC(ZSTD(3)),

    -- ============ 内容预览 (可检索, 前 2KB) ============
    request_text_preview     String DEFAULT ''                CODEC(ZSTD(3)),
    response_text_preview    String DEFAULT ''                CODEC(ZSTD(3)),

    -- ============ 原始 JSON (小 body 内联, ZSTD 高压缩) ============
    request_json             String DEFAULT ''                CODEC(ZSTD(6)),
    response_json            String DEFAULT ''                CODEC(ZSTD(6)),

    -- ============ COS 引用 (大 body 转存到对象存储) ============
    request_cos_key          String DEFAULT ''                CODEC(ZSTD(3)),
    response_cos_key         String DEFAULT ''                CODEC(ZSTD(3)),

    -- ============ 流式原始 chunks (可选, debug 用, 默认空) ============
    response_stream_chunks   String DEFAULT ''                CODEC(ZSTD(9)),

    -- ============ 工具调用 (JSON 数组) ============
    tool_calls_json          String DEFAULT ''                CODEC(ZSTD(6)),

    -- ============ 扩展字段 (JSON, 用于灵活扩展) ============
    metadata_json            String DEFAULT '{}'              CODEC(ZSTD(6)),

    -- ============ 索引 ============
    -- 全文初筛 (ngram bloom filter, 4-gram 适配中英文混合)
    INDEX idx_request_text  (request_text_preview)
        TYPE ngrambf_v1(4, 65536, 3, 0) GRANULARITY 4,
    INDEX idx_response_text (response_text_preview)
        TYPE ngrambf_v1(4, 65536, 3, 0) GRANULARITY 4,

    -- 等值查询加速
    INDEX idx_user           (user_id)        TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_api_key        (api_key_hash)   TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_org            (organization_id) TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_model          (model)          TYPE set(200)            GRANULARITY 4,
    INDEX idx_provider       (provider)       TYPE set(20)             GRANULARITY 4,

    -- 错误诊断
    INDEX idx_error          (is_error, upstream_status) TYPE minmax  GRANULARITY 4,

    -- 时间范围
    INDEX idx_duration       (duration_ms)    TYPE minmax              GRANULARITY 4
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (toStartOfHour(created_at), user_id, request_id)
PRIMARY KEY (toStartOfHour(created_at), user_id)
-- TTL 表达式要求结果为 DateTime/Date 类型, 不支持直接用 DateTime64
-- 所以用 toDateTime() 转换 (只影响 TTL 判断, 存储仍是毫秒精度)
TTL  toDateTime(created_at) + INTERVAL 365 DAY DELETE,
     toDateTime(created_at) + INTERVAL  90 DAY TO VOLUME 'cold',
     toDateTime(created_at) + INTERVAL   7 DAY TO VOLUME 'warm'
SETTINGS
    storage_policy = 'tiered',
    index_granularity = 8192,
    min_bytes_for_wide_part = 10485760,
    ttl_only_drop_parts = 1,
    merge_with_ttl_timeout = 86400;

-- 注: 若开发环境没有配置多盘 storage_policy, 可以临时改为:
--   SETTINGS storage_policy = 'default', ...
-- 并删除 TTL 中的 TO VOLUME 子句

INSERT INTO schema_versions (version, description) VALUES (2, 'create main table llm_call_logs');
