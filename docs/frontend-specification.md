# 拍卖系统前端 Specification

> 基于 `docs/api-reference.md` 后端 API + 现有前端代码 + 参考截图设计
> 技术栈：React 19 + TypeScript + Vite 8（保持不变）

---

## 一、项目现状总览

### 现有文件结构

**web-admin（17 个文件）**
```
src/
├── api/client.ts          # 19 个 API 函数 ✅ 完整
├── hooks/useWebSocket.ts  # WS Hook ✅ 已实现但未使用
├── pages/
│   ├── LoginPage.tsx      # 登录/注册 ✅
│   ├── RoomListPage.tsx   # 直播间列表 ⚠️ 缺封面/搜索
│   ├── RoomDetailPage.tsx # 商品+竞拍管理 ⚠️ 缺图片上传/起拍价UI
│   └── OrderListPage.tsx  # 订单列表 ⚠️ 缺搜索/详情弹窗/分页
```

**web-h5（19 个文件）**
```
src/
├── api/client.ts          # 10 个 API 函数 ⚠️ 缺订单列表API
├── hooks/useWebSocket.ts  # WS Hook ✅ 已集成
├── components/
│   └── AuctionPanel.tsx   # 竞拍面板 ✅ 核心功能完整
├── pages/
│   ├── LoginPage.tsx      # 登录/注册 ✅
│   ├── RoomListPage.tsx   # 直播首页 ⚠️ 缺封面渲染/分类
│   ├── RoomPage.tsx       # 直播间内页 ❌ 缺视频画面+主播栏+商品浮层
│   └── OrderPage.tsx      # 我的订单 ⚠️ 缺收货地址/结束弹窗
```

### 关键缺失项（按优先级）

| 优先级 | 缺失项 | 影响端 | 说明 |
|--------|--------|--------|------|
| **P0** | 直播视频播放器组件 | H5 | 直播间核心体验，当前完全空白 |
| **P0** | 主播信息头部栏 | H5 | 头像/昵称/关注/观看人数 |
| **P0** | 商品侧边浮层 | H5 | 多竞拍切换 Tab（竞拍中/即将开拍/成交/截拍中） |
| **P1** | 图片上传功能 | Admin+H5 | 商品封面/直播间封面 |
| **P1** | 拍卖结束结果弹窗 | H5 | 成交价 + 倒计时返回 |
| **P1** | 收货地址选择 | H5 | 支付前选地址 |
| **P1** | Admin 订单详情+筛选 | Admin | 搜索/状态筛选/分页 |
| **P2** | 弹幕/评论区 | H5 | 截图中有评论滚动区 |
| **P2** | Admin 竞拍起拍价/保留价 UI | Admin | API 有字段但表单未暴露 |
| **P2** | Admin 商品编辑功能 | Admin | 目前只有删除 |

---

## 二、商家/主播端（PC 管理后台）详细设计

### 2.1 页面路由

| 路由 | 组件 | 说明 |
|------|------|------|
| `#/login` | `LoginPage` | 登录/注册（已有，微调样式） |
| `#/` | `DashboardPage` | **新增** — 数据概览看板 |
| `#/rooms` | `RoomListPage` | 直播间列表（重构：加封面图+操作按钮） |
| `#/rooms/:id` | `RoomDetailPage` | 直播间详情（重构：加商品图片上传+竞拍表格化） |
| `#/products` | `ProductListPage` | **新增** — 独立商品管理页（带图片上传） |
| `#/orders` | `OrderListPage` | 订单列表（增强：搜索+详情弹窗+分页） |

### 2.2 DashboardPage（新增 — 数据概览）

**布局**：4 个统计卡片 + 最近竞拍列表 + 快捷操作

