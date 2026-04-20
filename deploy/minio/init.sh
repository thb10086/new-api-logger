#!/bin/sh
# ============================================================
# MinIO 初始化脚本
# 创建 LLM Logger 所需的 buckets:
#   - llm-logs:        Gateway 上传的请求/响应 raw body (zstd 压缩)
#   - clickhouse-cold: ClickHouse cold tier 存储 (S3 disk)
# 由 Makefile init-minio 调用
# ============================================================
set -e

MINIO_ENDPOINT="${MINIO_ENDPOINT:-http://localhost:9000}"
MINIO_USER="${MINIO_ROOT_USER:-minioadmin}"
MINIO_PASS="${MINIO_ROOT_PASSWORD:-minioadmin}"

echo "==> 配置 mc alias..."
mc alias set local "${MINIO_ENDPOINT}" "${MINIO_USER}" "${MINIO_PASS}"

echo "==> 创建 bucket: llm-logs"
mc mb -p local/llm-logs || true

echo "==> 创建 bucket: clickhouse-cold"
mc mb -p local/clickhouse-cold || true

echo "==> 设置 versioning (生产建议开启)..."
mc version enable local/llm-logs || echo "  (skip) versioning 不可用"

echo ""
echo "============================================================"
echo "MinIO 初始化完成"
echo "  Console: http://localhost:9001  (${MINIO_USER} / ${MINIO_PASS})"
echo ""
echo "  buckets:"
mc ls local/ | sed 's/^/    /'
echo ""
echo "  生产环境建议在控制台配置生命周期规则:"
echo "    - llm-logs/ 90天 → 低频存储, 365天 → 删除"
echo "============================================================"
