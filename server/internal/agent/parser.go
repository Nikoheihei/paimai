package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// 意图解析层：把买家自然语言 prompt 解析成结构化 StrategySkill。
//
// 解析顺序：
//  1. 若配置了 LLM（AGENT_LLM_API_KEY），优先调用 LLM 解析；
//  2. LLM 不可用或失败时，回退到内置中文规则引擎；
//  3. 调用方显式传入的 budget / keywords / trigger / pace / stopRatio 永远优先于解析结果。

// 价格识别：匹配「数字 + 单位」，支持 元/块/¥/￥/rmb/k/千/万。
var budgetPattern = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(万|w|千|k|元|块|rmb|￥|\$)?`)

// 命令词 / 停用词：解析关键词时剥离，避免把「帮我」「最高」当成商品词。
var stopwordRunes = []string{
	"帮我", "请", "想要", "我要", "需要", "麻烦", "帮忙",
	"在", "于", "从", "把", "给", "对",
	"拍下", "拍得", "拍一件", "拍一个", "拍卖", "竞拍", "竞价", "出价", "购买", "买下", "买一件", "买一个", "拍",
	"一件", "一个", "一只", "一条", "一块", "若干", "几件",
	"最高", "最多", "最贵", "不超过", "不要超过", "超过", "预算", "封顶", "上限", "以内", "之内", "左右", "大约", "大概",
	"不要", "不能", "别", "就", "的话", "如果", "并且", "而且", "然后",
	"专场", "场次", "活动",
	"元", "块", "钱", "人民币", "rmb",
	"模式", "策略", "主动", "跟价", "追价", "跟随", "保守",
	// 新增停用词：避免把助词当关键词
	"价格", "以下", "以下", "以内", "以上", "左右", "的时候", "的时候",
	"可以", "能够", "的话", "除非", "否则", "但是", "但",
}

// LLM 配置（OpenAI 兼容 /v1/chat/completions）。空 APIKey 表示禁用 LLM。
type llmConfig struct {
	apiKey  string
	baseURL string
	model   string
}

func loadLLMConfig() llmConfig {
	return llmConfig{
		apiKey:  strings.TrimSpace(os.Getenv("AGENT_LLM_API_KEY")),
		baseURL: strings.TrimRight(envOr("AGENT_LLM_BASE_URL", "https://api.openai.com"), "/"),
		model:   envOr("AGENT_LLM_MODEL", "gpt-4o-mini"),
	}
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// ParseIntent 解析买家意图，产出结构化策略。
// explicit* 为调用方在表单里显式填写的值，优先级最高。
func ParseIntent(ctx context.Context, prompt string, explicitBudgetCents int64, explicitKeywords []string) ParsedStrategy {
	prompt = strings.TrimSpace(prompt)
	strategy := ParsedStrategy{Prompt: prompt}

	// 1. LLM 优先（可选）。
	if cfg := loadLLMConfig(); cfg.apiKey != "" && prompt != "" {
		if parsed, err := llmParseIntent(ctx, cfg, prompt); err == nil && parsed != nil {
			strategy.MaxBudgetCents = parsed.MaxBudgetCents
			strategy.ProductKeywords = normalizeKeywords(parsed.ProductKeywords)
			// LLM 可能返回新维度字段
			if parsed.Trigger != "" {
				strategy.Trigger = parsed.Trigger
			}
			if parsed.Pace != "" {
				strategy.Pace = parsed.Pace
			}
			if parsed.StopRatio > 0 {
				strategy.StopRatio = parsed.StopRatio
			}
		}
	}

	// 2. 规则引擎补全 LLM 未给出的字段。
	if strategy.MaxBudgetCents <= 0 {
		strategy.MaxBudgetCents = parseBudgetCents(prompt)
	}
	if len(strategy.ProductKeywords) == 0 {
		strategy.ProductKeywords = extractKeywords(prompt)
	}
	if strategy.Strategy == "" {
		strategy.Strategy = strategyFromPrompt(prompt)
	}
	if strategy.MaxBidTimes <= 0 {
		strategy.MaxBidTimes = parseMaxBidTimes(prompt)
	}
	if strategy.MinIntervalMs <= 0 {
		strategy.MinIntervalMs = parseMinIntervalMs(prompt)
	}
	// 新维度字段：如果 LLM 没给，从规则引擎解析
	if strategy.Trigger == "" {
		strategy.Trigger = triggerFromPrompt(prompt)
	}
	if strategy.Pace == "" {
		strategy.Pace = paceFromPrompt(prompt)
	}
	if strategy.StopRatio == 0 {
		strategy.StopRatio = parseBudgetRatio(prompt)
	}
	strategy.RequireHumanPay = true

	// 3. 调用方显式值覆盖一切。
	if explicitBudgetCents > 0 {
		strategy.MaxBudgetCents = explicitBudgetCents
	}
	if len(explicitKeywords) > 0 {
		strategy.ProductKeywords = normalizeKeywords(explicitKeywords)
	}
	return strategy
}

// parseBudgetCents 从中文 prompt 中识别预算，返回分；识别不到返回 0。
// 取「最高/不超过/预算/封顶」附近的金额；否则取 prompt 中最大的合理金额。
func parseBudgetCents(prompt string) int64 {
	if prompt == "" {
		return 0
	}
	matches := budgetPattern.FindAllStringSubmatch(prompt, -1)
	var best int64
	for _, m := range matches {
		raw := m[1]
		unit := m[2]
		val, err := strconv.ParseFloat(raw, 64)
		if err != nil || val <= 0 {
			continue
		}
		yuan := val
		switch strings.ToLower(unit) {
		case "万", "w":
			yuan = val * 10000
		case "千", "k":
			yuan = val * 1000
		case "", "元", "块", "rmb", "￥", "$":
			// 无单位的裸数字（如纯计数）忽略，避免误把「一件」的数字当价格。
			if unit == "" && val < 5 {
				continue
			}
		}
		cents := int64(yuan * 100)
		if cents > best {
			best = cents
		}
	}
	return best
}

// extractKeywords 从 prompt 中抽取商品关键词（无 LLM 时的内置回退）。
// 策略：移除金额片段和停用词，保留剩余的 CJK / 英文连续整段作为关键词。
func extractKeywords(prompt string) []string {
	if prompt == "" {
		return nil
	}
	cleaned := budgetPattern.ReplaceAllString(prompt, " ")
	for _, sw := range stopwordRunes {
		cleaned = strings.ReplaceAll(cleaned, sw, " ")
	}
	// 按非 CJK / 标点切分，保留连续的中文或英文整段。
	fields := strings.FieldsFunc(cleaned, func(r rune) bool {
		return !isCJK(r) && !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') && !(r >= '0' && r <= '9')
	})

	seen := map[string]bool{}
	var keywords []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if len([]rune(f)) < 2 || seen[f] {
			continue
		}
		seen[f] = true
		keywords = append(keywords, f)
	}
	if len(keywords) > 6 {
		keywords = keywords[:6]
	}
	return keywords
}

func isCJK(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

// --- LLM 调用 ---

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

const llmSystemPrompt = `你是拍卖 Agent 策略模板解析器。把用户中文需求解析成 JSON，只允许输出这些字段：
{"maxBudgetCents": <整数，最高预算，单位分，1元=100分；无法判断填0>, "productKeywords": [<商品关键词字符串数组，2-6个，尽量短，便于子串匹配>], "trigger": "lead|follow", "pace": "min_step|reserve", "stopRatio": <0到1之间，0表示仅受预算限制；无法判断填0>, "maxBidTimes": <整数；无法判断填0>, "minIntervalMs": <整数毫秒；无法判断填0>, "requireHumanPay": true}
字段说明：
- trigger: lead=主动出价（默认），follow=跟价模式（有人先出价才跟）
- pace: min_step=最小步长加价（默认），reserve=保留价优先（先一次出到保留价）
- stopRatio: 0=仅受预算硬约束，0.6=预算60%时停止，0.8=预算80%时停止
- productKeywords: 只填商品名/品类词，不要填「价格」「以下」等助词
绝对不要输出自动支付策略；requireHumanPay 必须是 true。只输出 JSON，不要解释。`

func llmParseIntent(ctx context.Context, cfg llmConfig, prompt string) (*ParsedStrategy, error) {
	reqBody := chatRequest{
		Model:       cfg.model,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: llmSystemPrompt},
			{Role: "user", Content: prompt},
		},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var parsed chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, err
	}
	if len(parsed.Choices) == 0 {
		return nil, errEmptyLLM
	}
	content := strings.TrimSpace(parsed.Choices[0].Message.Content)
	content = stripCodeFence(content)

	var out struct {
		MaxBudgetCents  int64     `json:"maxBudgetCents"`
		ProductKeywords []string  `json:"productKeywords"`
		Trigger         string    `json:"trigger"`
		Pace            string    `json:"pace"`
		StopRatio       float64   `json:"stopRatio"`
		MaxBidTimes     int       `json:"maxBidTimes"`
		MinIntervalMs   int64     `json:"minIntervalMs"`
		RequireHumanPay bool      `json:"requireHumanPay"`
		Custom          CustomStrategySkill `json:"custom"`
	}
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, err
	}
	return &ParsedStrategy{
		MaxBudgetCents:  out.MaxBudgetCents,
		ProductKeywords: out.ProductKeywords,
		Trigger:         out.Trigger,
		Pace:            out.Pace,
		StopRatio:       out.StopRatio,
		MaxBidTimes:     out.MaxBidTimes,
		MinIntervalMs:   out.MinIntervalMs,
		RequireHumanPay: true,
		Custom:          out.Custom,
	}, nil
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

var errEmptyLLM = &parseError{"llm returned no choices"}

type parseError struct{ msg string }

func (e *parseError) Error() string { return e.msg }
