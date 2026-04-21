#!/usr/bin/env bash
# ============================================================
# LLM Logger - 一键 VPS 部署脚本
#
# 用法 (需 root 或 sudo 权限):
#   # 1. 本地已 clone 仓库:
#   cd llm-logger && sudo bash deploy/scripts/server-install.sh
#
#   # 2. 远程全自动:
#   curl -fsSL https://raw.githubusercontent.com/<repo>/main/deploy/scripts/server-install.sh | sudo bash
#
# 行为:
#   - 检测并安装 Docker + Docker Compose plugin
#   - 生成 .env (含强随机密码)
#   - 启动全栈 (docker compose up -d)
#   - 自动 migrate CH schema + init MinIO bucket
#   - 打印 Admin UI / Gateway 地址
#
# 支持: Ubuntu 20.04+ / Debian 11+ / CentOS 8+ / Rocky 9+
# ============================================================

set -euo pipefail

# -------- 颜色 --------
C_RED='\033[0;31m'
C_GREEN='\033[0;32m'
C_YELLOW='\033[1;33m'
C_BLUE='\033[0;34m'
C_NONE='\033[0m'

log()  { echo -e "${C_BLUE}[llm-logger]${C_NONE} $*"; }
ok()   { echo -e "${C_GREEN}[OK]${C_NONE} $*"; }
warn() { echo -e "${C_YELLOW}[WARN]${C_NONE} $*"; }
die()  { echo -e "${C_RED}[FATAL]${C_NONE} $*" >&2; exit 1; }

# -------- 前置检查 --------
[[ $EUID -eq 0 ]] || die "请使用 sudo 或 root 权限运行此脚本"

REPO_URL="${REPO_URL:-https://github.com/thb10086/new-api-logger.git}"
INSTALL_DIR="${INSTALL_DIR:-/opt/new-api-logger}"
BRANCH="${BRANCH:-main}"

log "安装目录: ${INSTALL_DIR}"
log "Git 仓库: ${REPO_URL} (branch: ${BRANCH})"

# -------- 1. 系统检测 --------
if [[ -f /etc/os-release ]]; then
  . /etc/os-release
  OS_ID="${ID:-unknown}"
  OS_VERSION="${VERSION_ID:-unknown}"
else
  die "无法识别操作系统 (/etc/os-release 不存在)"
fi
log "操作系统: ${OS_ID} ${OS_VERSION}"

# -------- 2. 安装 Docker --------
install_docker() {
  if command -v docker &>/dev/null && docker compose version &>/dev/null; then
    ok "Docker + Compose 已安装: $(docker --version)"
    return 0
  fi
  log "安装 Docker..."
  case "${OS_ID}" in
    ubuntu|debian)
      apt-get update -y
      apt-get install -y ca-certificates curl gnupg
      install -m 0755 -d /etc/apt/keyrings
      curl -fsSL "https://download.docker.com/linux/${OS_ID}/gpg" \
        -o /etc/apt/keyrings/docker.asc
      chmod a+r /etc/apt/keyrings/docker.asc
      echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/${OS_ID} $(. /etc/os-release && echo ${VERSION_CODENAME}) stable" \
        > /etc/apt/sources.list.d/docker.list
      apt-get update -y
      apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
      ;;
    centos|rhel|rocky|almalinux)
      yum install -y yum-utils
      yum-config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
      yum install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
      ;;
    *)
      die "暂不支持的系统 ${OS_ID}, 请手动安装 Docker 后重跑"
      ;;
  esac
  systemctl enable --now docker
  ok "Docker 安装完成"
}
install_docker

# -------- 3. 安装基础工具 --------
for pkg in git curl openssl; do
  if ! command -v $pkg &>/dev/null; then
    log "安装 $pkg..."
    if command -v apt-get &>/dev/null; then
      apt-get install -y $pkg
    elif command -v yum &>/dev/null; then
      yum install -y $pkg
    fi
  fi
done

# -------- 4. Clone / Pull 代码 --------
if [[ -d "${INSTALL_DIR}/.git" ]]; then
  log "仓库已存在, pull 最新代码..."
  cd "${INSTALL_DIR}"
  git fetch origin "${BRANCH}"
  git checkout "${BRANCH}"
  git pull --rebase origin "${BRANCH}"
else
  log "Clone 仓库到 ${INSTALL_DIR}..."
  git clone --branch "${BRANCH}" "${REPO_URL}" "${INSTALL_DIR}"
  cd "${INSTALL_DIR}"
fi
ok "代码就绪: $(git rev-parse --short HEAD)"

