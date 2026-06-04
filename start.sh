#!/bin/bash
# start.sh — 一键启动拍卖系统
# 首次运行会自动构建 Docker 镜像，启动所有服务，并初始化演示数据。

set -e

echo "=============================="
echo "  实时竞拍大师 — 一键启动"
echo "=============================="
echo ""

# 检查 Docker
if ! command -v docker &> /dev/null; then
  echo "❌ 请先安装 Docker Desktop"
  echo "   https://www.docker.com/products/docker-desktop/"
  exit 1
fi

echo "=== 构建并启动所有服务 ==="
docker compose up -d --build
echo ""

echo "=== 等待服务就绪（约 10 秒） ==="
for i in $(seq 1 30); do
  if curl -s http://localhost:8080/ping > /dev/null 2>&1; then
    echo "  ✅ 后端服务已就绪"
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "  ⚠️ 后端服务未在预期内就绪，请检查 docker compose logs"
  fi
  sleep 1
done
echo ""

echo "=== 初始化演示数据 ==="
./scripts/init-demo.sh
echo ""

echo "=============================="
echo "  ✅ 启动完成！"
echo "=============================="
echo ""
echo "  H5 直播间:    http://localhost:5173"
echo "  管理后台:     http://localhost:5174"
echo "  API:          http://localhost:8080/ping"
echo ""
echo "  演示账号: demo / demo123456"
echo ""
echo "  停止服务: docker compose down"
echo "  查看日志: docker compose logs -f"
