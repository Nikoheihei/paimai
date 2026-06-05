# 拍卖系统 API 文档

> 所有 API 返回格式：`{ "code": 0, "message": "success", "data": ... }`
> 错误时：`{ "code": 4xx/5xx, "message": "错误描述" }`
> 鉴权方式：`Authorization: Bearer <token>`
> 金额统一用"分"（cents），前端展示时除以 100

---

## 一、认证 API（无需 Token）

### POST /api/auth/register
注册新用户

**Request Body:**
```json
{
  "username": "string (3-32位, 字母/数字/下划线)",
  "password": "string (8-64位, 需包含字母和数字)",
  "nickname": "string (选填, 昵称)"
}
```

**Response Data:**
```json
{
  "userId": 1,
  "username": "alice",
  "nickname": "Alice",
  "token": "eyJhbGciOiJIUzI1NiIs..."
}
```

### POST /api/auth/login
用户登录

**Request Body:**
```json
{
  "username": "string",
  "password": "string"
}
```

**Response Data:** 同注册

### GET /api/auth/me
获取当前登录用户信息（需 Token）

**Response Data:**
```json
{
  "userId": 1,
  "username": "alice",
  "nickname": "Alice",
  "avatarUrl": "",
  "role": "buyer | seller | anchor"
}
```

---

## 二、买家端 API（需 Token）

### GET /api/rooms
获取所有直播中的直播间列表（平台首页）

**Response Data:**
```json
[
  {
    "id": 1,
    "sellerId": 2,
    "title": "翡翠世家 · 冰种专场",
    "coverUrl": "",
    "status": "live"
  }
]
```

### GET /api/rooms/:roomId
获取单个直播间详情

**Response Data:** 同上单个对象

### GET /api/rooms/:roomId/auctions?status=running
获取直播间内的竞拍列表

| 参数 | 类型 | 说明 |
|---|---|---|
| status | string | 选填，过滤条件：running / draft / sold |

**Response Data:**
```json
[
  {
    "id": 1,
    "roomId": 1,
    "productId": 1,
    "mode": "sudden_death | extension",
    "startPriceCents": 0,
    "currentPriceCents": 500,
    "bidIncrementCents": 100,
    "capPriceCents": 10000,
    "reservePriceCents": null,
    "startAt": "2026-06-02T19:35:42+08:00",
    "endAt": "2026-06-02T19:45:42+08:00",
    "extendThresholdSec": 0,
    "extendDurationSec": 0,
    "status": "draft | scheduled | running | sold | failed | cancelled",
    "winnerUserId": null,
    "version": 1,
    "cancelReason": ""
  }
]
```

### GET /api/auctions/:id
获取单个竞拍详情

### GET /api/auctions/:id/ranking?limit=10
获取竞拍排行榜

| 参数 | 类型 | 说明 |
|---|---|---|
| limit | int | 选填，默认 10 |

**Response Data:**
```json
[
  { "rank": 1, "userId": 2, "amountCents": 500 },
  { "rank": 2, "userId": 3, "amountCents": 300 }
]
```

### POST /api/auctions/:id/bids
用户出价

**Request Body:**
```json
{
  "userId": 2,
  "amountCents": 500,
  "idempotencyKey": "uuid-or-client-generated-unique-key",
  "clientTs": 0
}
```

**Response Data:**
```json
{
  "accepted": true,
  "auctionId": 1,
  "userId": 2,
  "amountCents": 500,
  "currentPriceCents": 500,
  "status": "running | sold",
  "endAt": "2026-06-02T19:45:42+08:00",
  "extended": false,
  "sold": false,
  "reserveMet": true,
  "idempotentReplay": false,
  "tooFrequent": false
}
```

> **拒绝时**返回 HTTP 409 + 错误 message，非标准 data 结构

### GET /api/orders
获取当前用户的订单列表

**Response Data:**
```json
[
  {
    "id": 1,
    "auctionId": 1,
    "productId": 1,
    "buyerId": 2,
    "sellerId": 1,
    "finalPriceCents": 500,
    "status": "pending_payment | paid | closed",
    "createdAt": "2026-06-02T19:45:42+08:00",
    "paidAt": null
  }
]
```

### GET /api/orders/:id
获取单个订单详情

### POST /api/orders/:id/pay
模拟支付订单（pending_payment → paid）

**Response Data:** 支付后的订单对象

