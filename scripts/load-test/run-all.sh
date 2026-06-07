#!/bin/bash
# run-all.sh — 一键压测 + 观测脚本
# 用法: ./scripts/load-test/run-all.sh [BUYER_COUNT]

set -e

BUYER_COUNT=${1:-100}
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RESULT_DIR="${SCRIPT_DIR}/results"

mkdir -p "${RESULT_DIR}"

echo "=========================================="
echo "  竞拍系统压测 (Buyers: ${BUYER_COUNT})"
echo "=========================================="

# 0. 压测前观测
echo ""
echo "[0/5] 压测前状态快照..."
node "${SCRIPT_DIR}/05-observe.mjs" "${RESULT_DIR}/setup.json" > "${RESULT_DIR}/before.txt" 2>/dev/null || true
cat "${RESULT_DIR}/before.txt"

# 1. 准备测试数据
echo ""
echo "[1/5] 准备测试数据..."
node "${SCRIPT_DIR}/01-setup.mjs" "${BUYER_COUNT}" > "${RESULT_DIR}/setup.json"
echo "  数据已保存"

# 2. HTTP 出价压测 - 50 VUs
echo ""
echo "[2/5] HTTP 出价压测 (50 VUs, 30s)..."
k6 run -q \
  -e SETUP_FILE="${RESULT_DIR}/setup.json" \
  -e VUS=50 -e DURATION=30s \
  --summary-export="${RESULT_DIR}/http-50.json" \
  "${SCRIPT_DIR}/02-http-bid.js" 2>&1 | tail -8

# 3. HTTP 出价压测 - 100 VUs
echo ""
echo "[3/5] HTTP 出价压测 (100 VUs, 30s)..."
k6 run -q \
  -e SETUP_FILE="${RESULT_DIR}/setup.json" \
  -e VUS=100 -e DURATION=30s \
  --summary-export="${RESULT_DIR}/http-100.json" \
  "${SCRIPT_DIR}/02-http-bid.js" 2>&1 | tail -8

# 4. 业务一致性验证
echo ""
echo "[4/5] 业务一致性验证..."
node "${SCRIPT_DIR}/04-consistency.mjs" "${RESULT_DIR}/setup.json"

# 5. 压测后观测 + 完整报告
echo ""
echo "[5/5] 压测后观测报告..."
node "${SCRIPT_DIR}/05-observe.mjs" "${RESULT_DIR}/setup.json" "${RESULT_DIR}/http-100.json"

echo ""
echo "=========================================="
echo "  全部完成"
echo "  详细结果: ${RESULT_DIR}/"
echo "=========================================="
