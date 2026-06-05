# 前端开发测试策略与执行报告

> 项目：paimai 直播竞拍平台  
> 更新时间：2026-06-05  
> 阶段：第一部分完成 → 第二部分过渡

---

## 一、测试策略总览

### 1.1 分阶段测试矩阵

| 阶段 | 测试重点 | 工具/方法 | 当前状态 |
|------|---------|----------|---------|
| **P1: 基础框架** | 编译通过、路由正常、项目结构合理 | `tsc -b`, `go build`, `npm run build` | ✅ 已完成 |
| **P2: 页面组件** | UI 渲染正确、交互流畅、视觉达标 | Dev Server 走查、浏览器 DevTools | 🔄 进行中 |
| **P3: 接口联调** | API 对接正确、状态管理无误、WebSocket 通 | 单元测试 + 集成测试 | ⏳ 待开始 |
| **P4: 整体功能** | E2E 流程跑通、性能达标、可发布 | Playwright/Cypress E2E | ⏳ 待开始 |

### 1.2 技术栈与测试工具映射

| 层 | 技术栈 | 测试工具 |
|----|--------|---------|
| 语言检查 | TypeScript strict mode | `tsc -b` (strictNullChecks, noUnusedLocals) |
| 构建检查 | Vite + React | `vite build` (生产构建) |
| 样式检查 | CSS Modules + 全局 CSS | 浏览器 DevTools + 目视检查 |
| 组件交互 | React 18 | 浏览器手动走查 (后续加 RTL) |
| API 对接 | Fetch wrapper | Mock Server + 真实后端联调 |
| 实时通信 | WebSocket (自定义 hook) | ws 连接测试 + 消息收发验证 |
| 后端编译 | Go + Gin + GORM | `go build ./...` |

---

## 二、已完成的测试步骤

### 2.1 Code Review 问题修复（7/7 通过）

参考文档：[code-review-2026-06-05.md](code-reviews/code-review-2026-06-05.md)

| # | 问题 | 严重度 | 修复文件 | 验证方式 |
|---|------|--------|---------|---------|
| 1 | `randomString()` 返回固定字符串 | 🔴 Bug | `server/internal/handler/upload.go` | go build + code review |
| 2 | `filtered.includes(a => ...)` 类型错误 | 🔴 TS | `web-h5/.../ProductFloatPanel.tsx` | tsc -b |
| 3 | Address 类型无后端支撑 | 🟡 设计 | `web-h5/src/shared/types.ts` | TODO 标注 |
| 4 | AnchorHeader 用 UserInfo 冒充主播 | 🟡 逻辑 | `AnchorHeader.tsx` + `LiveRoomPage.tsx` | isPlaceholder prop |
| 5 | productNames 只有占位文本 | 🟡 数据 | `LiveRoomPage.tsx` | TODO 标注待联表 |
| 6 | ImageUploader 未对接上传 API | 🟡 功能 | `ImageUploader.tsx` + `client.ts` | 接入 POST /api/upload |
| 7 | 上传接口无鉴权 | 🔴 安全 | `upload.go` | 加 AuthRequired 中间件 |

### 2.2 TypeScript 严格编译（12+ 错误修复）

```bash
# H5 端
cd web-h5 && npx tsc -b          # 严格模式（含 project references）
cd web-h5 && npm run build        # tsc -b && vite build

# Admin 端  
cd web-admin && npx tsc -b
cd web-admin && npm run build

# Go 后端
cd server && go build ./...
```

**修复的问题清单：**

