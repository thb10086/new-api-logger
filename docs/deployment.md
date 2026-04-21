# 部署指南

全栈基于 Docker Compose 一次性部署. ClickHouse schema 与 MinIO bucket 由
compose 自动初始化, 无需手动执行任何 DDL / 建桶命令.

---

## 1. 前置依赖

| 依赖 | 最低版本 | 说明 |
| --- | --- | --- |
| Docker Engine | 24.0+ | 需支持 BuildKit 与 Compose v2 |
| Docker Compose | v2.20+ | 使用了 `ports: !override` 语法 |
| 服务器规格 | 2 CPU / 4 GB RAM / 40 GB 硬盘 | 最小值, 生产推荐 4C/8G 及以上 |
| 网络 | 能出站拉镜像 + 访问 newapi | 国内可用镜像加速器 |
| 操作系统 | Linux 任意发行版 | 推荐 Debian 12 / Ubuntu 22.04 / OpenCloudOS 9 |

---

## 2. 快速部署

### 2.1 一键服务器安装 (推荐)

```bash
curl -fsSL https://raw.githubusercontent.com/thb10086/new-api-logger/main/deploy/scripts/server-install.sh \
  | sudo bash
```

脚本自动:

1. 检测并安装 Docker / Docker Compose.
2. Clone 仓库到 `/opt/new-api-logger`.
3. 从 `.env.prod.example` 生成 `.env`, 用 `openssl rand` 填充所有密码.
4. `docker compose -f docker-compose.yml -f docker-compose.prod.yml build` + `up -d`.
5. ClickHouse schema + MinIO bucket 自动初始化.
6. 打印面板地址、代理地址、登录 Token.

### 2.2 手动 Docker Compose 部署

```bash
# 1. Clone
git clone https://github.com/thb10086/new-api-logger.git /opt/new-api-logger
cd /opt/new-api-logger

# 2. 生成生产 .env
cp .env.prod.example .env
sed -i "s|CHANGE_ME_openssl_rand_hex_32|$(openssl rand -hex 32)|g" .env
sed -i "s|CHANGE_ME_minio_access_key|$(openssl rand -hex 16)|" .env
sed -i "s|CHANGE_ME_minio_secret_key_at_least_40_chars|$(openssl rand -hex 32)|" .env
sed -i "s|^LLMLOG_API_TOKEN=.*|LLMLOG_API_TOKEN=$(openssl rand -hex 32)|" .env
chmod 600 .env

# 3. 配置 newapi 上游地址 (见 §3.3 三种模式)
sed -i "s|^LLMLOG_UPSTREAM_URL=.*|LLMLOG_UPSTREAM_URL=http://host.docker.internal:3000|" .env

# 4. 加载 .env 到 shell (compose v2.24+ 推荐)
set -a; source .env; set +a

# 5. 构建并启动全栈
docker compose -f docker-compose.yml -f docker-compose.prod.yml build
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d

# 6. 查看 6 个容器状态
docker compose ps
```

**预期结果**:

| 容器 | 状态 |
| --- | --- |
| `llmlog-clickhouse` | Up (healthy) |
| `llmlog-minio` | Up (healthy) |
| `llmlog-minio-init` | Exited (0) |
| `llmlog-gateway` | Up |
| `llmlog-query-api` | Up |
| `llmlog-admin-ui` | Up |

`minio-init` 是一次性初始化容器, 执行完 bucket 创建后退出是**正常状态**.

---

## 3. 配置说明

### 3.1 环境变量

