# 部署指南

本文档描述 LLM Logger 在**本地开发**和**生产服务器**两种场景下的部署方式.

---

## 0. 一键部署 (推荐)

已有 Linux 服务器 (Ubuntu 20+/Debian 11+/CentOS 8+) 直接一键跑:

```bash
# SSH 到服务器后
curl -fsSL https://raw.githubusercontent.com/<your-org>/llm-logger/main/deploy/scripts/server-install.sh \
  | sudo bash
```

脚本会自动:

1. 安装 Docker + Compose plugin
2. Clone 仓库到 `/opt/llm-logger`
3. 生成随机密码的 `.env`
4. 构建并启动全部 5 个服务
5. 应用 CH schema + 创建 MinIO buckets
6. 打印 Admin UI 地址 + 登录 Token

**或本地已 clone**:

```bash
cd llm-logger
sudo bash deploy/scripts/server-install.sh
```

---

## 1. 本地开发

### 1.1 前置依赖

- **Docker 23+** (含 docker compose plugin)
- **Go 1.22+** (仅本地 `go run` 模式需要)
- **Python 3.11+** (建议用 `uv`,仅本地 `uv run` 模式需要)
- **Node 20+** (仅前端开发需要)
- **GNU make** (Windows 推荐 git bash / WSL)

### 1.2 方式 A - 全栈容器 (最省事)

```bash
cp .env.example .env
make deploy          # 一键: build + up + migrate + init-minio
```

完成后访问:

- Admin UI: <http://localhost:8888>
- Gateway: <http://localhost:8080>
- MinIO Console: <http://localhost:9001> (minioadmin/minioadmin)

### 1.3 方式 B - 基础设施 + 本地热重载

适合改 Gateway / Query API / Admin UI 源码时快速迭代:

```bash
cp .env.example .env
make up-deps                  # 只起 CH + MinIO
make migrate && make init-minio

# 开 3 个终端
make gateway-run              # Go: 改完 Ctrl+C 重启
make query-api-install        # 首次
make query-api-run            # Python: --reload 热加载
make admin-ui-install         # 首次
make admin-ui-dev             # Vite: HMR 实时
```

### 1.4 验证链路

```bash
# 1. 发一条测试请求穿过 Gateway (若 newapi 未起会 502, Gateway 仍会采集错误 trace)
curl -N http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-test" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"hi"}]}'

# 2. 浏览器打开 http://localhost:8888 输入 .env 中的 LLMLOG_API_TOKEN 登录
#    应看到刚才的 trace

# 3. 或直接 curl Query API (需与 admin-ui 同 token)
curl -H "Authorization: Bearer $(grep LLMLOG_API_TOKEN .env | cut -d= -f2)" \
  http://localhost:8000/api/v1/traces?limit=5 | jq
```

### 1.5 常用调试命令

```bash
make status          # docker compose ps
make logs            # 全部服务日志
make logs-gateway    # 只看 gateway
make ch-shell        # 进 ClickHouse SQL 交互
# SQL: SELECT count(), max(created_at) FROM llm_call_logs;

curl http://localhost:9090/metrics          # Gateway Prometheus 指标
go tool pprof http://localhost:9090/debug/pprof/profile  # pprof CPU
```

---

## 2. 生产部署

### 2.1 部署拓扑 (单节点快速版)

```text
                  [公网]
                     │ HTTPS (443)
                     ▼
              ┌───────────────┐
              │  Caddy/Nginx  │   TLS 终止 + 域名分发
              └───┬───────┬───┘
                  │       │
      admin.xx.com│       │api.xx.com (gateway)
                  ▼       ▼
         ┌──────────┐  ┌──────────┐
         │ admin-ui │  │ gateway  │
         │ :8888    │  │ :8080    │
         └──────┬───┘  └────┬─────┘
                │           │ 反代到 newapi
                │           ▼
         ┌──────┴───┐  ┌──────────┐
         │query-api │  │  newapi  │
         │ :8000    │  │ :3000    │
         └──────┬───┘  └──────────┘
                │
                ▼
        ┌────────────────┐
        │ clickhouse     │  (127.0.0.1 only)
        │ + minio/COS    │
        └────────────────┘
```