| 文件 | 错误类型 | 修复方式 |
|------|---------|---------|
| `AnchorHeader.tsx` | JSX 中三元表达式解析歧义 | 用 `<span>` 包裹文本节点 |
| `RoomDetailPage.tsx` | 未使用变量 `handleImageChange`, `uploadImage` | 删除死代码 |
| `ProductFloatPanel.tsx` | 未使用 import `StatusBadge`, `PriceDisplay` | 清理 import |
| `AuctionPanel.tsx` | 类型冲突 (`api/client.Auction` vs `shared/types.Auction`) | 统一用 shared types + 包装函数适配 |
| `AuctionPanel.tsx` | 未使用变量 `tickRef`, `ApiAuction` | 删除/重命名 |
| `ImageUploader.tsx` | 未使用 import `getToken` | 清理 import |
| `LiveRoomPage.tsx` | `useMemo` 类型推断失败 | 显式标注返回值 `() => ({})` |

**最终构建结果：**

```
✅ web-h5:   tsc -b → vite build → 32 modules, CSS 28.6KB, JS 220KB
✅ web-admin: tsc -b → vite build → 24 modules, JS 212KB
✅ server:   go build ./... → 零错误
```

### 2.3 视觉重构 — 仿直播平台 UI 升级

将所有前端页面从"管理后台原型风格"升级为**抖音/快手沉浸式直播风格**：

#### 重写的文件清单

| 文件 | 变化量 | 主要改动 |
|------|-------|---------|
| `web-h5/src/App.css` | 19KB → **28.6KB** | 完整设计系统重写 |
| `web-h5/src/components/VideoPlayer.tsx` | 全量重写 | LIVE 角标 + 动态渐变背景 + 观看数 |
| `web-h5/src/components/AuctionPanel.tsx` | 全量重写 | 毛玻璃悬浮卡 + 渐变按钮 + 出价动画 |
| `web-h5/src/components/AnchorHeader.tsx` | 全量重写 | 圆角头像卡片 + 在线状态 + 关注动效 |
| `web-h5/src/components/AuctionResultModal.tsx` | 新增 | 彩纸粒子庆祝动画 + 光效辉光 |
| `web-h5/src/pages/LiveRoomPage.tsx` | 全量重写 | 全屏沉浸布局 + 底部工具栏 + 商品浮层 |

#### 设计系统核心参数

```css
/* 品牌色 */
--brand-primary: #fe2c55;       /* 抖音红 */
--brand-gold: #ff9800;          /* 金色强调 */
--gradient-brand: linear-gradient(135deg, #fe2c55, #ff6a3d);

/* 毛玻璃 */
--glass-bg: rgba(255, 255, 255, 0.08);
--glass-blur: blur(20px);
--glass-border: rgba(255, 255, 255, 0.12);

/* 直播间背景 */
--live-bg: radial-gradient(ellipse at 30% 20%, #1a0a2e 0%, #0f0f1a 50%, #000 100%);

/* 字体 */
--price-font: 'DIN Alternate', 'Helvetica Neue', sans-serif;
```

### 2.4 Dev Server 启动验证

```bash
# H5 移动端 → http://localhost:5175/
cd web-h5 && npm run dev

# Admin 管理后台 → http://localhost:5176/
cd web-admin && npm run dev
```

两个服务均已启动并通过浏览器预览。

---

## 三、数据库模型与索引参考

> 来源：`server/internal/model/models.go`

### 3.1 表结构与索引详情

#### User（用户基本信息）
| 字段 | 类型 | 约束 | 索引 |
|------|------|------|------|
| id | uint64 | PK, AUTO_INCREMENT | PRIMARY |
| nickname | string(255) | NOT NULL | - |
| avatar_url | string(512) | - | - |
| role | enum(buyer/seller/anchor) | NOT NULL, DEFAULT 'buyer' | - |
| created_at | timestamp | - | - |

> **注意**: User 表没有 username 字段，认证信息在 UserAuth 表

#### UserAuth（用户认证，1:1 关联 User）
| 字段 | 类型 | 约束 | 索引 |
|------|------|------|------|
| id | uint64 | PK, AUTO_INCREMENT | PRIMARY |
| user_id | uint64 | **NOT NULL** | **UNIQUE** |
| **username** | string(64) | **NOT NULL** | **🔑 UNIQUE** ← 登录唯一标识 |
| password_hash | string(255) | NOT NULL | JSON 隐藏 (-) |
| created_at / updated_at | timestamp | - | - |