所有配置集中在 `.env`, 从 `.env.prod.example` 拷贝修改. 核心字段:

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `LLMLOG_UPSTREAM_URL` | `http://host.docker.internal:3000` | newapi 上游地址, 见 §3.3 |
| `LLMLOG_UPSTREAM_TIMEOUT` | `600s` | 上游请求超时 |
| `LLMLOG_CH_DATABASE` | `llm_logger` | ClickHouse 数据库名 |
| `LLMLOG_CH_USER` | `default` | ClickHouse 用户 |
| `LLMLOG_CH_PASSWORD` | (必填) | ClickHouse 密码 |
| `LLMLOG_S3_BUCKET` | `llm-logs` | 对象存储 bucket 名 |
| `LLMLOG_S3_ACCESS_KEY` | (必填) | S3 access key |
| `LLMLOG_S3_SECRET_KEY` | (必填) | S3 secret key |
| `LLMLOG_API_TOKEN` | (必填) | Admin UI 登录 Token |
| `LLMLOG_API_CORS_ORIGINS` | `*` | 生产应限定为实际面板域名 |
| `LLMLOG_CAPTURE_WORKERS` | `8` | 采集并发 worker 数 |
| `LLMLOG_CH_BATCH_SIZE` | `500` | ClickHouse 批量写入条数 |
| `LLMLOG_CH_BATCH_INTERVAL` | `300ms` | ClickHouse 批量写入最大延迟 |
| `LLMLOG_DISK_SPOOL_ENABLED` | `true` | CH 不可用时落盘兜底 (零丢失保证) |
| `LLMLOG_DISK_SPOOL_DIR` | `/var/lib/llmlog/spool` | 兜底队列本地路径 (容器内, 已挂 volume) |
| `LLMLOG_DISK_SPOOL_MAX_BYTES` | `10737418240` | 兜底队列最大磁盘占用 (默认 10 GB) |
| `LLMLOG_DISK_SPOOL_INTERVAL` | `30s` | 重放扫描周期 |

### 3.2 端口配置

生产默认使用 **28xxx / 29xxx 段**, 避开常见服务端口:

| 变量 | 默认 | 暴露 | 说明 |
| --- | --- | --- | --- |
| `GATEWAY_PORT` | `28080` | 公网 | Gateway 代理入口 (Caddy 反代此端口) |
| `ADMIN_UI_PORT` | `28888` | 公网 | 管理面板 |
| `CH_HTTP_PORT` | `28123` | `127.0.0.1` | ClickHouse HTTP |
| `CH_TCP_PORT` | `29000` | `127.0.0.1` | ClickHouse Native TCP |
| `MINIO_API_PORT` | `29100` | `127.0.0.1` | MinIO S3 API |
| `MINIO_CONSOLE_PORT` | `29001` | `127.0.0.1` | MinIO Web Console |
| `QUERY_API_PORT` | `28000` | 不暴露 | admin-ui 内 nginx 反代 `/api/*` |
| `GATEWAY_METRICS_PORT` | `29090` | `127.0.0.1` | Prometheus 抓取指标 |

`docker-compose.prod.yml` 把非对外端口绑定到 `127.0.0.1`, 只有 `GATEWAY_PORT` / `ADMIN_UI_PORT` 开放到 `0.0.0.0`.

### 3.3 newapi 上游连接三种模式

gateway 全量透传 newapi 的所有请求 (前端页面 + API + 静态资源), 只有 4 个 LLM 路径被采集.
newapi 端口不变, gateway 使用独立端口, 通过 Caddy 反代后用户通过域名访问 gateway.

```text
用户 -> https://domain -> Caddy -> gateway:28080 -> newapi:3000
                                      |
                                      v (异步旁路)
                                   ClickHouse
```

**模式 A — 走宿主机 (默认推荐)**

```bash
LLMLOG_UPSTREAM_URL=http://host.docker.internal:3000
```

- newapi 端口不变, gateway 通过宿主机网络连接
- 依赖 compose 里已配置的 `extra_hosts: host.docker.internal:host-gateway`, Linux 下也可用
- 流量多一跳, 单请求额外开销 < 1ms 可忽略

**模式 B — 加入 newapi 的 docker 网络**

在 `docker-compose.yml` 的 gateway 服务加:

```yaml
networks:
  - llmlog
  - newapi_default
```

并在文件末尾声明外部网络:

```yaml
networks:
  newapi_default:
    external: true
```

`.env` 改:

```bash
LLMLOG_UPSTREAM_URL=http://new-api:3000
```

此时 newapi 可以关掉宿主端口映射, 只对内暴露, 安全性更高.

**模式 C — Gateway 共享宿主机网络**

```yaml
gateway:
  network_mode: host
```

注释掉 `ports` 与 `networks` 段. 适合需要极低延迟或绑定特定网卡的场景.

---

## 4. 自动初始化机制

### 4.1 ClickHouse Schema

`docker-compose.yml` 把 `./migrations/clickhouse/` 挂载为 ClickHouse 容器的
`/docker-entrypoint-initdb.d`. 官方镜像在**数据目录为空**时会按字典序执行该目录
下所有 `.sql`:

```text
migrations/clickhouse/
├── 01_schema.sql              # 建库 + 主表 llm_call_logs + 所有索引 + 存储策略
├── 02_materialized_views.sql  # 3 个 MV (hourly / daily / errors)
└── 03_views.sql               # 6 个逻辑视图 (含 v_user_profiles, v_claude_code_traces)
```

所有 DDL 使用 `CREATE ... IF NOT EXISTS` / `CREATE OR REPLACE`, 重复执行无副作用. 数据卷
`clickhouse_data` 已存在时不重跑 (由 entrypoint 机制决定).

### 4.2 MinIO Bucket

`docker-compose.yml` 定义了一次性初始化服务 `minio-init`, 使用官方 `minio/mc` 镜像:

1. 等 `minio` 容器健康.
2. `mc alias set` 连接.
3. `mc mb --ignore-existing` 创建 `llm-logs` 和 `clickhouse-cold` 两个 bucket.
4. 打印日志后自动退出.

`gateway` 和 `query-api` 的 `depends_on` 声明了
`minio-init: service_completed_successfully`, 保证 bucket 就绪后才启动业务服务,
避免首个请求上传失败.

### 4.3 查看初始化日志

```bash
# ClickHouse schema 应用记录
docker compose logs clickhouse | grep -i "docker-entrypoint-initdb\|Running"

# Bucket 创建记录
docker compose logs minio-init

# 预期输出
# >> [minio-init] connecting to http://minio:9000
# >> [minio-init] creating bucket: llm-logs
# >> [minio-init] creating bucket: clickhouse-cold
# >> [minio-init] buckets ready, exiting.
```

---

## 5. 运维操作

### 5.1 启停与重启

```bash
# 加载环境变量到 shell (生产强烈推荐)
set -a; source .env; set +a

# 启动 / 强制重建镜像
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build

# 停止
docker compose -f docker-compose.yml -f docker-compose.prod.yml stop

# 停止 + 删容器 (保留数据卷)
docker compose -f docker-compose.yml -f docker-compose.prod.yml down

# 停止 + 删容器 + 删数据卷 (危险)
docker compose -f docker-compose.yml -f docker-compose.prod.yml down -v

# 重启单个服务
docker compose restart gateway
```

### 5.2 日志查看

```bash
docker compose logs -f                    # 聚合日志
docker compose logs -f gateway            # 单服务
docker compose logs --tail=200 clickhouse # 查最近 N 行
docker compose logs --since 10m gateway   # 按时间过滤
```

### 5.3 备份 / 恢复

**ClickHouse**:

```bash
# ClickHouse BACKUP 命令
docker compose exec -T clickhouse clickhouse-client --query \
  "BACKUP DATABASE llm_logger TO Disk('backups', 'llm_logger.zip')"

# 或直接快照数据卷
docker run --rm -v new-api-logger_clickhouse_data:/src -v "$(pwd)/backups:/dst" \
  alpine tar czf /dst/ch-$(date +%F).tar.gz -C /src .
```

**MinIO**:

```bash
docker compose exec -T minio sh -c "
  mc alias set local http://localhost:9000 \$MINIO_ROOT_USER \$MINIO_ROOT_PASSWORD &&
  mc mirror --overwrite local/llm-logs /data/backup
"
```

---

## 6. 生产最佳实践

### 6.1 域名 HTTPS 访问 (Caddy)

如果需要通过域名 + HTTPS 访问 (如 `https://api.yourdomain.com`), 推荐 Caddy.
Caddy 自动申请和续签 Let's Encrypt 证书, 零配置.

**安装 Caddy**:

```bash
apt install -y debian-keyring debian-archive-keyring apt-transport-https curl
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | tee /etc/apt/sources.list.d/caddy-stable.list
apt update && apt install caddy
```

**配置**: 复制仓库中的模板并替换域名:

```bash
cp deploy/caddy/Caddyfile /etc/caddy/Caddyfile
sed -i 's/api.yourdomain.com/你的实际域名/' /etc/caddy/Caddyfile
systemctl restart caddy
```

模板内容 (`deploy/caddy/Caddyfile`):

```text
api.yourdomain.com {
    reverse_proxy localhost:28080

    @streaming {
        path /v1/chat/completions
        path /v1/responses
        path /v1/messages
        path /v1beta/models/*
    }
    reverse_proxy @streaming localhost:28080 {
        flush_interval -1
    }
}
```

