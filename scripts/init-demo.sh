#!/bin/bash
set -e

BASE="http://localhost:8080"

echo "=========================================="
echo "  初始化多商家演示数据"
echo "=========================================="
echo ""

# ====== 商家1：翡翠世家 ======
echo ">>> 商家1：翡翠世家"

REG=$(curl -s -X POST "$BASE/api/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"username":"jade","password":"jade123456","nickname":"翡翠世家","role":"seller"}')
TOKEN1=$(echo "$REG" | python3 -c "import sys,json; print(json.load(sys.stdin).get('data',{}).get('token','') or '')" 2>/dev/null)
if [ -z "$TOKEN1" ]; then
  TOKEN1=$(curl -s -X POST "$BASE/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d '{"username":"jade","password":"jade123456"}' | \
    python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])")
fi
echo "  已登录"

# 创建直播间
ROOM_RESP=$(curl -s -X POST "$BASE/api/admin/rooms" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN1" \
  -d '{"title":"翡翠世家 · 冰种专场"}')
RID1=$(echo "$ROOM_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
echo "  直播间: #$RID1"

# 创建商品
echo "  DEBUG: TOKEN1 length=${#TOKEN1}"
PROD_RESP=$(curl -s -w "
HTTP_CODE:%{http_code}" -X POST "$BASE/api/admin/products" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN1" \
  -d '{"name":"冰种翡翠手镯","description":"缅甸天然冰种翡翠"}')
HTTP_CODE=$(echo "$PROD_RESP" | grep "HTTP_CODE:" | sed 's/HTTP_CODE://')
PROD_BODY=$(echo "$PROD_RESP" | grep -v "HTTP_CODE:")
echo "  创建商品响应: HTTP $HTTP_CODE → $PROD_BODY"
PID1=$(echo "$PROD_BODY" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
echo "  商品: #$PID1 冰种翡翠手镯"

# 创建第二个商品
curl -s -X POST "$BASE/api/admin/products" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN1" \
  -d '{"name":"糯种飘花挂件"}' > /dev/null

# 创建竞拍
AUC_RESP=$(curl -s -X POST "$BASE/api/admin/auctions" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN1" \
  -d "{\"roomId\":$RID1,\"productId\":$PID1,\"mode\":\"sudden_death\",\"startPriceCents\":0,\"bidIncrementCents\":100,\"capPriceCents\":10000}")
AID1=$(echo "$AUC_RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
echo "  竞拍: #$AID1"

# 发布 + 开始
curl -s -X POST "$BASE/api/admin/auctions/$AID1/publish" \
  -H "Authorization: Bearer $TOKEN1" > /dev/null
curl -s -X POST "$BASE/api/admin/auctions/$AID1/start" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN1" \
  -d '{"durationSec":1200}' > /dev/null

# 开播
curl -s -X POST "$BASE/api/admin/rooms/$RID1/live" \
  -H "Authorization: Bearer $TOKEN1" > /dev/null
echo "  已开播"


# ====== 商家2：潮玩社 ======
echo ""
echo ">>> 商家2：潮玩社"

REG2=$(curl -s -X POST "$BASE/api/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"username":"toy","password":"toy123456","nickname":"潮玩社","role":"seller"}')
TOKEN2=$(echo "$REG2" | python3 -c "import sys,json; print(json.load(sys.stdin).get('data',{}).get('token','') or '')" 2>/dev/null)
if [ -z "$TOKEN2" ]; then
  TOKEN2=$(curl -s -X POST "$BASE/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d '{"username":"toy","password":"toy123456"}' | \
    python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])")
fi
echo "  已登录"

ROOM_RESP2=$(curl -s -X POST "$BASE/api/admin/rooms" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN2" \
  -d '{"title":"潮玩社 · 限量手办"}')
RID2=$(echo "$ROOM_RESP2" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
echo "  直播间: #$RID2"

PROD_RESP2=$(curl -s -w "
HTTP_CODE:%{http_code}" -X POST "$BASE/api/admin/products" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN2" \
  -d '{"name":"限量版孙悟空手办","description":"2026春季限定版"}')
HTTP_CODE2=$(echo "$PROD_RESP2" | grep "HTTP_CODE:" | sed 's/HTTP_CODE://')
PROD_BODY2=$(echo "$PROD_RESP2" | grep -v "HTTP_CODE:")
echo "  创建商品响应: HTTP $HTTP_CODE2 → $PROD_BODY2"
PID2=$(echo "$PROD_BODY2" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
echo "  商品: #$PID2 孙悟空手办"

curl -s -X POST "$BASE/api/admin/products" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN2" \
  -d '{"name":"宝可梦皮卡丘公仔"}' > /dev/null

AUC_RESP2=$(curl -s -X POST "$BASE/api/admin/auctions" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN2" \
  -d "{\"roomId\":$RID2,\"productId\":$PID2,\"mode\":\"sudden_death\",\"startPriceCents\":0,\"bidIncrementCents\":50,\"capPriceCents\":5000}")
AID2=$(echo "$AUC_RESP2" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
echo "  竞拍: #$AID2"

curl -s -X POST "$BASE/api/admin/auctions/$AID2/publish" \
  -H "Authorization: Bearer $TOKEN2" > /dev/null
curl -s -X POST "$BASE/api/admin/auctions/$AID2/start" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN2" \
  -d '{"durationSec":900}' > /dev/null
curl -s -X POST "$BASE/api/admin/rooms/$RID2/live" \
  -H "Authorization: Bearer $TOKEN2" > /dev/null
echo "  已开播"


# ====== demo 演示商家（离线） ======
echo ""
echo ">>> 演示商家（离线）"

REG3=$(curl -s -X POST "$BASE/api/auth/register" \
  -H 'Content-Type: application/json' \
  -d '{"username":"demo","password":"demo123456","nickname":"演示商家","role":"seller"}') || true
TOKEN3=$(echo "$REG3" | python3 -c "import sys,json; print(json.load(sys.stdin).get('data',{}).get('token','') or '')" 2>/dev/null)
if [ -z "$TOKEN3" ]; then
  TOKEN3=$(curl -s -X POST "$BASE/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d '{"username":"demo","password":"demo123456"}' | \
    python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])")
fi

ROOM_RESP3=$(curl -s -X POST "$BASE/api/admin/rooms" \
  -H 'Content-Type: application/json' \
  -H "Authorization: Bearer $TOKEN3" \
  -d '{"title":"老李古董 · 杂项专场"}')
RID3=$(echo "$ROOM_RESP3" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
echo "  直播间: #$RID3（未开播）"


echo ""
echo "=========================================="
echo "  ✅ 完成！"
echo "=========================================="
echo ""
echo "  H5 首页:        http://localhost:5173"
echo "  管理后台:       http://localhost:5174"
echo ""
echo "  商家账号:"
echo "    翡翠世家:     jade / jade123456"
echo "    潮玩社:       toy / toy123456"
echo "    演示商家:     demo / demo123456"
echo ""
echo "  首页会显示 2 个直播中的房间"
echo "=========================================="