### WS /api/rooms/:roomId/ws?userId=1
WebSocket 连接（实时推送）

**连接参数:**

| 参数 | 说明 |
|---|---|
| roomId | 路径参数，直播间 ID |
| userId | 查询参数，用户 ID（开发阶段） |

**推送消息格式:**
```json
{
  "type": "bid.accepted | unknown.type",
  "data": { "...事件载荷..." }
}
```

---

## 三、商家管理端 API（需 Token + 商家角色）

> 所有路由前缀：`/api/admin`

### 直播间管理

#### POST /api/admin/rooms
创建直播间

**Request Body:**
```json
{
  "title": "翡翠专场",
  "coverUrl": ""
}
```

**Response Data:**
```json
{
  "id": 1,
  "sellerId": 1,
  "title": "翡翠专场",
  "coverUrl": "",
  "status": "offline",
  "createdAt": "2026-06-02T19:23:54+08:00"
}
```

#### GET /api/admin/rooms
获取当前商家的所有直播间

#### GET /api/admin/rooms/:id
获取单个直播间详情

#### PATCH /api/admin/rooms/:id
更新直播间信息

**Request Body:** 同创建

#### POST /api/admin/rooms/:id/live
开播（offline → live）

**Response Data:** 开播后的房间对象

#### POST /api/admin/rooms/:id/close
关播（live → closed），同时结算所有进行中竞拍

**Response Data:**
```json
{
  "roomId": 1,
  "status": "closed",
  "settled": 2
}
```

### 商品管理

#### POST /api/admin/products
创建商品

**Request Body:**
```json
{
  "name": "冰种翡翠手镯",
  "imageUrl": "",
  "description": ""
}
```

**Response Data:**
```json
{
  "id": 1,
  "sellerId": 1,
  "name": "冰种翡翠手镯",
  "imageUrl": "",
  "description": "",
  "createdAt": "2026-06-02T19:23:54+08:00"
}
```

#### GET /api/admin/products
获取当前商家的所有商品列表

#### GET /api/admin/products/:id
获取单个商品详情

#### DELETE /api/admin/products/:id
删除商品

### 竞拍管理

#### POST /api/admin/auctions
创建竞拍

**Request Body:**
```json
{
  "roomId": 1,
  "productId": 1,
  "mode": "sudden_death | extension",
  "startPriceCents": 0,
  "bidIncrementCents": 100,
  "capPriceCents": 10000,
  "reservePriceCents": null,
  "extendThresholdSec": 0,
  "extendDurationSec": 0,
  "startAt": "2026-06-02T19:35:00+08:00",
  "endAt": "2026-06-02T19:45:00+08:00"
}
```

**Response Data:** 创建的竞拍对象（同买家端竞拍结构）

#### GET /api/admin/auctions?roomId=X
获取指定直播间的竞拍列表

| 参数 | 说明 |
|---|---|
| roomId | 必填，直播间 ID |

#### POST /api/admin/auctions/:id/publish
发布竞拍（draft → scheduled）

#### POST /api/admin/auctions/:id/start
开始竞拍（scheduled → running）

**Request Body:**
```json
{
  "durationSec": 600
}
```

#### POST /api/admin/auctions/:id/cancel
取消竞拍

**Request Body:**
```json
{
  "reason": "商品已售"
}
```

### 订单管理

#### GET /api/admin/orders
获取当前商家的订单列表

#### GET /api/admin/orders/:id
获取单个订单详情

#### POST /api/admin/orders/:id/pay
模拟支付订单（商家后台代付）

### 结算管理

#### POST /api/admin/auctions/:id/settle
手动触发指定竞拍的结算

**Response Data:**
```json
{
  "auctionId": 1,
  "settled": true,
  "status": "sold | failed",
  "orderId": 1,
  "finalPriceCents": 500
}
```

---

## 四、数据模型速查

### 竞拍状态机
```
draft → scheduled → running → sold (成交)
                          └→ failed (流拍)
                          └→ cancelled (取消)
```

### 订单状态
```
pending_payment → paid (支付成功)
             └→ closed (关闭)
```

### 直播间状态
```
offline → live → closed
```

### 竞价模式
| mode | 说明 |
|---|---|
| sudden_death | 绝杀模式：倒计时结束即结束，不支持延时 |
| extension | 延时模式：倒计时结束前出价可延长 |