```
┌─────────────────────────────────────────────┐
│  📊 商家数据概览                             │
├──────────┬──────────┬──────────┬────────────┤
│ 直播间数  │ 商品总数  │ 进行中竞拍 │ 今日成交额  │
│   3      │   15     │    2     │  ¥12,500   │
├──────────┴──────────┴──────────┴────────────┤
│  最近竞拍动态                                │
│  ┌────┬─────────┬──────┬──────┬────────┐    │
│  │ ID │ 商品名   │ 状态  │ 当前价 │ 操作   │    │
│  ├────┼─────────┼──────┼──────┼────────┤    │
│  │ 1  │ 翡翠手镯 │ running│ ¥500 │ 查看  │    │
│  │ 2  │ 和田玉   │ sold  │ ¥1200│ -     │    │
│  └────┴─────────┴──────┴──────┴────────┘    │
└─────────────────────────────────────────────┘
```

**调用 API**：
- `GET /api/admin rooms` → 统计直播间数
- `GET /api/admin products` → 统计商品数
- `GET /api/admin auctions?status=running` → 进行中竞拍
- `GET /api/admin orders` → 成交统计（本地计算今日总额）

### 2.3 RoomListPage（重构 — 直播间列表）

**改动点**：

| 功能 | 当前状态 | 目标 |
|------|---------|------|
| 封面图展示 | ❌ 无 | 显示 `coverUrl` 或默认占位图 |
| 编辑入口 | ❌ 无 | 每行卡片加"编辑"/"删除"按钮 |
| 开播/关播操作 | ❌ 在详情页内 | 移到列表页行内操作按钮 |
| 搜索/筛选 | ❌ 无 | 加标题搜索框 + 状态筛选下拉（offline/live/closed） |
| 空状态 | ⚠️ 有文字 | 加引导插画 + "创建第一个直播间"按钮 |

**新 UI 布局**（参考截图 1 的表格风格）：
```
┌──────────────────────────────────────────────────────────┐
│ 我的直播间                              [+ 创建直播间]    │
├──────────────────────────────────────────────────────────┤
│ [搜索框................] [状态 ▼全部] [查询]              │
├──────┬─────────────┬────────┬────────┬────┬─────────────┤
│ 封面 │ 名称         │ 状态   │ 创建时间│ 操作│             │
├──────┼─────────────┼────────┼────────┼────┼─────────────┤
│ 🖼️  │ 翡翠世家专场  │ ●直播中 │ 06-04  │管理│[进入][关播] │
│ 🖼️  │ 古钱币夜场    │ 未开播 │ 06-03  │编辑│[进入][开播] │
│ 🖼️  │ 和田玉精品    │ 已结束 │ 06-02  │查看│[进入]      │
└──────┴─────────────┴────────┴────────┴────┴─────────────┘
```

### 2.4 RoomDetailPage（重构 — 核心）

**页面分区**（Tab 切换）：

#### Tab 1: 商品管理

| 功能 | 当前状态 | 目标 |
|------|---------|------|
| 商品列表 | ⚠️ 纯文字 | 表格化：缩略图 + 名称 + 描述 + 操作 |
| 添加商品 | ⚠️ 只有名称/描述 | **增加图片上传**（拖拽或点击上传） |
| 编辑商品 | ❌ 无 | 行内"编辑"按钮弹出修改弹窗 |
| 删除商品 | ✅ 有 | 保持确认弹窗 |

**新增图片上传组件 `ImageUploader`**:
- 支持：拖拽上传 / 点击选择 / URL 粘贴三模式
- 上传目标：后端需提供 `POST /api/admin/upload`（或先用 base64 存 localStorage 占位）
- 预览：上传后立即显示缩略图
- 调用 API: `POST /api/admin products { name, imageUrl, description }`

#### Tab 2: 竞拍管理

| 功能 | 当前状态 | 目标 |
|------|---------|------|
| 竞拍列表 | ⚠️ 简单列表 | 表格化（参考截图 2）：商品名 + 当前价 + 状态 + 操作按钮 |
| 创建竞拍 | ⚠️ 缺起拍价/保留价 | **补全所有字段**：起拍价 + 保留价 + 加价幅度 + 封顶价 + 时长 + 延时参数 |
| 发布/开始/取消 | ✅ 有 | 行内按钮，状态标签颜色区分 |
| 结算 | ✅ API 有 | 对 sold/failed 状态显示"手动结算"按钮 |

