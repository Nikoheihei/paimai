#!/bin/bash
# 一键重置测试环境：清理并创建新的测试竞拍
# 用法: ./scripts/reset-auction.sh [duration_sec]
# 默认 duration=600 (10分钟)

BASE="http://localhost:8080"
DURATION=${1:-600}

echo "=== 取消旧竞拍（清理 Redis 缓存）==="
for id in $(curl -s "$BASE/api/admin/auctions" | python3 -c "import sys,json; [print(a['id']) for a in json.load(sys.stdin).get('data',[])]" 2>/dev/null); do
  echo "  取消 auction:$id"
  curl -s -X POST "$BASE/api/admin/auctions/$id/cancel"     -H 'Content-Type: application/json'     -d '{"reason":"测试环境重置"}' > /dev/null 2>&1 || true
done

echo ""
echo "=== 创建新竞拍 ==="
RESULT=$(curl -s -X POST "$BASE/api/admin/auctions"   -H 'Content-Type: application/json'   -d '{"roomId":1,"productId":1,"mode":"sudden_death","startPriceCents":0,"bidIncrementCents":100,"capPriceCents":10000,"startAt":"2026-06-02T18:00:00+08:00","endAt":"2026-06-02T20:00:00+08:00"}')

AUC_ID=$(echo "$RESULT" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
echo "  竞拍 ID: $AUC_ID"

echo ""
echo "=== 发布 ==="
curl -s -X POST "$BASE/api/admin/auctions/$AUC_ID/publish" > /dev/null
echo "  已发布"

echo ""
echo "=== 开始（${DURATION}秒）==="
curl -s -X POST "$BASE/api/admin/auctions/$AUC_ID/start"   -H 'Content-Type: application/json'   -d "{\"durationSec\": $DURATION}" > /dev/null
echo "  已开始"

echo ""
echo "=== 测试出价（用户2 出 ¥5.00）==="
TS=$(date +%s)
BID_RESULT=$(curl -s -X POST "$BASE/api/auctions/$AUC_ID/bids"   -H 'Content-Type: application/json'   -d "{\"userId\":2,\"amountCents\":500,\"idempotencyKey\":\"reset-test-$TS\"}")
echo "  $BID_RESULT"

echo ""
echo "=============================="
echo "  浏览器打开: http://localhost:5173/?roomId=1&userId=1"
echo "  再次出价:"
echo "    curl -X POST \"$BASE/api/auctions/$AUC_ID/bids\" \\"
echo "      -H 'Content-Type: application/json' \\"
echo "      -d '{\"userId\":3,\"amountCents\":800,\"idempotencyKey\":\"test-$(date +%s)\"}'"
echo "=============================="
