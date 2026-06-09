# Agent 运行时交付说明（Prompt 解析 / 常驻运行器 / 策略决策 / LLM 接入）

本次补全把「安全外壳 + 业务骨架」升级为可运行的自动出价 Agent 闭环。
未改动核心出价 / 结算 / 支付 / 状态机，所有写操作仍走现有路径。

## 新增能力

| 能力 | 文件 | 说明 |
|---|---|---|
| Prompt 意图解析 | `server/internal/agent/parser.go` | 中文规则引擎解析「最高 800 元 / 1.2万 / 关键词」，可选 LLM 优先；表单显式值最高优先级。已接入 `CreateBuyerAgent` |
| 实时策略决策 | `server/internal/agent/policy.go` | `DecideBid()`：已领先则不抬价、预算硬约束、封顶价收敛、下一合法加价。纯函数 + 单测 |
| 常驻任务运行器 | `server/internal/agent/runner.go` | 周期扫描 `active buyer agent × running 竞拍`，匹配→决策→经 `SubmitBuyerBid`（全部安全校验）出价 |
| 事件订阅 / Win 观察 | `runner.go` `reconcileWinsPass` | 轮询 accepted 出价，竞拍 sold 且无 Pact 时补建 Pact（覆盖结算定时器收尾场景） |
| 单元测试 | `parser_policy_test.go` | 解析预算/关键词、决策出价/跳过/封顶，全部通过 |

数据流：`prompt → ParseIntent → strategy_json → Runner(DecideBid) → SubmitBuyerBid → PlaceBid(现有) → win → Pact → 人工审批 → 现有 pay`。

## 前端（web-h5）

| 页面/路由 | 文件 | 入口 |
|---|---|---|
| 我的 Agent（创建/激活/暂停） | `pages/AgentListPage.tsx` `/agents` | 顶部导航「我的 Agent」、竞拍详情「🤖 派 Agent」 |
| 决策回放（审计时间线） | `pages/AgentDetailPage.tsx` `/agents/:agentId` | Agent 卡片点击 |
| 赢拍审批 Pact（选地址/批准/拒绝） | `pages/PactListPage.tsx` `/pacts` | 顶部导航「赢拍审批」 |
| API 封装 | `api/client.ts` | buyer-agent CRUD、pacts、audit |

跳转修复：导航栏新增「我的 Agent / 赢拍审批」，路由全部挂在受保护 `AppLayout` 下，批准 Pact 后自动跳订单页用现有支付。

## 管理端（web-admin）

| 页面/路由 | 文件 | 说明 |
|---|---|---|
| 运营 Agent 控制台 | `pages/MerchantAgentPage.tsx` `#/agents` | 自然语言创建 merchant ops agent、列表、生成只读报表、审计回放 |
| API 封装 | `api/client.ts` | `createMerchantAgent` / `listMerchantAgents` / `createMerchantReport` / `getMerchantAgentAudit` |

导航栏新增「运营 Agent」。商家运营 Agent **无任何出价权限**（schema/service/handler 三层禁止），只做只读运营。
后端报表 job 当前返回只读占位结果（`CreateMerchantReportJob`），可后续接入真实聚合查询。

## 如何运行

后端（默认开启 Runner）：
```bash
cd server
JWT_SECRET=<任意密钥> go run .
# 需要 MySQL(3306) + 可选 Redis；缺失时降级不崩溃
```
前端：
```bash
cd web-h5 && npm install && npm run dev   # /api 代理到后端
```

## ⚙️ 配置（环境变量）

Runner：
| 变量 | 默认 | 说明 |
|---|---|---|
| `AGENT_RUNNER_ENABLED` | `true` | 关闭设 `false` |
| `AGENT_RUNNER_INTERVAL_MS` | `2000` | 扫描周期（毫秒） |

**LLM 接入（可选，留空则用内置中文规则引擎，无需任何 key）**：
| 变量 | 默认 | 说明 |
|---|---|---|
| `AGENT_LLM_API_KEY` | 空 | **在这里配置你的 API Key**；设置后意图解析优先走 LLM |
| `AGENT_LLM_BASE_URL` | `https://api.openai.com` | OpenAI 兼容网关（可换国内兼容服务） |
| `AGENT_LLM_MODEL` | `gpt-4o-mini` | 模型名 |

示例：
```bash
export AGENT_LLM_API_KEY=sk-xxxx
export AGENT_LLM_BASE_URL=https://api.openai.com
export AGENT_LLM_MODEL=gpt-4o-mini
JWT_SECRET=dev go run .
```
LLM 调用失败/超时（8s）自动回退规则引擎，不影响出价闭环。

## 验收 Demo（对应设计文档第 13 节）

1. `/agents` 输入「帮我在翡翠专场拍一件冰种挂件，最高 800 元」→ 创建（解析出预算 ¥800 + 关键词）。
2. 点「激活」。
3. Runner 自动匹配 running 竞拍、预算内出价（`/agents/:id` 决策回放可见每步）。
4. 赢拍后 Agent 停止、`/pacts` 生成待审批 Pact。
5. 选地址批准 → 跳订单页用现有支付；不批准则走现有超时关单。

## 边界 / 未做

- Merchant Ops Agent 已有后端路由，前端在 `web-admin`，本次未加管理端 UI。
- LLM 仅用于意图解析；出价决策为确定性规则引擎（可审计、不超预算），不接 LLM。