**竞拍创建表单完整字段**（对齐 API `POST /api/admin/auctions`）:

```typescript
interface AuctionForm {
  productId: number;           // 商品选择（下拉）
  mode: 'sudden_death' | 'extension'; // 竞拍模式
  startPriceCents: number;     // 起拍价（元，前端存分）
  bidIncrementCents: number;   // 加价幅度（元）
  capPriceCents: number;       // 封顶价（元）
  reservePriceCents: number;   // 保留价（元，可选）
  durationSec: number;         // 时长（秒）
  extendThresholdSec: number;  // 延时阈值（秒，仅 extension 模式）
  extendDurationSec: number;   // 延时时长（秒，仅 extension 模式）
}
```

**竞拍状态标签颜色映射**（参考截图 2）:
- `draft` → 灰色「草稿」
- `scheduled` → 蓝色「待开始」（价格文案为"起拍价"）
- `running` → 红色「竞拍中」（价格文案为"当前最高价"，有"立即出价"按钮）
- `sold` → 绿色「已成交」（价格文案为"落槌价"）
- `failed` → 灰色「流拍」
- `cancelled` → 灰色「已取消」

**竞拍行操作按钮**（根据状态动态显示）:
- `draft`: [发布] [删除]
- `scheduled`: [开始] [取消]
- `running`: [取消] [结算]（强制结算用）
- `sold/failed/cancelled`: 无操作（只读）

#### Tab 3: WebSocket 监控（新增）
- 展示本房间实时 WS 连接数（需后端支持 `/api/admin/rooms/:id/stats` 或从 Hub 获取）
- 显示最近 20 条 WS 消息日志（调试用）
- 复用已有的 `useWebSocket` hook

### 2.5 ProductListPage（新增 — 独立商品管理）

**功能**：
- 表格展示所有商品（缩略图 + 名称 + 描述 + 关联竞拍数 + 创建时间）
- 新建商品（含图片上传）
- 编辑商品（弹窗表单）
- 删除商品（确认弹窗）
- 批量删除（多选 checkbox）

**调用 API**: `listProducts`, `createProduct`, `DELETE /api/admin/products/:id`
**注意**：后端目前缺少 `PUT /api/admin/products/:id` 编辑接口，需要补充

### 2.6 OrderListPage（增强 — 订单管理）

**新增功能**：

| 功能 | 实现 |
|------|------|
| 订单搜索 | 按买家 ID / 竞拍 ID 搜索 |
| 状态筛选 | 下拉：全部 / 待付款 / 已付款 / 已关闭 |
| 时间范围 | 开始日期 ~ 结束日期选择器 |
| 详情弹窗 | 点击订单行展开：商品信息 + 买家信息 + 金额明细 + 支付时间 |
| 分页 | 底部分页控件（每页 20 条） |
| 导出 CSV | 导出当前筛选结果 |

**订单详情弹窗内容**:
```
┌──────── 订单详情 ─────────┐
│ 订单号: #1001              │
│ ─────────────────────────  │
│ 商品: 冰种翡翠手镯          │
│ 竞拍ID: #5                 │
│ 买家: user_abc (ID: 10)    │
│ ─────────────────────────  │
│ 最终价格: ¥500.00          │
│ 状态: 待付款 (橙色)         │
│ 创建时间: 2026-06-04 17:00 │
│ 支付时间: --               │
│ ─────────────────────────  │
│        [模拟支付] [关闭]    │
└────────────────────────────┘
```

---

## 三、用户端（移动端 H5）详细设计

### 3.1 页面路由

| 路由 | 组件 | 说明 |
|------|------|------|
| `#/login` | `LoginPage` | 登录/注册（已有） |
| `#/` | `HomePage` | **重命名 RoomListPage** — 首页（增强） |
| `#/rooms/:id` | `LiveRoomPage` | **重命名 RoomPage** — 直播间（大改） |
| `#/auctions/:id` | `AuctionDetailPage` | **新增** — 单个竞拍详情（全屏出价） |
| `#/orders` | `OrderPage` | 我的订单（增强：地址选择+结束弹窗） |
| `#/orders/:id` | `OrderDetailPage` | **新增** — 订单详情+支付流程 |
| `#/address` | `AddressListPage` | **新增** — 收货地址管理 |