#### LiveRoom（直播间）
| 字段 | 约束 | 索引 |
|------|------|------|
| seller_id | NOT NULL | INDEX |
| status | enum(offline/live/closed), DEFAULT offline | - |

#### Auction（拍卖场次）— 核心业务表
| 字段 | 约束 | 索引 |
|------|------|------|
| room_id | NOT NULL | 复合 idx_room_status (with status) |
| product_id | NOT NULL | INDEX |
| status | enum(draft/scheduled/running/sold/failed/cancelled) | 复合 idx_room_status + idx_status_end_at (with end_at) |
| end_at | timestamp | 复合 idx_status_end_at (with status) |
| winner_user_id | nullable | INDEX |
| version | int32, DEFAULT 1 | 乐观锁 |

#### Bid（出价记录）
| 字段 | 约束 | 索引 |
|------|------|------|
| auction_id | NOT NULL | **复合唯一** idx_auction_idem (with idempotency_key) |
| user_id | NOT NULL | INDEX |
| idempotency_key | string(128), NOT NULL | **复合唯一** idx_auction_idem (with auction_id) |

> **幂等设计**: `(auction_id, idempotency_key)` 联合唯一索引防止重复出价

#### Order（订单）
| 字段 | 约束 | 索引 |
|------|------|------|
| auction_id | NOT NULL | **UNIQUE** (一拍一单) |
| buyer_id / seller_id | NOT NULL | INDEX |

#### OutboxEvent（事件发件箱）
| 字段 | 约束 | 索引 |
|------|------|------|
| event_uuid | string(64), DEFAULT '' | **UNIQUE** (事件去重) |
| event_type / status | NOT NULL | INDEX |

### 3.2 关键索引策略总结

```
唯一约束（数据完整性）:
├── UserAuth.username      → 用户登录名全局唯一
├── UserAuth.user_id       → 1:1 关联 User 表
├── Order.auction_id       → 一个拍卖只能生成一个订单
├── Bid.(auction_id, idempotency_key) → 幂等出价防重复
└── OutboxEvent.event_uuid → 事件消费者去重

性能索引（查询加速）:
├── LiveRoom.seller_id     → 按卖家列出直播间
├── Product.seller_id      → 按卖家列商品
├── Auction.room_id+status → 房间内按状态筛选拍卖
├── Auction.status+end_at  → 定时任务扫描即将结束的拍卖
├── Auction.winner_user_id → 查询用户赢得的拍卖
├── Bid.user_id            → 查询用户出价历史
├── Order.buyer_id         → 买家订单列表
└── Order.seller_id        → 卖家订单列表
```

---

## 四、下一步测试计划（P2/P3）

### 4.1 页面走查清单（P2 — 当前阶段）

#### H5 移动端 (http://localhost:5175)

- [ ] **登录页**
  - [ ] 手机号输入框 + 验证码输入
  - [ ] 登录按钮点击反馈
  - [ ] 错误提示 Toast 显示
  
- [ ] **房间列表页**
  - [ ] 卡片布局（封面图 + 标题 + 状态标签）
  - [ ] "正在直播" 角标显示
  - [ ] 点击进入直播间跳转

