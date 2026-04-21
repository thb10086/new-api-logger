# LLM Logger 部署指南

本文档覆盖从零到生产上线的完整部署流程,默认全栈基于 Docker Compose,schema 与 bucket 由 Compose 自动初始化。

---

## 1. 前置依赖

| 依赖 | 最低版本 | 说明 |
| --- | --- | --- |
| Docker Engine | 24.0+ | 需支持 BuildKit 与 Compose v2 |
| Docker Compose | v2.20+ | 使用 `ports: !override` 语法需要 v2.20+ |
| 服务器资源 | 2 CPU / 4 GB RAM / 40 GB 硬盘 | 最小规格,生产推荐 4C/8G 及以上 |
| 网络 | 能出站访问镜像仓库 + newapi | 可用国内镜像源加速 |
| 操作系统 | Linux (任意发行版) | 推荐 Debian 12 / Ubuntu 22.04 / OpenCloudOS 9 |

---

## 2. 快速部署

### 2.1 服务器一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/thb10086/new-api-logger/main/deploy/scripts/server-install.sh \
  | sudo bash
```

脚本行为:

1. 检测并安装 Docker / Docker Compose
2. Clone 仓库到 `/opt/llm-logger`
3. 从 `.env.prod.example` 生成 `.env`,用 `openssl rand` 随机填充所有密码
4. `docker compose build`
5. `docker compose up -d`(自动初始化 ClickHouse schema + MinIO bucket)
6. 打印面板地址、代理地址、登录 Token

### 2.2 手动 Docker Compose 部署

```bash
# 1. Clone
git clone https://github.com/thb10086/new-api-logger.git /opt/llm-logger
cd /opt/llm-logger

# 2. 生成生产 .env
cp .env.prod.example .env
sed -i "s|CHANGE_ME_openssl_rand_hex_32|$(openssl rand -hex 32)|g" .env
sed -i "s|CHANGE_ME_minio_access_key|$(openssl rand -hex 16)|" .env
sed -i "s|CHANGE_ME_minio_secret_key_at_least_40_chars|$(openssl rand -hex 32)|" .env
sed -i "s|^LLMLOG_API_TOKEN=.*|LLMLOG_API_TOKEN=$(openssl rand -hex 32)|" .env
chmod 600 .env

# 3. 配置 newapi 上游 (见 §3.3 三种模式)
sed -i "s|^LLMLOG_UPSTREAM_URL=.*|LLMLOG_UPSTREAM_URL=http://host.docker.internal:3000|" .env

# 4. 加载 .env 到 shell (compose v2.24+ 推荐)
set -a; source .env; set +a

# 5. 构建并启动全栈
docker compose -f docker-compose.yml -f docker-compose.prod.yml build
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d

# 6. 确认 6 个服务状态
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

`minio-init` 是**一次性初始化容器**,执行完 bucket 创建后退出是正常的。

---

## 3. 配置说明

### 3.1 环境变量

所有配置都集中在 `.env`,推荐从 `.env.prod.example` 复制修改。核心字段:

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `LLMLOG_UPSTREAM_URL` | `http://host.docker.internal:3000` | newapi 上游地址,见 §3.3 |
| `LLMLOG_UPSTREAM_TIMEOUT` | `600s` | 上游请求超时 |
| `LLMLOG_CH_DATABASE` | `llm_logger` | ClickHouse 数据库名 |
| `LLMLOG_CH_USER` | `default` | ClickHouse 用户 |
| `LLMLOG_CH_PASSWORD` | (必填) | ClickHouse 密码 |
| `LLMLOG_S3_BUCKET` | `llm-logs` | 对象存储 bucket 名 |
| `LLMLOG_S3_ACCESS_KEY` | (必填) | S3 access key |
| `LLMLOG_S3_SECRET_KEY` | (必填) | S3 secret key |
| `LLMLOG_API_TOKEN` | (必填) | Admin UI 登录 Token |
| `LLMLOG_API_CORS_ORIGINS` | `*` | 生产应限定为实际面板域名 |
| `LLMLOG_CAPTURE_WORKERS` | `16` | 采集并发 worker 数 |
| `LLMLOG_CH_BATCH_SIZE` | `500` | ClickHouse 批量写入条数 |
| `LLMLOG_CH_BATCH_INTERVAL` | `300ms` | ClickHouse 批量写入最大延迟 |
| `LLMLOG_DISK_SPOOL_ENABLED` | `true` | CH 不可用时落盘兜底 (零丢失保证) |
| `LLMLOG_DISK_SPOOL_DIR` | `/var/lib/llmlog/spool` | 兜底队列本地路径 (容器内, 已挂 volume) |
| `LLMLOG_DISK_SPOOL_MAX_BYTES` | `10737418240` | 兜底队列最大磁盘占用 (默认 10 GB) |
| `LLMLOG_DISK_SPOOL_INTERVAL` | `30s` | 重放扫描周期 |

