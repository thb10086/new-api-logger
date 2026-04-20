# LLM Logger - Admin UI

最小化的管理控制台,基于 Vite + React 18 + TypeScript + TailwindCSS.

## 功能

- 请求 traces 列表 (按 provider / model / user_id / 错误状态过滤)
- 单 trace 详情 (含 raw JSON 拉取)
- 用量统计 (按日 / 按小时聚合, KPI 总览)
- 实时健康看板 (provider 维度,30s 自动刷新)

## 启动 (本地开发)

```bash
# 安装依赖
npm install

# 开发模式 (默认 http://localhost:5173)
npm run dev

# 后端 query-api 默认在 http://localhost:8000, 已通过 vite proxy 转发
```

首次进入会要求输入 Query API Token (对应 `LLMLOG_API_TOKEN`).

## 构建

```bash
npm run build      # 输出到 dist/
npm run preview    # 本地预览构建产物
```

## Docker

```bash
docker build -t llm-logger-admin-ui .
docker run -p 8080:80 llm-logger-admin-ui
```

容器内 Nginx 会把 `/api/*` 反代到 `query-api:8000` (在 docker-compose 网络下生效).

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `VITE_API_BASE` | `http://localhost:8000` | 开发模式下 vite proxy 的目标 |

## 目录结构

```
src/
├── api/client.ts       # Query API client + 类型
├── components/         # Layout / TokenGate / Badge
├── pages/              # 4 个页面: Traces / TraceDetail / Stats / Health
├── lib/utils.ts        # 工具函数
├── App.tsx
├── main.tsx
└── index.css
```