### 2.2 生产启动

```bash
cd /opt/llm-logger
cp .env.prod.example .env
# 必改: LLMLOG_UPSTREAM_URL / 所有 CHANGE_ME 密码字段
# 推荐: openssl rand -hex 32 生成随机值
vim .env

make deploy-prod     # 使用 docker-compose.prod.yml override
# 等效: docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

`docker-compose.prod.yml` 相较默认的变化:

- ClickHouse / MinIO / Query API 全部绑 127.0.0.1,**不对公网暴露**
- 容器加资源上限 + 日志滚动 (50MB x 5)
- Gateway 默认并发调到 16 workers / batch 500
- 强制 `restart: unless-stopped`

### 2.3 newapi 上游连接的 3 种模式

Gateway 在 docker-compose 中需要能访问 newapi. 根据 newapi 部署形态选其一:

#### 模式 1 — newapi 独立容器 + 宿主机端口映射 (默认, 最简单)

newapi 的 compose 已有:

```yaml
services:
  newapi:
    ports:
      - "3000:3000"
```

**llm-logger `.env` 配置**:

```bash
LLMLOG_UPSTREAM_URL=http://host.docker.internal:3000
```

`extra_hosts: host.docker.internal:host-gateway` 已在 `docker-compose.yml` 配好,Linux 也能用.

#### 模式 2 — newapi 与 llm-logger 共享 Docker network (推荐,更安全)

**优势**: 流量不出宿主机,newapi 不用暴露 3000 端口到公网.

**操作**:

1. 查 newapi 的 network 名:`docker network ls | grep newapi`
2. 修改 `docker-compose.yml` 给 gateway 加上 newapi 的外部 network:

    ```yaml
    services:
      gateway:
        networks:
          - llmlog
          - newapi_default   # newapi 的 network 名
    networks:
      newapi_default:
        external: true
    ```

3. `.env`:

    ```bash
    LLMLOG_UPSTREAM_URL=http://newapi:3000   # newapi 的 container name / hostname
    ```

#### 模式 3 — Gateway 共享宿主机网络 (极端场景)

**适用**: newapi 跑在宿主机非 docker,或已有复杂 iptables 规则.

**操作**: 取消注释 `docker-compose.yml` 中 gateway 的 `network_mode: host`,同时**注释掉该 service 的 `ports` 和 `networks` 段**(host 模式下互斥).

```bash
LLMLOG_UPSTREAM_URL=http://localhost:3000
```

### 2.4 暴露 Admin UI 面板

默认面板监听 `http://<server-ip>:8888`.改端口:

```bash
# .env
ADMIN_UI_PORT=80          # 直接 80 端口
```

**公网暴露建议**: 前置一层 Caddy 自动 HTTPS:

```caddy
# /etc/caddy/Caddyfile
admin.yourdomain.com {
    reverse_proxy localhost:8888
}

api.yourdomain.com {
    reverse_proxy localhost:8080
}
```

随后 LLM 客户端 `base_url` 填 `https://api.yourdomain.com/v1`,浏览器访问 `https://admin.yourdomain.com` 登录面板.

### 2.5 防火墙白名单

```bash
# UFW 示例 (其他防火墙类似)
ufw allow 22/tcp          # SSH
ufw allow 443/tcp         # Caddy HTTPS
ufw allow 80/tcp          # Caddy HTTP -> HTTPS 跳转
# Gateway / Admin UI / CH / MinIO 均不对公网开放, 仅 Caddy 内部反代
ufw enable
```

### 2.6 并发调优参数

根据你的 VPS 规格调整 `.env`:

| 参数 | 默认 | 中型 VPS (4C/8G) | 大型 VPS (16C/32G) | 说明 |
|---|---|---|---|---|
| `LLMLOG_CAPTURE_WORKERS` | 4 | 16 | 64 | 采集处理协程数 |
| `LLMLOG_CAPTURE_BUFFER` | 10000 | 20000 | 100000 | 采集队列长度 |
| `LLMLOG_CH_BATCH_SIZE` | 200 | 500 | 1000 | CH 批量写大小 |
| `LLMLOG_CH_BATCH_INTERVAL` | 500ms | 300ms | 200ms | CH 批量写周期 |
| `LLMLOG_COS_OFFLOAD_THRESHOLD` | 32768 | 32768 | 32768 | 超过此大小转 COS |
| `CH_MEM_LIMIT` | 6g | 6g | 24g | (prod.yml) CH 内存 |

**实测吞吐参考** (4 核 8G VPS, Gateway 单实例):

- 非流式请求 (100ms): ~3000 QPS
- SSE 流式 (5s, 20 chunks/s): ~500-1000 并发流
- CH 写入: ~5000 records/s (async_insert)

达到 **10k+ QPS** 需要 Gateway 水平扩容(见 §2.7).

### 2.7 水平扩容 (高流量场景)

Gateway 是**无状态**的,可直接水平扩展:

1. 在多台 VPS / 多个容器各跑一个 Gateway,共享同一个 ClickHouse
2. 前置 Nginx/HAProxy/Caddy 做 L7 负载均衡
3. `LLMLOG_UPSTREAM_URL` 指向内网 newapi (或用 K8s Service hostname)

**扩容要点**:

- 客户端的 SSE 长连接有粘滞性,LB 用 least_conn 或 random 即可
- ClickHouse 单机足以承载 10-30k writes/s,写入不会成瓶颈
- 若 newapi 成为瓶颈,独立扩容 newapi 即可

### 2.8 ClickHouse 进阶 (大规模)

默认 docker-compose 的 CH 单节点在 **1C/2G ~ 8C/32G** 之间都能跑,日 1000 万请求无压力.
若需要更高规模:

**数据卷规划**:

- 挂载独立 NVMe 做 `hot` 盘
- 大容量 SSD 做 `warm` 盘
- `deploy/clickhouse/config.d/storage.xml` 中的 `<cold>` disk 改为生产 S3:

  ```xml
  <cold>
    <type>s3</type>
    <endpoint>https://cos.ap-guangzhou.myqcloud.com/your-cold-bucket/</endpoint>
    <access_key_id>AKIDxxxxx</access_key_id>
    <secret_access_key>xxxxx</secret_access_key>
  </cold>
  ```

**内存/IO 调优**:

- `max_server_memory_usage` 默认即系统内存 90%
- `background_pool_size` 推荐 16+
- 日志级别 `<level>warning</level>` 降噪

**集群 (亿级以上日请求)**:

