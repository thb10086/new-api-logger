-- ============================================================
-- LLM Logger - ClickHouse schema 初始化 (第 1 步: 库 + 主事实表)
--
-- 本目录下的 .sql 文件会在 ClickHouse 容器首次启动时, 由官方
-- 镜像按字典序执行 (docker-entrypoint-initdb.d), 完成一次性建库.
-- 数据卷已存在时不会重跑.
--
-- 设计要点 (llm_call_logs):
--   * 列存 + ZSTD 压缩, 长文本压缩比 5-15 倍.
--   * 主键 (toStartOfHour(created_at), user_id) — 同时优化按时间扫描
--     和按用户时间线检索.
--   * 排序键加入 request_id, 保证同 hour 内按 id 稳定排序.
--   * 分区 toYYYYMM(created_at), 冷热 TTL 迁移以月为粒度.
--   * 三段 TTL: 7d 热盘 (NVMe) → 90d 温盘 (SSD) → 365d 冷盘 (S3) → 删除.
--   * Skip index:
--       - ngrambf_v1(4, 65536, 3, 0) 用于 request / response 文本初筛 (中英文友好)
--       - bloom_filter 用于高基数等值过滤
--       - set(N) 用于低基数枚举
--       - minmax 用于数值范围过滤
--   * 大 body (> LLMLOG_COS_OFFLOAD_THRESHOLD, 默认 32KB) 由 gateway
--     上传到对象存储, 此处仅存 request_cos_key / response_cos_key.
-- ============================================================

CREATE DATABASE IF NOT EXISTS llm_logger
    COMMENT 'LLM 上下文采集与归档系统';

USE llm_logger;