### 3.2 HomePage（首页 — 增强）

**改动**:
- 渲染直播间 `coverUrl` 封面图（当前是空渐变背景）
- 显示观看人数（需后端 API 支持，暂可用随机数占位）
- 下拉刷新（Pull to Refresh）
- 顶部搜索栏（按直播间标题搜索）

**布局**（参考截图中抖音/快手的首页风格）:
```
┌────────────────────┐
│ 🔍 搜索直播间...    │ ← 顶部固定搜索
├────────────────────┤
│ ┌────────┐ ┌──────┐│
│ │ 封面图  │ │封面图 ││ ← 2列瀑布流网格
│ │● LIVE  │ │● LIVE ││
│ │ 翡翠专场│ │古钱币  ││
│ │ 1.2k在看│ │856在看││
│ └────────┘ └──────┘│
│ ┌────────┐ ┌──────┐│
│ │ ...    │ │ ...  ││
│ └────────┘ └──────┘│
├────────────────────┤
│  🏠首页  📦订单     │ ← 底部 Tab 导航
└────────────────────┘
```

### 3.3 LiveRoomPage（直播间 — 大改，最复杂页面）

这是整个项目最核心的页面。参考截图 3/4，分为以下区域：

#### 整体布局（从上到下）

```
┌━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┓
│ 【区域 A】视频直播画面 (全宽)     │  ← 占屏幕 55% 高度
│  ┌──────────────────────────┐   │
│  │                          │   │
│  │   视频播放器 / 占位画面   │   │
│  │                          │   │
│  └──────────────────────────┘   │
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
│ 【区域 B】主播信息栏              │  ← 固定高度 60px
│  [头像] 昵称  关注btn  观看数   │
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
│ 【区域 C】竞拍主操作区            │  ← 可向上滑动展开
│  商品图 + 名称                   │
│  当前价(大字) | 倒计时 | 出价按钮 │
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
│ 【区域 D】排行榜                  │
│  TOP 1-10 排名列表               │
┣━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┫
│ 【区域 E】底部工具栏              │  ← fixed bottom
│  [💬聊天] [🎁礼物] [📤分享] [🛒]│
┗━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━┛

右侧悬浮浮层（覆盖在视频上）:
┌──────────────┐
│ 商品列表      │  ← 可收起/展开
│ ──────────── │
│ [Tab: 全部]   │
│ 商品1 竞拍中  │
│ 商品2 即将开拍│
│ 商品3 已成交  │
│ ...          │
└──────────────┘
```

#### 区域 A: 视频直播画面 (`VideoPlayer` 组件)

**实现方案**（两种可选）:

**方案一: 占位模拟（推荐先做，快速上线）**
- 使用静态图片或渐变色背景模拟直播画面
- 右上角显示"LIVE"红色角标 + 观看人数
- 左上角显示主播头像小圆圈
- 支持点击暂停/播放（模拟）
- 底部半透明遮罩显示"直播中"文字

**方案二: 真实视频流（后续迭代）**
- 接入 WebRTC 或 HLS (hls.js)
- 后端需提供推流地址（OBS / RTMP）
- 本 spec 先以方案一为准

**组件接口**:
```typescript
interface VideoPlayerProps {
  coverUrl?: string;        // 直播间封面图
  isLive: boolean;          // 是否正在直播
  viewerCount?: number;     // 观看人数
  anchorName?: string;      // 主播昵称
  onToggleFullscreen?: () => void;
}
```

#### 区域 B: 主播信息栏 (`AnchorHeader` 组件)

**参考截图 3/4 顶部**:
```
┌────────────────────────────────────────────┐
│ [🔴头像]  盛京古币        [关注](红色按钮)  │
│          2839 观看                      ⋯  │
│ 📦带货总榜  🕐1天              [更多直播 >]│
└────────────────────────────────────────────┘
```