### 3.2 端口配置

生产默认使用 **28xxx / 29xxx 段**,避开常见服务端口:

| 变量 | 默认 | 暴露 | 说明 |
| --- | --- | --- | --- |
| `GATEWAY_PORT` | `28080` | 公网 | LLM 代理入口,客户端 `base_url` 指向此 |
| `ADMIN_UI_PORT` | `28888` | 公网 | 管理面板 |
| `CH_HTTP_PORT` | `28123` | `127.0.0.1` | ClickHouse HTTP |
| `CH_TCP_PORT` | `29000` | `127.0.0.1` | ClickHouse Native TCP |
| `MINIO_API_PORT` | `29100` | `127.0.0.1` | MinIO S3 API |
| `MINIO_CONSOLE_PORT` | `29001` | `127.0.0.1` | MinIO Web Console |
| `QUERY_API_PORT` | `28000` | 不暴露 | 由 admin-ui 内 nginx 反代 |
| `GATEWAY_METRICS_PORT` | `29090` | `127.0.0.1` | Prometheus 抓取指标 |

修改端口只需改 `.env`,compose 会自动读取。`prod.yml` 将非对外端口绑定到 `127.0.0.1`,只有 `GATEWAY_PORT` 和 `ADMIN_UI_PORT` 开放到 `0.0.0.0`。

### 3.3 newapi 上游连接三种模式

**模式 A — 走宿主机(默认推荐)**:

```
LLMLOG_UPSTREAM_URL=http://host.docker.internal:3000
```

- 无需改动 newapi 容器
- 依赖 compose.yml 已配置的 `extra_hosts: host.docker.internal:host-gateway`,Linux 下也可用
- 流量多一跳,单请求开销 < 1 ms,可忽略

**模式 B — 加入 newapi 的 docker 网络**:

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

```
LLMLOG_UPSTREAM_URL=http://new-api:3000
```

此时 newapi 可以关闭宿主 3000 端口映射,只对内暴露,安全性更高。

**模式 C — gateway 共享宿主机网络**:

```yaml
gateway:
  network_mode: host
```

注释掉 ports 与 networks 段。适合 gateway 需要极低延迟或需要绑定特定接口的场景。

---

## 4. 自动初始化机制

Compose 启动时自动完成 schema 与 bucket 初始化,无需手动跑 migration。

### 4.1 ClickHouse Schema

`docker-compose.yml` 把 `./migrations/clickhouse/` 挂载为 ClickHouse 容器的 `/docker-entrypoint-initdb.d`。ClickHouse 官方镜像在**数据目录为空**时会按字典序执行该目录下的所有 `.sql`。

当前 migration 文件:

```
migrations/clickhouse/
├── 001_init.sql                   # 建库 + schema_versions 记录表
├── 002_main_table.sql             # 主表 llm_calls (冷热分层, TTL)
├── 003_materialized_views.sql     # 按小时 × 模型 × 用户预聚合视图
├── 004_helper_views.sql           # 运维辅助视图
└── 005_client_type.sql            # 客户端类型字段 + 物化视图扩展 (幂等)
```

**只在首次启动生效**。数据卷 `clickhouse_data` 已存在时不重复执行,避免误删。

**对既有部署升级**: `005_client_type.sql` 设计为幂等,可直接手动应用而不清卷:

```bash
docker compose exec -T clickhouse clickhouse-client --multiquery \
  < migrations/clickhouse/005_client_type.sql
```

应用后, gateway 会自动开始填写 `client_type`/`client_app_version`/`user_agent` 字段,历史数据保持空值。

### 4.2 MinIO Bucket

`docker-compose.yml` 定义了独立的 `minio-init` 服务,使用官方 `minio/mc` 镜像:

- 等 `minio` 健康
- `mc alias set` 连接
- `mc mb --ignore-existing` 创建 `llm-logs` 和 `clickhouse-cold` 两个 bucket
- 打印日志后自动退出

`gateway` 和 `query-api` 的 `depends_on` 声明了 `minio-init: service_completed_successfully`,保证 bucket 就绪后才启动业务服务,避免首个请求写失败。

### 4.3 查看初始化日志

```bash
# Schema 应用记录
docker compose logs clickhouse | grep -i "docker-entrypoint-initdb\|Running"

# Bucket 创建记录
docker compose logs minio-init

# 预期输出示例
# >> [minio-init] connecting to http://minio:9000
# >> [minio-init] creating bucket: llm-logs
# >> [minio-init] creating bucket: clickhouse-cold
# >> [minio-init] buckets ready, exiting.
```

---

## 5. 运维操作

### 5.1 启动 / 停止 / 重启