**流量路径**:

```text
客户端 -> https://api.yourdomain.com:443 -> Caddy (TLS) -> gateway:28080 -> newapi:3000
```

启用域名后, 防火墙只需放行 443, 可关闭 3000 的公网访问.
如果 Admin UI 也需要域名, 解注释 Caddyfile 中 `admin.yourdomain.com` 段.

### 6.2 资源限制

`docker-compose.prod.yml` 默认值适配 **2C / 4G** 的 VPS. 大机器请在 `.env` 覆盖:

| 机器规格 | `CH_CPU_LIMIT` | `CH_MEM_LIMIT` | `GATEWAY_CPU_LIMIT` | `GATEWAY_MEM_LIMIT` |
| --- | --- | --- | --- | --- |
| 2C / 4G | 1.5 | 2g | 0.8 | 512m |
| 4C / 8G | 2.5 | 4g | 1.5 | 1g |
| 8C / 16G | 4 | 8g | 2 | 2g |
| 16C / 32G+ | 8 | 16g | 4 | 4g |

**注意**: `cpus` 值必须**严格小于**主机物理核数, 否则 Docker 报
`Range of CPUs is from 0.01 to N.00`.

### 6.3 防火墙

只放行对外端口:

```bash
# ufw
ufw allow 28080/tcp
ufw allow 28888/tcp   # Admin UI

# firewalld
firewall-cmd --permanent --add-port=3000/tcp
firewall-cmd --permanent --add-port=28888/tcp   # Admin UI
firewall-cmd --reload
```

挂了 Caddy + TLS 后, 可以只放 443, 关闭 28080 的公网访问.

### 6.4 切换到腾讯 COS / AWS S3

生产强烈推荐用云厂商对象存储替代本地 MinIO. 修改 `.env`:

```bash
# 腾讯云 COS 示例
LLMLOG_S3_ENDPOINT=https://cos.ap-guangzhou.myqcloud.com
LLMLOG_S3_REGION=ap-guangzhou
LLMLOG_S3_BUCKET=llm-logs-1234567890
LLMLOG_S3_ACCESS_KEY=AKIDxxxxxxxxxxxx
LLMLOG_S3_SECRET_KEY=xxxxxxxxxxxxxx
LLMLOG_S3_USE_PATH_STYLE=false
```

然后注释掉 `docker-compose.yml` 的 `minio` 和 `minio-init` 服务块, `gateway` /
`query-api` 的 `depends_on` 里也移除对 minio 的依赖.

---

## 7. 故障排查

### 7.1 `docker compose up` 报端口占用但 `ss -tlnp` 查不到

**原因**: `docker-compose.yml` 的 `ports` 与 `docker-compose.prod.yml` 的 `ports`
被 compose v2 追加合并, 同端口被 bind 两次.

**解法**: `docker-compose.prod.yml` 的 `ports` 已加 `!override` 标签强制替换,
升级到 compose v2.20+ 即可生效.

### 7.2 `.env` 变量没被 compose 读取

**原因**: compose v2.24+ 在某些边界情况下不会把 `.env` 用作 YAML 插值.

**解法**: 启动前 `source .env` 到 shell, 优先级最高:

```bash
set -a
source .env
set +a
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

或封装为 `up.sh`:

```bash
#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
set -a
source .env
set +a
docker compose -f docker-compose.yml -f docker-compose.prod.yml "$@"
```

### 7.3 ClickHouse schema 没自动应用

**原因**: `docker-entrypoint-initdb.d` 只在 ClickHouse **数据目录为空**时执行.
如果之前启动过并留下卷, 不会重跑.

**解法**:

```bash
# 方案 A: 清掉数据卷重来 (仅限空表)
docker compose down -v
docker compose up -d