**字段来源**:
- 头像/昵称: 从 JWT token 解析 seller 信息，或调 `GET /api/auth/me`
- 观看数: WS 连接数 / 或后端 stats API
- 关注按钮: 纯 UI（后续对接关注 API）
- "更多直播": 跳转回首页

#### 区域 C: 竞拍主操作区（增强现有 AuctionPanel）

**基于现有 `AuctionPanel.tsx` 增加**:

| 新增内容 | 说明 |
|---------|------|
| 商品图片+名称展示 | 当前显示"商品 #productId"，改为显示实际商品名称和缩略图 |
| 出价成功动效 | 出价成功时价格数字放大+金色闪烁动画 |
| 被超越提醒强化 | 当前只有文字提示，加震动效果 + 红色高亮横幅 |
| 竞拍延时提示 | 倒计时 < extendThresholdSec 时显示"⏰ 延时中"黄色闪烁 |
| 立即出价快捷键 | 参考截图的大红色"⚡ 立即出价"按钮（一键出到当前价+最小加价幅度） |

**出价按钮文案规则**（参考截图 2 注释）:
- 竞拍中 + 无人出价 → "起拍价"
- 竞拍中 + 已有人出价 → "当前最高价"
- 即将开拍(scheduled) → "起拍价"
- 已成交(sold) → "落槌价"
- 流拍/取消 → 灰色禁用态

#### 区域 D: 排行榜（已有，微调）

**现有功能保留**，增加:
- 自己的排名行特殊高亮（蓝色左边框 + "我"标记）
- TOP 3 用金银铜图标
- 点击排名可查看该用户的出价历史

#### 区域 E: 底部工具栏（新增）

**参考截图底部**:
```
┌──────────────────────────────────────┐
│ 💬说点什么...    [📤] [❤️448] [🛒5] │
└──────────────────────────────────────┘
```

| 元素 | 功能 | 实现阶段 |
|------|------|---------|
| 评论输入框 | 输入发送弹幕 | P2: 先做 UI 不发消息 |
| 分享按钮 | 复制链接 / 分享到微信 | P2: 先 toast 提示"分享功能开发中" |
| 点赞数 | 显示点赞数 + 动画 | P2: 纯计数展示 |
| 购物车图标 | 显示已参与竞拍数量 | P2: 点击跳转到我的订单 |

#### 右侧悬浮商品浮层 (`ProductFloatPanel` 组件)

**参考截图 2 右侧的商品列表**:
```
┌────────────────┐
│  主播推荐       │
│ [全部] [在拍]   │
│ [成交] [预展]   │
│ ────────────── │
│ 🖼️ 商品名       │
│    竞拍中        │
│    起拍价 ¥100  │
│                │
│ 🖼️ 商品名       │
│    即将开拍      │
│    起拍价 ¥200  │
│    [🔔提醒我]   │
│                │
│ 🖼️ 商品名       │
│    已成交        │
│    落槌价 ¥500  │
└────────────────┘
```

**Tab 分类逻辑**:
- **全部**: 该房间所有关联竞拍
- **在拍(`running`)**: 正在进行的竞拍（高亮显示）
- **即将开拍(`scheduled`)**: 已发布未开始的竞拍，带"提醒我"按钮
- **已成交(`sold`)** / **预展(`draft`)**: 历史记录

- 点击某个商品 → 切换区域 C/D 的竞拍上下文（加载该商品的竞拍数据）

#### WS 消息处理增强

现有 `useWebSocket` 已处理 `bid.accepted`，需新增消息类型:

| 消息 type | 处理动作 |
|-----------|---------|
| `bid.accepted` | ✅ 已有：刷新价格+排行榜 |
| `outbid` | **新增**：显示被超越横幅 + 手机震动( navigator.vibrate ) |
| `auction.extended` | **新增**：倒计时延长 + 显示"延时"提示 |
| `auction.ended` | **新增**：弹出拍卖结果弹窗（见下方 3.6 节） |
| `ranking.updated` | **新增**：刷新排行榜 |