- [ ] **直播间页面**（核心页面）
  - [ ] A: VideoPlayer 区域
    - [ ] 全屏铺满无留白
    - [ ] LIVE 角标脉冲发光动画
    - [ ] 观看数动态变化
  - [ ] B: AnchorHeader 区域
    - [ ] 圆角头像 + 渐变边框
    - [ ] 昵称显示（fallback 为 `用户{ID}`）
    - [ ] 在线绿点 + "X人在看"
    - [ ] 关注按钮 hover/active 状态
  - [ ] C: AuctionPanel 区域
    - [ ] 毛玻璃效果可见
    - [ ] 价格大字斜体显示
    - [ ] 出价按钮渐变色 + 点击效果
    - [ ] 快捷出价三连按钮
  - [ ] D: ProductFloatPanel 区域
    - [ ] 右侧滑出抽屉
    - [ ] Tab 切换（全部/在拍/即将开拍/成交）
    - [ ] 商品卡片展示
  - [ ] E: BottomToolbar 区域
    - [ ] 图标按钮（分享/点赞/购物车）
    - [ ] 评论输入框

- [ ] **AuctionResultModal**
  - [ ] 成交时弹窗触发
  - [ ] 彩纸飘落动画
  - [ ] 成交价大字显示
  - [ ] 倒计时返回按钮

#### Admin 管理后台 (http://localhost:5176)

- [ ] **登录页** — 管理员账号密码登录
- [ ] **房间列表** — Table 展示 + 操作按钮
- [ ] **房间详情页**
  - [ ] Tab 切换（商品管理 / 竞拍管理）
  - [ ] 商品表格 CRUD
  - [ ] ImageUploader 图片上传
  - [ ] 创建竞拍表单字段校验
  - [ ] 状态流转按钮（发布/开始/取消/结算）

### 4.2 控制台检查项

```
浏览器 F12 Console 检查：
□ 无 TypeScript 运行时错误 (TypeError, Cannot read property...)
□ 无未捕获的 Promise rejection
□ 网络请求无 404（静态资源/API）
□ WebSocket 连接成功（ws:// 开头消息）
□ 无 CORS 错误
□ CSS 无 deprecation 警告
```

### 4.3 接口联调测试计划（P3）

| 模块 | 接口 | 测试要点 |
|------|------|---------|
| 认证 | POST /api/auth/login | 登录成功返回 token |
| 认证 | GET /api/auth/me | 携带 token 返回用户信息 |
| 房间 | GET /api/rooms/:id | 房间详情包含主播信息 |
| 房间 | GET /api/rooms/:id/auctions | 竞拍列表含商品名称 |
| 拍卖 | GET /api/auctions/:id | 单场拍卖完整数据 |
| 拍卖 | POST /api/auctions/:id/bids | 出价幂等性 + 价格校验 |
| 上传 | POST /api/upload (Bearer token) | 图片上传 + URL 返回 |
| WS | WebSocket /ws?token=xxx | 连接 + 消息收发 + 心跳 |

### 4.4 性能基线目标（P4 参考）

| 指标 | 目标值 | 测量方法 |
|------|--------|---------|
| First Contentful Paint | < 1.5s | Lighthouse |
| Largest Contentful Paint | < 2.5s | Lighthouse |
| Cumulative Layout Shift | < 0.1 | Lighthouse |
| Bundle Size (H5) | < 250KB gzipped | vite build output |
| WebSocket 消息延迟 | < 200ms | DevTools Network WS tab |

---

## 五、已知技术债与后续跟进

| # | 问题 | 影响 | 计划 |
|---|------|------|------|
| 1 | `productNames` 只有占位 `商品#ID` | 商品浮层无法显示真名 | 后端竞拍列表需联表查 product.name 或额外接口 |
| 2 | `isPlaceholder` 硬编码为 true | 主播信息栏始终显示"加载中" | 后端需返回主播 UserInfo（非当前登录用户） |
| 3 | Address CRUD API 缺失 | 无法填写收货地址 | Phase 2 补充 |
| 4 | 观看数为前端随机数 | 数据不准 | 后端推送真实在线人数 |
| 5 | 无单元测试框架 | 无法自动化回归 | 引入 Vitest + Testing Library |
| 6 | 无 E2E 测试 | 流程回归靠人工 | 引入 Playwright |

---

*本文档随开发进度持续更新。*
