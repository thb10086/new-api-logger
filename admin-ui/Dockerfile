## ============================================================
## Admin UI - 多阶段构建 (Vite build → Nginx serve)
## 兼容 Docker <23 (不使用 heredoc COPY)
## ============================================================

## ------- Stage 1: builder -------
FROM node:20-alpine AS builder

WORKDIR /app

# 优先拷贝依赖描述文件以利用 layer cache
COPY package.json package-lock.json* ./
RUN npm install --no-audit --no-fund

COPY . .
RUN npm run build

## ------- Stage 2: runtime (nginx) -------
FROM nginx:1.27-alpine

# 安装 nginx 配置
COPY nginx.conf /etc/nginx/conf.d/default.conf

# 前端静态资源
COPY --from=builder /app/dist /usr/share/nginx/html

# 运行时配置注入脚本 (支持 LLMLOG_ADMIN_API_BASE 环境变量)
COPY docker-entrypoint.sh /docker-entrypoint.d/40-inject-config.sh
RUN chmod +x /docker-entrypoint.d/40-inject-config.sh

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1/nginx-health || exit 1
