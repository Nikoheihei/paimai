#!/bin/bash
# 产线协调脚本 — 编排多 AI 产线执行顺序
# 用法: ./pipeline-bus.sh [pipeline-name]

set -euo pipefail

PIPELINE_DIR="$(cd "$(dirname "$0")" && pwd)"
REPORT_DIR="${PIPELINE_DIR}/reports"
mkdir -p "${REPORT_DIR}"

run_pipeline() {
    local name="$1"
    local config="${PIPELINE_DIR}/${name}-pipeline.yml"
    
    if [ ! -f "${config}" ]; then
        echo "❌ 未找到产线配置: ${config}"
        exit 1
    fi
    
    echo "=== 启动产线: ${name} ==="
    echo "配置: ${config}"
    
    # 读取产线配置中的 validation gates 并执行
    # （当前为占位，后续可扩展为自动解析 yml 并调用对应验证）
    echo "产线运行中..."
    echo "完成时间: $(date -u +'%Y-%m-%dT%H:%M:%SZ')"
    
    # 输出报告
    cat > "${REPORT_DIR}/${name}-$(date +%Y%m%d%H%M%S).json" << EOF
{
    "pipeline": "${name}",
    "status": "completed",
    "timestamp": "$(date -u +'%Y-%m-%dT%H:%M:%SZ')",
    "gates": []
}
EOF
    echo "✅ 产线 ${name} 完成 | 报告: ${REPORT_DIR}/"
}

# 如果没有指定产线，按依赖顺序跑
if [ $# -eq 0 ]; then
    echo "可用的产线:"
    for f in "${PIPELINE_DIR}"/*-pipeline.yml; do
        basename "$f" | sed 's/-pipeline.yml//'
    done
    echo ""
    echo "用法: $0 <pipeline-name>"
    exit 0
fi

run_pipeline "$1"