# -------- 5. 生成 .env --------
if [[ ! -f .env ]]; then
  log "生成 .env (随机强密码)..."
  cp .env.prod.example .env

  CH_PWD=$(openssl rand -hex 32)
  S3_KEY=$(openssl rand -hex 16)
  S3_SEC=$(openssl rand -hex 32)
  API_TOKEN=$(openssl rand -hex 32)

  sed -i "s|CHANGE_ME_openssl_rand_hex_32|${CH_PWD}|" .env
  sed -i "s|CHANGE_ME_minio_access_key|${S3_KEY}|" .env
  sed -i "s|CHANGE_ME_minio_secret_key_at_least_40_chars|${S3_SEC}|" .env
  # API_TOKEN 占位符是同一 CHANGE_ME_openssl_rand_hex_32, 上面已经被覆盖,
  # 所以这里兜底改一下 LLMLOG_API_TOKEN 行 (留到第二次 sed)
  sed -i "s|^LLMLOG_API_TOKEN=.*|LLMLOG_API_TOKEN=${API_TOKEN}|" .env

  # 询问上游地址
  echo ""
  warn "需要配置 newapi 上游地址 (LLMLOG_UPSTREAM_URL)"
  echo "  示例:"
  echo "    - newapi 改为端口 3001:            http://host.docker.internal:3001"
  echo "    - newapi 与本项目同 network    : http://newapi:3001"
  echo "    - newapi 在宿主机其他端口      : http://localhost:<port>"
  read -rp "请输入 LLMLOG_UPSTREAM_URL [http://host.docker.internal:3001]: " UPSTREAM
  UPSTREAM="${UPSTREAM:-http://host.docker.internal:3001}"
  sed -i "s|^LLMLOG_UPSTREAM_URL=.*|LLMLOG_UPSTREAM_URL=${UPSTREAM}|" .env

  chmod 600 .env
  ok ".env 已生成, API_TOKEN 保存在 .env 中, 妥善备份"
else
  warn ".env 已存在, 跳过生成. 如需重置请手动删除"
fi

# -------- 6. 拉起服务 --------
log "构建并启动全栈 (docker compose up -d)..."
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build

log "等待 ClickHouse healthy (最多 60s)..."
for i in $(seq 1 30); do
  if docker compose ps --format json 2>/dev/null \
     | grep -q '"Service":"clickhouse".*"Health":"healthy"' 2>/dev/null \
     || docker compose ps clickhouse | grep -qi "healthy"; then
    ok "ClickHouse 已健康"
    break
  fi
  sleep 2
done

# -------- 7. Bootstrap --------
# ClickHouse 官方镜像会在**数据目录为空**时自动执行挂载的
# /docker-entrypoint-initdb.d/*.sql (即 migrations/clickhouse/*.sql),
# 这一步显式再跑一遍是为了支持"基于已有卷重新 clone 仓库"场景.
# 所有 DDL 都是 CREATE ... IF NOT EXISTS, 重复执行无副作用.
log "初始化 ClickHouse schema (幂等)..."
for f in migrations/clickhouse/*.sql; do
  log "  执行 $f"
  docker compose exec -T clickhouse clickhouse-client --multiquery < "$f" || \
    warn "  $f 执行时出现警告 (通常是对象已存在, 可忽略)"
done

log "初始化 MinIO buckets..."
# 读取 .env 里的 S3 凭证
S3_KEY_FINAL=$(grep -E '^LLMLOG_S3_ACCESS_KEY=' .env | cut -d= -f2-)
S3_SEC_FINAL=$(grep -E '^LLMLOG_S3_SECRET_KEY=' .env | cut -d= -f2-)
BUCKET_FINAL=$(grep -E '^LLMLOG_S3_BUCKET=' .env | cut -d= -f2- || echo "llm-logs")
BUCKET_FINAL="${BUCKET_FINAL:-llm-logs}"

docker compose exec -T minio sh -c "
  mc alias set local http://localhost:9000 ${S3_KEY_FINAL} ${S3_SEC_FINAL} && \
  mc mb -p local/${BUCKET_FINAL} && \
  mc mb -p local/clickhouse-cold && \
  echo 'MinIO buckets ok'
" || warn "MinIO 初始化失败, 稍后手动执行 make init-minio"

# -------- 8. 完成 --------
SERVER_IP=$(curl -s ifconfig.me || echo "<server-ip>")
ADMIN_PORT=$(grep -E '^ADMIN_UI_PORT=' .env | cut -d= -f2- || echo 8888)
GATEWAY_PORT=$(grep -E '^GATEWAY_PORT=' .env | cut -d= -f2- || echo 8080)
API_TOKEN_FINAL=$(grep -E '^LLMLOG_API_TOKEN=' .env | cut -d= -f2-)

echo ""
echo -e "${C_GREEN}================================================================${C_NONE}"
echo -e "${C_GREEN}  LLM Logger 部署完成${C_NONE}"
echo -e "${C_GREEN}================================================================${C_NONE}"
echo ""
echo "  管理面板 (浏览器访问):"
echo -e "    ${C_BLUE}http://${SERVER_IP}:${ADMIN_PORT}${C_NONE}"
echo ""
echo "  Gateway 代理地址 (客户端 base_url 填这个):"
echo -e "    ${C_BLUE}http://${SERVER_IP}:${GATEWAY_PORT}${C_NONE}"
echo ""
echo "  登录 Token (请妥善保存):"
echo -e "    ${C_YELLOW}${API_TOKEN_FINAL}${C_NONE}"
echo ""
echo "  常用命令:"
echo "    cd ${INSTALL_DIR}"
echo "    make status      # 查看服务状态"
echo "    make logs        # 查看日志"
echo "    make restart     # 重启服务"
echo ""
echo -e "${C_YELLOW}  安全提示:${C_NONE}"
echo "    - 建议通过 Caddy/Nginx 加 TLS 后再对公网暴露 Admin UI"
echo "    - 防火墙只需放行 Gateway(${GATEWAY_PORT}) + Admin UI(${ADMIN_PORT})"
echo "    - ClickHouse/MinIO 已绑 127.0.0.1, 不对外"
echo ""