### 3.4 AuctionDetailPage（新增 — 竞拍全屏详情）

当用户从商品浮层点击某个竞拍时，进入全屏竞拍详情页。

**用途**: 用户想专注看单个竞拍的出价过程时使用（类似截图 3 的全屏出价体验）

**包含内容**:
- 顶部: 返回按钮 + 商品名称
- 中上部: 大号当前价 + 倒计时（复用 AuctionPanel 的核心逻辑）
- 中部: 出价操作区（快捷出价 + 自定义输入）
- 下部: 出价历史时间线（替代简单排行榜）
- 底部固定: 出价按钮

### 3.5 OrderPage（增强 — 我的订单）

**新增功能**:

| 功能 | 说明 |
|------|------|
| 订单 Tab 切换 | 全部 / 待付款 / 已付款 |
| 订单卡片优化 | 显示商品缩略图 + 名称 + 金额 + 状态标签 |
| 地址选择入口 | 待付款订单显示"添加收货地址" |
| 支付流程优化 | 点击支付 → 选地址 → 确认 → 模拟支付 → 成功页 |

**支付流程**（参考截图 5 的地址选择）:
```
订单详情 → 点击[去支付] → 地址选择弹窗 → 确认支付 → 支付成功
```

### 3.6 AddressListPage & AddressSelector（新增 — 收货地址）

**AddressListPage** (`#/address`):
- 收货地址列表（参考截图 5）
- 新增地址（表单：收货人 + 电话 + 省市区 + 详细地址）
- 设为默认地址
- 编辑/删除地址
- "设为直播竞价收货地址"按钮（参考截图 5 底部大按钮）

**数据存储**: 
- 方案 A: 后端新增地址 CRUD API（推荐）
- 方案 B: 先存 localStorage（快速实现，后续迁移）

**AddressSelector 弹窗组件**（嵌入 OrderPage 使用）:
```
┌── 收货地址 ──── [＋添加收货地址] ──┐
│                                      │
│ ◉ 收货人: 夏宝 19074170082           │
│   收货地址: 广东省广州市番禺区...     │
│                                      │
│ ○ 收货人: 夏宝 19074170082           │
│   收货地址: 广东省深圳市龙岗区...     │
│                                      │
│ ○ 收货人: 夏瑾 19074170082           │
│   收货地址: 广东省珠海市香洲区...     │
│                                      │
│  [🔴 设为直播竞价收货地址]            │
└──────────────────────────────────────┘
```

### 3.7 拍卖结束结果弹窗 (`AuctionResultModal` 组件)

**触发时机**: WS 收到 `auction.ended` 消息 或 竞拍状态变为 sold/failed

**UI 参考** 截图 6:
```
┌──────────────────────────┐
│                          │
│     🎭 (动画吉祥物)       │
│                          │
│    本次拍卖已结束          │
│    还有更多好物等你来拍     │
│                          │
│  ┌────┐                  │
│  │🖼️ │ 古钱币拍卖420号拍品 │
│  └────┘                  │
│  最终成交价 ¥2            │
│                          │
│  [ 🔴 回到直播间(3) ]     │  ← 3 秒倒计时自动跳转
│                          │
└──────────────────────────┘
```

**行为**:
- 显示成交商品、最终价
- 若当前用户是 winner → 显示绿色"🎉 恭喜您得标！"额外文案
- "回到直播间"按钮带倒计时（3-2-1），到 0 自动关闭弹窗
- 背景半透明蒙层，阻止其他交互

---

## 四、共享基础设施

### 4.1 需要新建的通用组件

