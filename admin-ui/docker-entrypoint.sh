#!/bin/sh
## ============================================================
## LLM Logger Admin UI - 容器启动脚本
##
## 作用: 启动时把运行时配置注入到 index.html, 避免重新 build 镜像
## 支持的环境变量:
##   LLMLOG_ADMIN_API_BASE      前端请求 API 的 base URL (默认: /api)
## ============================================================

set -eu

API_BASE="${LLMLOG_ADMIN_API_BASE:-/api}"

INDEX_FILE="/usr/share/nginx/html/index.html"

if [ -f "$INDEX_FILE" ]; then
  # 构造 JSON 配置 (转义反斜杠/双引号/斜杠以安全嵌入 sed)
  CONFIG_JSON=$(printf '{"apiBaseUrl":"%s"}' "$API_BASE")
  # 替换占位符 (使用 | 作为分隔符, 避免 URL 中的 / 冲突)
  sed -i "s|__LLMLOG_CONFIG_PLACEHOLDER__|${CONFIG_JSON}|g" "$INDEX_FILE"
  echo "[entrypoint] admin-ui 运行时配置已注入: apiBaseUrl=${API_BASE}"
else
  echo "[entrypoint] WARN: ${INDEX_FILE} 不存在, 跳过配置注入"
fi

exec "$@"