- 至少 3 节点 + 2 副本 (推荐 ClickHouse Keeper 替代 ZooKeeper)
- 主表改用 `ReplicatedMergeTree`,上层加 `Distributed` 引擎
- 分片键:`cityHash64(user_id)` 保证用户时间线在单分片内
- 详见 [ClickHouse 集群部署官方文档](https://clickhouse.com/docs/en/architecture/cluster-deployment)

### 2.9 对象存储选型

默认用 MinIO 便于本地开发.生产推荐:

- **腾讯云 COS** (国内业务,与 CVM 同 region 内网免流量费)
- **AWS S3** (海外)
- **Cloudflare R2** (零出网费,适合查询频繁场景)

在 `.env` 里切换到 COS 只需改 4 个变量 (见 `.env.prod.example` 注释段).

bucket 配置要点:

- 开启版本控制 (防误删)
- 配置生命周期规则:
  - 0-90d: 标准存储
  - 91-365d: 低频存储
  - 365d 后: 删除
- CORS 仅在 Admin UI 直接拉取 COS 时需要 (默认通过 Query API 代理,不需要)

### 2.10 K8s 部署

构建镜像:

```bash
make gateway-image
docker tag prismai/llm-logger-gateway:latest your-registry/llm-logger-gateway:v0.1.0
docker push your-registry/llm-logger-gateway:v0.1.0
# query-api / admin-ui 同理
```

K8s Gateway Deployment 关键配置:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: llm-logger-gateway
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: gateway
          image: your-registry/llm-logger-gateway:v0.1.0
          ports:
            - containerPort: 8080
              name: http
            - containerPort: 9090
              name: metrics
          env:
            - name: LLMLOG_UPSTREAM_URL
              value: http://newapi-svc:3000
            - name: LLMLOG_CH_HOST
              value: clickhouse-svc
            # 其他环境变量从 Secret/ConfigMap 注入
          resources:
            requests:
              cpu: 500m
              memory: 256Mi
            limits:
              cpu: 2000m
              memory: 1Gi
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8080
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8080
```

Query API / Admin UI 部署要点:

- Query API 副本数 1-2 即可 (查询负载轻)
- 必须设置 `LLMLOG_API_TOKEN` (强随机字符串)
- 建议内网域名,不直接对公网暴露 (或加 Cloudflare Access / OAuth2 代理)
- Admin UI 通过 `LLMLOG_ADMIN_API_BASE` 环境变量指定 Query API 地址,支持同域/跨域

### 2.11 监控告警

#### Prometheus 指标

Gateway 暴露 `/metrics` (默认 :9090), 包括:

- Go runtime 指标 (cpu / mem / goroutine / GC)
- (TODO) 业务指标: `llmlog_requests_total{provider,status}`, `llmlog_capture_dropped_total`

#### 关键告警规则建议

```yaml
groups:
  - name: llm-logger
    rules:
      - alert: CaptureQueueAlmostFull
        expr: llmlog_capture_queue_length / llmlog_capture_queue_cap > 0.8
        for: 5m
      - alert: CHSinkFailedHigh
        expr: rate(llmlog_ch_failed_records_total[5m]) > 100
        for: 5m
      - alert: GatewayP99LatencyHigh
        expr: histogram_quantile(0.99, llmlog_proxy_overhead_ms) > 10
        for: 10m
```

---

## 3. 客户端接入

把 newapi 的 base URL 改为 Gateway 地址即可, 鉴权 header 原样传递:

```python
# 之前
client = OpenAI(api_key="sk-xxx", base_url="https://your-newapi.com/v1")

# 之后
client = OpenAI(api_key="sk-xxx", base_url="https://llm-logger.yourcompany.com/v1")
```

Gateway 会:

- 透明转发到内部 newapi
- 每次请求注入响应 header `X-LLM-Logger-Request-Id`, 客户端可记录关联

---

## 4. 数据迁移与回填

### 历史 trace 回填

如有历史日志需导入, 可:

1. 写脚本读取历史日志 → 构造 `Record` JSON → POST 到 CH `INSERT INTO llm_call_logs FORMAT JSONEachRow`
2. 或用 `clickhouse-client --input-format=JSONEachRow ...`

### Schema 升级

- 所有 migration 在 `migrations/clickhouse/` 中按版本号编号
- 在 `schema_versions` 表中记录已应用版本
- 升级流程:
  1. `make ch-shell` 检查当前 `MAX(version) FROM schema_versions`
  2. 应用更高版本号的 SQL
  3. 在线 DDL: `ALTER TABLE ... ADD COLUMN ...` 不影响读写

---

## 5. 故障排查

| 现象 | 可能原因 | 排查命令 |
|------|---------|---------|
| `ready` 返回 503 | 队列接近满 | `curl :9090/metrics \| grep capture_queue` |
| CH 没数据 | sink 写入失败 | 看 Gateway 日志 `CH 批量写入最终失败` |
| COS 上传失败 | 凭证/权限/网络 | Gateway 日志 `COS 上传失败`, 检查 IAM |
| SSE 客户端卡住 | flusher 没生效 | 确保上游 Content-Type 含 `text/event-stream` |
| 内存 OOM | body 过大 | 调小 `LLMLOG_MAX_BODY_BYTES` |