# 方案 B: 手动执行 (保留已有数据)
for f in migrations/clickhouse/*.sql; do
  docker compose exec -T clickhouse clickhouse-client --multiquery < "$f"
done
```

所有 DDL 都是 `CREATE ... IF NOT EXISTS` / `CREATE OR REPLACE`, 方案 B 不会损坏现有数据.

### 7.4 Gateway 连不到 newapi

```bash
# 1. 从 gateway 容器内测试
docker compose exec gateway wget -O- -T 3 "$LLMLOG_UPSTREAM_URL/health" 2>&1 | head

# 2. 看实际用的 URL
docker compose logs gateway | grep -i upstream

# 3. 模式 A 下确认 host.docker.internal 解析正常
docker compose exec gateway getent hosts host.docker.internal
```

对应方案见 §3.3.

### 7.5 Admin UI 登录后 API 报 401 / CORS

- **401**: `.env` 里的 `LLMLOG_API_TOKEN` 与前端输入的不一致
- **CORS**: `LLMLOG_API_CORS_ORIGINS` 没包含实际访问的域名
- **改后需重启 query-api**: `docker compose restart query-api`

### 7.6 磁盘兜底队列堆积 (spool backlog)

当 CH 长时间不可用时, gateway 会把 batch 写到 `/var/lib/llmlog/spool` 下的
NDJSON 文件, 待 CH 恢复后自动 replayer 回放.

```bash
# 查看队列堆积量 (容器内)
docker compose exec gateway sh -c \
  "du -sh /var/lib/llmlog/spool && ls /var/lib/llmlog/spool | wc -l"

# 看 replayer 日志
docker compose logs gateway | grep -Ei "spool|replayer"

# 强制触发 replay (通常 30s 自动扫描, 无需手动)
docker compose restart gateway
```

- 堆积到 `LLMLOG_DISK_SPOOL_MAX_BYTES` 时, 新 batch 被**丢弃**并告警, 建议监控容器 `/var/lib/llmlog/spool` 占用.
- 文件名形如 `batch-YYYYMMDD-HHMMSS-<n>.ndjson`, 可 `jq` 读取.
- 回放成功后文件自动删除.

### 7.7 用户画像页显示空 / "未识别"

- **空页面**: `GET /api/v1/users` 聚合条件是 `user_id != ''`. 如果客户端请求没带
  `X-Forwarded-User` 或 newapi 没把 api_key 映射到 user_id, 这里就是空的.
  - 验证: `SELECT count() FROM llm_call_logs WHERE user_id != ''`
  - 解法: 让客户端请求携带 `X-Forwarded-User: <id>` header, 或在 newapi 层面给
    每个 api_key 关联一个 `user_id`.
- **全是 "未识别"**: 客户端 UA 不在 `client_classify.go` 的识别清单中.
  - 打开 trace 详情查看 `client_user_agent` 原值.
  - 新增规则: 编辑 `gateway/internal/handlers/client_classify.go` 的 `ClassifyClient`
    函数加分支, 重启 gateway.

### 7.8 查询变慢 / CH part 数过多

长时间运行后 CH parts 过多会拖慢合并. 监控:

```bash
docker compose exec -T clickhouse clickhouse-client --query "
  SELECT table, count() AS parts, sum(rows) AS rows
  FROM system.parts
  WHERE database = 'llm_logger' AND active
  GROUP BY table
  ORDER BY parts DESC
"
```

单表 parts > 1000 或 TTL 迁移明显滞后时, 手动触发 merge:

```bash
docker compose exec -T clickhouse clickhouse-client --query \
  "OPTIMIZE TABLE llm_logger.llm_call_logs FINAL"
```

`OPTIMIZE ... FINAL` 是重操作, 建议低峰期执行.

---

## 8. Kubernetes 部署

仓库提供的是 Compose 部署. 迁移到 K8s 的推荐做法:

- `gateway` → Deployment + Service + HPA (按 CPU 自动扩容, 无状态无依赖)
- `query-api` → Deployment + Service
- `admin-ui` → Deployment + Service + Ingress
- `clickhouse` → 用官方 `clickhouse-operator` (生产推荐, 原生支持分片 + 备份)
- `minio` → 改用托管对象存储 (COS / S3), 无需在 K8s 内部署

所有 `LLMLOG_*` 环境变量放进 ConfigMap + Secret 即可.

---

## 9. 卸载

```bash
cd /opt/new-api-logger
docker compose -f docker-compose.yml -f docker-compose.prod.yml down -v
docker image rm \
  thb10086/new-api-logger-gateway:latest \
  thb10086/new-api-logger-query-api:latest \
  thb10086/new-api-logger-admin-ui:latest
cd .. && rm -rf /opt/new-api-logger
```

数据卷 `down -v` 已删除. 如有备份需求, 先执行 §5.3 再卸载.
