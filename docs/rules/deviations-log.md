# 偏差仲裁记录

> AI 产线运行中出现的偏差案例记录。
> 每条偏差必须转化为一条新规则，防止同类问题重复出现。

## 记录格式

```markdown
### YYYY-MM-DD: [偏差标题]

- **产线**：[产线名称]  
- **AI 输出**：[AI 做了什么 / 生成了什么]  
- **偏差表现**：[哪里错了？测试怎么发现的？]  
- **根因分析**：[为什么 AI 会犯这个错？提示词模糊？规则缺失？]  
- **人的仲裁**：[人介入后做了什么决策]  
- **新增规则**：[由此产出的规则 ID，对应 auction-rules.yaml 中的条目]  
- **复测结果**：[更新规则后 AI 重跑是否通过]  
```

---

### 2026-06-02: 出价频率限制缺失

- **产线**：`bid-closed-loop`
- **AI 输出**：原有 PHP 风格的 Go 出价 Lua 脚本没有检查同一用户的出价间隔
- **偏差表现**：规则库 `auction-rules.yaml` 已定义 `minimum-bid-interval`（两次出价 >= 1 秒），但实际 Lua 脚本和 Go 代码均未实现
- **根因分析**：初始开发时未将规则库作为强制验证输入，规则库落后于代码实现
- **人的仲裁**：在 Lua 脚本中增加 `KEYS[4]`（`lastBidTsKey`），在幂等检查之前加入频率检查；Go 侧同步更新 `bidLuaResult`、`BidResult`、`runBidScript`、`bidRejectMessage`
- **新增规则**：`minimum-bid-interval`（已在规则库中，现代码级落地）
- **复测结果**：✅ 编译通过，全量 `go test ./...` 通过，新增 7 个测试用例全部通过

---

### 2026-06-02: 规则库与 API 响应字段不同步

- **产线**：`bid-closed-loop`
- **AI 输出**：新增 `tooFrequent` 字段到 `bidLuaResult` 后，忘记更新 `BidResult` 结构体和 `toBidResult` 映射
- **偏差表现**：测试 `TestBidLuaResultTooFrequent` 编译失败 — `BidResult` 缺少 `TooFrequent` 字段
- **根因分析**：修改 Lua 返回结构时，Go 侧的 DTO 层需要联动修改，没有自动化检查跨层字段一致性
- **人的仲裁**：补上 `BidResult.TooFrequent` 字段和 `toBidResult` 中的赋值
- **新增规则**：无（Go 编译器的类型检查本身已捕获此问题，属于正常迭代流程）
- **复测结果**：✅ 编译通过，测试通过

### 2026-06-02: NewPublicService 未赋值 stream 字段

- **产线**：`websocket-push`
- **AI 输出**：修改了 `NewPublicService` 签名（加 `publisher *stream.Publisher` 参数），但在返回的 `PublicService` 结构体中**没有把参数赋值给 `stream` 字段**
- **偏差表现**：全链路集成测试 `TestBidToWSFullPipeline` 失败——`PlaceBid` 成功后 WS 未收到广播。排查发现 `s.stream` 为 `nil`，`Publish` 从未执行
- **根因分析**：修改函数签名时只改了参数列表和接口调用点（`main.go`、测试文件），漏掉了函数体内部的字段赋值。Go 编译器不检查"参数有名但没用"的情况
- **人的仲裁**：补上 `stream: publisher` 赋值
- **新增规则**：无（此为常见的 Go struct 字面量遗漏问题，类型系统和编译无法捕获接口实现缺失但结构体字段赋值遗漏。建议后续加集成测试覆盖）
- **复测结果**：✅ 3 个集成测试全部通过