| 组件 | 文件位置 | 用途 |
|------|---------|------|
| `ImageUploader` | `shared/components/ImageUploader.tsx` | 图片上传（拖拽/点击/URL） |
| `VideoPlayer` | `web-h5/components/VideoPlayer.tsx` | 直播视频/占位画面 |
| `AnchorHeader` | `web-h5/components/AnchorHeader.tsx` | 主播信息栏 |
| `ProductFloatPanel` | `web-h5/components/ProductFloatPanel.tsx` | 右侧商品浮层 |
| `AuctionResultModal` | `web-h5/components/AuctionResultModal.tsx` | 拍卖结束弹窗 |
| `AddressSelector` | `web-h5/components/AddressSelector.tsx` | 收货地址选择弹窗 |
| `StatusBadge` | `shared/components/StatusBadge.tsx` | 状态标签（通用颜色映射） |
| `PriceDisplay` | `shared/components/PriceDisplay.tsx` | 价格展示（分→元转换 + 大字样式） |
| `Countdown` | `shared/components/Countdown.tsx` | 倒计时（支持延时模式） |
| `EmptyState` | `shared/components/EmptyState.tsx` | 空状态占位 |
| `ConfirmModal` | `shared/components/ConfirmModal.tsx` | 通用确认弹窗 |
| `Toast` | `shared/components/Toast.tsx` | 全局 Toast 提示（替代当前行内提示） |

### 4.2 共享类型定义 (`shared/types/api.ts`)

```typescript
// === 认证 ===
interface AuthResponse { userId: number; username: string; nickname: string; token: string; }
interface UserInfo { userId: number; username: string; nickname: string; avatarUrl: string; role: 'buyer' | 'seller' | 'anchor'; }

// === 直播间 ===
interface LiveRoom { id: number; sellerId: number; title: string; coverUrl: string; status: 'offline' | 'live' | 'closed'; createdAt: string; }

// === 商品 ===
interface Product { id: number; sellerId: number; name: string; imageUrl: string; description: string; createdAt: string; }

// === 竞拍 ===
type AuctionMode = 'sudden_death' | 'extension';
type AuctionStatus = 'draft' | 'scheduled' | 'running' | 'sold' | 'failed' | 'cancelled';
interface Auction {
  id: number; roomId: number; productId: number; mode: AuctionMode;
  startPriceCents: number; currentPriceCents: number;
  bidIncrementCents: number; capPriceCents: number;
  reservePriceCents: number | null;
  startAt: string; endAt: string;
  extendThresholdSec: number; extendDurationSec: number;
  status: AuctionStatus; winnerUserId: number | null;
  version: number; cancelReason: string;
}

// === 出价 ===
interface BidResult {
  accepted: boolean; auctionId: number; userId: number;
  amountCents: number; currentPriceCents: number;
  status: AuctionStatus; endAt: string;
  extended: boolean; sold: boolean; reserveMet: boolean;
  idempotentReplay: boolean; tooFrequent: boolean;
}
interface RankingItem { rank: number; userId: number; amountCents: number; }

// === 订单 ===
type OrderStatus = 'pending_payment' | 'paid' | 'closed';
interface Order {
  id: number; auctionId: number; productId: number;
  buyerId: number; sellerId: number;
  finalPriceCents: number; status: OrderStatus;
  createdAt: string; paidAt: string | null;
}

// === WS 消息 ===
interface WsMessage { type: string; data: unknown; }
```

### 4.3 共享 Hooks

| Hook | 用途 |
|------|------|
| `useAuth` | 登录状态检测 + token 管理 + 用户信息缓存 |
| `useWebSocket` | 已有，两个项目可共用（移入 shared） |
| `use Countdown` | 倒计时逻辑（含延时扩展场景） |
| `useDebounce` | 防抖（搜索输入） |

### 4.4 API Client 增强

**web-admin `api/client.ts` 缺失函数**:
- `updateProduct(id, name, imageUrl, description)` — 需后端配合加 `PUT /api/admin/products/:id`
- `uploadImage(file): Promise<string>` — 图片上传（返回 URL）

**web-h5 `api/client.ts` 缺失函数**:
- `listBuyerOrders()` — 当前用内联 fetch，应统一抽取
- `getBuyerOrderDetail(id)` — 同上
- `listRooms()` — 首页获取直播间列表（当前可能用了 getRoom？）
- `uploadImage(file)` — 如 H5 也需要上传

---

## 五、后端 API 补充需求