```bash
# 加载环境变量 (生产强烈推荐)
set -a; source .env; set +a

# 启动 / 重建
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build

# 停止
docker compose -f docker-compose.yml -f docker-compose.prod.yml stop

# 停止 + 删容器 (保留卷)
docker compose -f docker-compose.yml -f docker-compose.prod.yml down

# 停止 + 删容器 + 删数据卷 (危险)
docker compose -f docker-compose.yml -f docker-compose.prod.yml down -v

# 重启单个服务
docker compose restart gateway
```

### 5.2 日志查看

```bash
# 聚合日志
docker compose logs -f

# 单服务
docker compose logs -f gateway
docker compose logs --tail=200 clickhouse
docker compose logs minio-init

# 按时间过滤
docker compose logs --since 10m gateway
```

### 5.3 备份 / 恢复

**ClickHouse**:

```bash
# 备份
docker compose exec -T clickhouse clickhouse-client --query \
  "BACKUP DATABASE llm_logger TO Disk('backups', 'llm_logger.zip')"

# 或直接快照数据卷
docker run --rm -v new-api-logger_clickhouse_data:/src -v "$(pwd)/backups:/dst" \
  alpine tar czf /dst/ch-$(date +%F).tar.gz -C /src .
```

**MinIO**:

```bash
# 从容器内 mc 同步到宿主机
docker compose exec -T minio sh -c "
  mc alias set local http://localhost:9000 \$MINIO_ROOT_USER \$MINIO_ROOT_PASSWORD &&
  mc mirror --overwrite local/llm-logs /data/backup
"
```

---

## 6. 生产最佳实践

### 6.1 HTTPS / 反向代理

Admin UI 与 Gateway 端口建议挂在 Nginx / Caddy 后,配置 TLS。示例 Nginx:

```nginx
server {
    listen 443 ssl http2;
    server_name admin.yourdomain.com;

    ssl_certificate     /etc/ssl/admin.crt;
    ssl_certificate_key /etc/ssl/admin.key;

    location / {
        proxy_pass http://127.0.0.1:28888;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}

server {
    listen 443 ssl http2;
    server_name llm.yourdomain.com;

    ssl_certificate     /etc/ssl/llm.crt;
    ssl_certificate_key /etc/ssl/llm.key;

    # Gateway 需要支持 SSE 长连接
    location / {
        proxy_pass http://127.0.0.1:28080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_buffering off;            # 关闭 buffer, SSE 首字节低延迟
        proxy_read_timeout 600s;        # 允许长流
        proxy_send_timeout 600s;
        chunked_transfer_encoding on;
    }
}
```

挂上 Nginx 后,记得把 `LLMLOG_API_CORS_ORIGINS` 改为 `https://admin.yourdomain.com`。

### 6.2 资源限制调优

`docker-compose.prod.yml` 默认值适配 **2C / 4G** 的 VPS。大机器请在 `.env` 覆盖:

| 机器规格 | `CH_CPU_LIMIT` | `CH_MEM_LIMIT` | `GATEWAY_CPU_LIMIT` | `GATEWAY_MEM_LIMIT` |
| --- | --- | --- | --- | --- |
| 2C / 4G | 1.5 | 2g | 0.8 | 512m |
| 4C / 8G | 2.5 | 4g | 1.5 | 1g |
| 8C / 16G | 4 | 8g | 2 | 2g |
| 16C / 32G+ | 8 | 16g | 4 | 4g |

**注意**: `cpus` 值必须**严格小于**主机物理核数,不能等于。否则 Docker 会报 `Range of CPUs is from 0.01 to N.00`。

### 6.3 防火墙

只放行对外端口:

```bash
# ufw
ufw allow 28080/tcp
ufw allow 28888/tcp

# firewalld
firewall-cmd --permanent --add-port=28080/tcp
firewall-cmd --permanent --add-port=28888/tcp
firewall-cmd --reload
```

挂了 Nginx + TLS 后,可以只放 443,关闭 28xxx 的公网访问。

### 6.4 对象存储切换到腾讯 COS / AWS S3

生产强烈推荐用云厂商的对象存储替代本地 MinIO。修改 `.env`:

```bash
# 腾讯云 COS 示例
LLMLOG_S3_ENDPOINT=https://cos.ap-guangzhou.myqcloud.com
LLMLOG_S3_REGION=ap-guangzhou
LLMLOG_S3_BUCKET=llm-logs-1234567890
LLMLOG_S3_ACCESS_KEY=AKIDxxxxxxxxxxxx
LLMLOG_S3_SECRET_KEY=xxxxxxxxxxxxxx
LLMLOG_S3_USE_PATH_STYLE=false
```

然后注释 `docker-compose.yml` 里的 `minio` 和 `minio-init` 服务。记得 `gateway` / `query-api` 的 `depends_on` 里也移除对 minio 的依赖。