CREATE TABLE IF NOT EXISTS llm_call_logs
(
    -- ============ 标识 + 时间 ============
    request_id              String                              CODEC(ZSTD(3)),
    parent_request_id       String DEFAULT ''                   CODEC(ZSTD(3)),
    created_at              DateTime64(3, 'UTC')                CODEC(Delta, ZSTD(3)),
    completed_at            Nullable(DateTime64(3, 'UTC'))      CODEC(Delta, ZSTD(3)),

    -- ============ 客户端 ============
    client_ip               IPv6                                CODEC(ZSTD(3)),
    client_user_agent       LowCardinality(String)              CODEC(ZSTD(3)),
    -- client_type 由 gateway 根据 UA 自动归类: claude_code / openai_sdk /
    -- anthropic_sdk / cherry_studio / chatbox / lobechat / cursor /
    -- continue / langchain / browser / curl / unknown.
    client_type             LowCardinality(String) DEFAULT ''   CODEC(ZSTD(3)),
    -- client_app_version 提取自 UA, 如 "claude-cli/0.2.17".
    client_app_version      LowCardinality(String) DEFAULT ''   CODEC(ZSTD(3)),

    -- 鉴权 (api_key 仅存 SHA-256 hash + 前缀, 不存明文)
    api_key_hash            FixedString(64) DEFAULT ''          CODEC(ZSTD(3)),
    api_key_prefix          String DEFAULT ''                   CODEC(ZSTD(3)),

    -- 业务用户标识 (由代理层从 header / token 解析填入)
    user_id                 String DEFAULT ''                   CODEC(ZSTD(3)),
    organization_id         String DEFAULT ''                   CODEC(ZSTD(3)),

    -- ============ 协议与路径 ============
    -- provider: openai_chat | openai_responses | anthropic | gemini
    provider                LowCardinality(String)              CODEC(ZSTD(3)),
    endpoint                LowCardinality(String)              CODEC(ZSTD(3)),
    http_method             LowCardinality(String) DEFAULT 'POST',
    is_stream               Bool DEFAULT false,

    -- ============ 模型 ============
    model                   LowCardinality(String)              CODEC(ZSTD(3)),
    model_normalized        LowCardinality(String) DEFAULT ''   CODEC(ZSTD(3)),

    -- ============ 请求结构元数据 ============
    request_message_count   UInt16 DEFAULT 0,
    request_has_image       Bool   DEFAULT false,
    request_has_audio       Bool   DEFAULT false,
    request_has_video       Bool   DEFAULT false,
    request_has_tool        Bool   DEFAULT false,
    request_temperature     Nullable(Float32),
    request_top_p           Nullable(Float32),
    request_max_tokens      Nullable(UInt32),
    request_size_bytes      UInt32 DEFAULT 0                    CODEC(ZSTD(3)),

    -- ============ 响应结构元数据 ============
    response_finish_reason  LowCardinality(String) DEFAULT '',
    response_size_bytes     UInt32 DEFAULT 0                    CODEC(ZSTD(3)),
    response_chunk_count    UInt32 DEFAULT 0,

    -- ============ Token 统计 ============
    prompt_tokens           UInt32 DEFAULT 0,
    completion_tokens       UInt32 DEFAULT 0,
    total_tokens            UInt32 DEFAULT 0,
    cached_tokens           UInt32 DEFAULT 0,
    reasoning_tokens        UInt32 DEFAULT 0,

    -- ============ 性能 ============
    ttft_ms                 UInt32 DEFAULT 0,
    duration_ms             UInt32 DEFAULT 0,
    upstream_status         UInt16 DEFAULT 0,

    -- ============ 错误 ============
    is_error                Bool   DEFAULT false,
    error_type              LowCardinality(String) DEFAULT '',
    error_message           String DEFAULT ''                   CODEC(ZSTD(3)),

    -- ============ 内容预览 (可检索, 前 2KB) ============
    request_text_preview    String DEFAULT ''                   CODEC(ZSTD(3)),
    response_text_preview   String DEFAULT ''                   CODEC(ZSTD(3)),

    -- ============ 原始 JSON (小 body 内联, 高压缩) ============
    request_json            String DEFAULT ''                   CODEC(ZSTD(6)),
    response_json           String DEFAULT ''                   CODEC(ZSTD(6)),

    -- ============ 对象存储引用 (大 body 转存到 COS/S3) ============
    request_cos_key         String DEFAULT ''                   CODEC(ZSTD(3)),
    response_cos_key        String DEFAULT ''                   CODEC(ZSTD(3)),

    -- ============ 流式原始 chunks (可选, 仅 debug, 默认空) ============
    response_stream_chunks  String DEFAULT ''                   CODEC(ZSTD(9)),

    -- ============ 工具调用 (JSON 数组) ============
    tool_calls_json         String DEFAULT ''                   CODEC(ZSTD(6)),

    -- ============ 扩展字段 (任意 JSON, 便于后续业务扩展) ============
    metadata_json           String DEFAULT '{}'                 CODEC(ZSTD(6)),

    -- ============ 索引 ============
    -- 文本初筛: 4-gram bloom filter 适配中英文混合
    INDEX idx_request_text   (request_text_preview)
        TYPE ngrambf_v1(4, 65536, 3, 0) GRANULARITY 4,
    INDEX idx_response_text  (response_text_preview)
        TYPE ngrambf_v1(4, 65536, 3, 0) GRANULARITY 4,

    -- 高基数等值过滤
    INDEX idx_user           (user_id)         TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_api_key        (api_key_hash)    TYPE bloom_filter(0.01) GRANULARITY 4,
    INDEX idx_org            (organization_id) TYPE bloom_filter(0.01) GRANULARITY 4,

    -- 低基数枚举
    INDEX idx_model          (model)           TYPE set(200) GRANULARITY 4,
    INDEX idx_provider       (provider)        TYPE set(20)  GRANULARITY 4,
    INDEX idx_client_type    (client_type)     TYPE set(20)  GRANULARITY 4,

    -- 错误诊断 + 延时范围
    INDEX idx_error          (is_error, upstream_status) TYPE minmax GRANULARITY 4,
    INDEX idx_duration       (duration_ms)     TYPE minmax   GRANULARITY 4
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(created_at)
ORDER BY (toStartOfHour(created_at), user_id, request_id)
PRIMARY KEY (toStartOfHour(created_at), user_id)
-- TTL 表达式要求 DateTime/Date 类型, 用 toDateTime() 转换
-- (只影响 TTL 判断, 存储仍是毫秒精度)
TTL  toDateTime(created_at) + INTERVAL 365 DAY DELETE,
     toDateTime(created_at) + INTERVAL  90 DAY TO VOLUME 'cold',
     toDateTime(created_at) + INTERVAL   7 DAY TO VOLUME 'warm'
SETTINGS
    storage_policy             = 'tiered',
    index_granularity          = 8192,
    min_bytes_for_wide_part    = 10485760,
    ttl_only_drop_parts        = 1,
    merge_with_ttl_timeout     = 86400;

-- 备注: 单盘开发环境可把 storage_policy 改为 'default' 并删掉 TTL 的
-- TO VOLUME 子句 (见 deploy/clickhouse/config.d/storage.xml).