前端完整实现以下功能需要后端配合新增的 API:

| # | API | 方法 | 用途 | 优先级 |
|---|-----|------|------|--------|
| 1 | `/api/upload` | POST | 图片上传（返回 URL） | P1 |
| 2 | `/api/admin/products/:id` | PUT | 编辑商品 | P1 |
| 3 | `/api/rooms` | GET | 买家端获取直播间列表（H5首页） | P1 |
| 4 | `/api/orders` | GET | 买家端订单列表 | P1 |
| 5 | `/api/addresses` | POST/GET/PUT/DELETE | 收货地址 CRUD | P2 |
| 6 | `/api/admin/rooms/:id/stats` | GET | 房间统计数据（连接数等） | P2 |
| 7 | `/api/auctions/:id/history` | GET | 出价历史时间线 | P2 |

---

## 六、实施计划建议

### Phase 1: 核心体验打通（预计 3-4 天）
1. **H5 直播间改造** — VideoPlayer 占位 + AnchorHeader + AuctionPanel 增强 + 商品浮层
2. **Admin 商品图片上传** — ImageUploader 组件 + 上传 API
3. **拍卖结束弹窗** — AuctionResultModal + WS auction.ended 处理

### Phase 2: 管理后台完善（预计 2-3 天）
4. **Admin RoomDetailPage 重构** — Tab 布局 + 竞拍表格 + 起拍价/保留价 UI
5. **Admin OrderListPage 增强** — 搜索 + 详情弹窗 + 分页
6. **ProductListPage 新增** — 独立商品管理页

### Phase 3: 交易流程完善（预计 2 天）
7. **收货地址管理** — AddressListPage + AddressSelector
8. **H5 订单+支付流程** — 地址选择 → 确认 → 支付 → 成功
9. **H5 首页增强** — 封面图 + 下拉刷新 + 搜索

### Phase 4: 打磨体验（预计 2 天）
10. **弹幕/评论 UI**（纯前端展示）
11. **出价动画/音效**
12. **错误边界 + Loading 骨架屏**
13. **响应式适配测试**

---

## 七、设计规范速查

### 颜色系统

| 用途 | web-admin (PC) | web-h5 (移动端) |
|------|---------------|-----------------|
| 主色调 | `#ff6b35` (橙红) | `#ff6b35` (橙红) |
| 成功/成交 | `#52c41a` (绿) | `#52c41a` (绿) |
| 危险/删除 | `#ff4d4f` (红) | `#ff4d4f` (红) |
| 警告/待付款 | `#faad14` (橙) | `#faad14` (橙) |
| 背景 | `#f5f5f5` (浅灰) | `#0f0f1a` (深黑) |
| 卡片背景 | `#ffffff` (白) | `#1a1a2e` (深蓝灰) |
| 文字主色 | `#333333` | `#ffffff` |
| 文字次色 | `#999999` | `#888888` |

### 竞拍状态 → 颜色/文案映射

| Status | 颜色 | 文案 | 价格描述 |
|--------|------|------|---------|
| `draft` | `#999` 灰 | 草稿 | - |
| `scheduled` | `#1890ff` 蓝 | 待开始 | "起拍价" |
| `running` | `#ff4d4f` 红 | 竞拍中 | "当前价" |
| `sold` | `#52c41a` 绿 | 已成交 | "落槌价" |
| `failed` | `#999` 灰 | 流拍 | - |
| `cancelled` | `#999` 灰 | 已取消 | - |

### 订单状态 → 颜色映射

| Status | 颜色 | 文案 |
|--------|------|------|
| `pending_payment` | `#faad14` 橙 | 待付款 |
| `paid` | `#52c41a` 绿 | 已支付 |
| `closed` | `#999` 灰 | 已关闭 |

### 金额展示规则
- 所有金额后端返回单位为**分 (cents)**
- 前端展示统一除以 100，格式为 `¥X.XX`（保留两位小数）
- 大额金额（>10000 元）可简化为 `¥1.2万`
- 竞拍面板中的当前价使用超大字号（36-48px），突出视觉层级
