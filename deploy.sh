#!/usr/bin/env bash
#
# Purify 一键部署脚本
# 用法: ./deploy.sh <VPS_IP> [SSH_USER]
#
# 示例:
#   ./deploy.sh 142.171.248.238
#   ./deploy.sh 142.171.248.238 ubuntu
#
set -euo pipefail

VPS_IP="${1:?用法: ./deploy.sh <VPS_IP> [SSH_USER]}"
SSH_USER="${2:-root}"
REMOTE_DIR="/opt/purify"
SSH_OPTS="-o StrictHostKeyChecking=accept-new -o ConnectTimeout=10"

echo "══════════════════════════════════════════"
echo "  Purify 部署 → ${SSH_USER}@${VPS_IP}"
echo "══════════════════════════════════════════"

# ── 1. 检查 SSH 连通性 ──
echo "[1/5] 检查 SSH 连接..."
ssh ${SSH_OPTS} "${SSH_USER}@${VPS_IP}" "echo '连接成功'" || {
    echo "❌ SSH 连接失败，请检查密钥或密码配置"
    exit 1
}

# ── 2. 确保远端有 Docker ──
echo "[2/5] 检查 Docker 环境..."
ssh ${SSH_OPTS} "${SSH_USER}@${VPS_IP}" bash <<'REMOTE_CHECK'
if ! command -v docker &>/dev/null; then
    echo "安装 Docker..."
    curl -fsSL https://get.docker.com | sh
    systemctl enable --now docker
fi
docker --version
docker compose version 2>/dev/null || docker-compose --version
REMOTE_CHECK

# ── 3. 同步代码到 VPS ──
echo "[3/5] 同步项目文件..."
ssh ${SSH_OPTS} "${SSH_USER}@${VPS_IP}" "mkdir -p ${REMOTE_DIR}"
rsync -avz --delete \
    --exclude='.git' \
    --exclude='bin/' \
    -e "ssh ${SSH_OPTS}" \
    "$(dirname "$0")/" \
    "${SSH_USER}@${VPS_IP}:${REMOTE_DIR}/"

# ── 4. 生成 API Key（如果不存在） ──
echo "[4/5] 配置环境变量..."
ssh ${SSH_OPTS} "${SSH_USER}@${VPS_IP}" bash <<REMOTE_ENV
cd ${REMOTE_DIR}
if [ ! -f .env ]; then
    API_KEY=\$(openssl rand -hex 32)
    echo "PURIFY_API_KEYS=\${API_KEY}" > .env
    echo "✨ 已生成 API Key: \${API_KEY}"
    echo "   请妥善保存！"
else
    echo "ℹ️  .env 已存在，跳过生成"
    cat .env
fi
REMOTE_ENV

# ── 5. 构建并启动 ──
echo "[5/5] 构建并启动服务..."
ssh ${SSH_OPTS} "${SSH_USER}@${VPS_IP}" bash <<REMOTE_UP
cd ${REMOTE_DIR}
docker compose down 2>/dev/null || true
docker compose up -d --build
echo ""
echo "等待服务启动..."
sleep 5
# Health check
if curl -sf http://127.0.0.1:8080/api/v1/health | python3 -m json.tool 2>/dev/null; then
    echo ""
    echo "══════════════════════════════════════════"
    echo "  ✅ Purify 部署成功！"
    echo "  地址: http://127.0.0.1:8080"
    echo "  日志: docker compose -f ${REMOTE_DIR}/docker-compose.yml logs -f"
    echo "══════════════════════════════════════════"
else
    echo "⚠️  服务可能还在启动中，请手动检查:"
    echo "  docker compose -f ${REMOTE_DIR}/docker-compose.yml logs"
fi
REMOTE_UP