---

## 7. 常见问题

### 7.1 `docker compose up` 报端口占用但 `ss -tlnp` 查不到

**根因**: 主 `docker-compose.yml` 的 `ports` 与 `docker-compose.prod.yml` 的 `ports` 被 compose v2 **追加合并**,导致同端口被 bind 两次。

**解法**: `docker-compose.prod.yml` 的 `ports` 已加 `!override` 标签强制替换,升级到 compose v2.20+ 即可生效。

### 7.2 `.env` 变量没被 compose 读取

**根因**: compose v2.24+ 在某些边界情况下不会把 `.env` 用作 YAML 插值。

**解法**: 启动前 `source .env` 到 shell,优先级最高:

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

**根因**: `docker-entrypoint-initdb.d` 只在 ClickHouse **数据目录为空**时执行。如果之前启动过并留下卷,不会重复跑。

**解法** (仅开发 / 初次部署出问题时):

```bash
docker compose down -v        # 清数据卷
docker compose up -d          # 重新起, 会重新应用 schema
```

生产环境如需增量 migration,手动执行:

```bash
docker compose exec -T clickhouse clickhouse-client --multiquery \
  < migrations/clickhouse/005_your_new_migration.sql
```

### 7.4 Gateway 连不到 newapi

**检查点**:

```bash
# 1. 从 gateway 容器内测试
docker compose exec gateway wget -O- -T 3 "$LLMLOG_UPSTREAM_URL/health" 2>&1 | head

# 2. 看实际用的 URL
docker compose logs gateway | grep -i upstream

# 3. 模式 A 时确认 host.docker.internal 解析正常
docker compose exec gateway getent hosts host.docker.internal
```

对应方案见 §3.3。

### 7.5 Admin UI 登录后 API 报 401 / CORS

- **401**: `.env` 里的 `LLMLOG_API_TOKEN` 与前端输入的不一致
- **CORS**: `LLMLOG_API_CORS_ORIGINS` 没包含实际访问的域名
- **改后需重启 query-api**:`docker compose restart query-api`

### 7.6 磁盘兜底队列积压 (spool)

当 CH 长时间不可用时,gateway 会把 batch 写到 `/var/lib/llmlog/spool` 下的 NDJSON 文件,待 CH 恢复后自动 replayer 回放。

```bash
# 查看队列堆积量 (容器内)
docker compose exec gateway sh -c "du -sh /var/lib/llmlog/spool && ls /var/lib/llmlog/spool | wc -l"

# 看 replayer 日志 (替换 docker compose logs gateway)
docker compose logs gateway | grep -Ei "spool|replayer"

# 强制触发 replay (实际上 30s 自动扫描, 无需手动)
docker compose restart gateway
```

- 文件堆积到 `LLMLOG_DISK_SPOOL_MAX_BYTES` 时,新数据会被**丢弃**并告警,建议监控容器 `/var/lib/llmlog/spool` 占用
- 文件命名形如 `batch-YYYYMMDD-HHMMSS-<n>.ndjson`,可 `jq` 读取
- 回放成功后文件自动删除

### 7.7 用户画像页显示空 / "未识别"

- **空页面**: `GET /api/v1/users` 依赖 `user_id` 非空,老数据 (未跑 005 migration) 的 `user_id` 取自 `X-Forwarded-User` header,若客户端没传则仍是空
  - 验证: `SELECT count() FROM llm_call_logs WHERE user_id != ''`
  - 解法: 让客户端发请求时携带 `X-Forwarded-User: <id>`,或在 newapi 层面给每个 API key 关联一个 user_id
- **全是 "未识别"**: 客户端 UA 不在 `client_classify.go` 的识别清单中
  - 打开 trace 详情查看 `client_user_agent` 原值
  - 需要新增规则: 编辑 `gateway/internal/handlers/client_classify.go` 加分支,重启 gateway

---

## 8. Kubernetes 部署

仓库中提供的是 Compose 部署。如需 K8s,建议:

- `gateway` → Deployment + Service + HPA(按 CPU 自动扩容)
- `query-api` → Deployment + Service
- `admin-ui` → Deployment + Service + Ingress
- `clickhouse` → 用官方 `clickhouse-operator`(生产推荐)
- `minio` → 改用托管对象存储

核心配置保持 `.env` 里的变量不变,放进 ConfigMap + Secret 即可。Helm Chart 在路线图中。

---

## 9. 卸载

```bash
cd /opt/llm-logger
docker compose -f docker-compose.yml -f docker-compose.prod.yml down -v
docker image rm prismai/llm-logger-gateway:latest prismai/llm-logger-query-api:latest prismai/llm-logger-admin-ui:latest
cd .. && rm -rf /opt/llm-logger
```

数据卷 `down -v` 已删除。如果有备份需求,先 §5.3 备份。
