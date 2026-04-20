-- ============================================================
-- LLM Logger - 001_init.sql
-- 创建数据库
-- ============================================================

CREATE DATABASE IF NOT EXISTS llm_logger
    COMMENT 'LLM 上下文采集与归档系统';

-- 切换到目标数据库 (后续脚本使用 USE 切换)
USE llm_logger;

-- 记录 schema 版本表
CREATE TABLE IF NOT EXISTS schema_versions
(
    version     UInt32,
    description String,
    applied_at  DateTime DEFAULT now()
)
ENGINE = MergeTree
ORDER BY version;

INSERT INTO schema_versions (version, description) VALUES (1, 'init database');
