package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"paimai/internal/model"
	"paimai/internal/service"
)

const (
	// 策略名称：保留旧名称以兼容已存储的数据，但内部决策逻辑统一由3维度驱动
	StrategyConservative      = "conservative"
	StrategyFollowUp          = "follow_up"
	StrategyReserveThenFollow = "reserve_then_follow"
	StrategyCapOnly           = "cap_only"
	StrategyCustom            = "custom"

	DefaultStrategy      = StrategyCustom
	DefaultMaxBidTimes   = 100
	DefaultMinIntervalMs = int64(3000)
)

var budgetRatioPattern = regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)\s*(%|％|成)`)

// --- 触发方式（Trigger）：决定 Agent 是否主动出价 ---

const (
	TriggerLead   = "lead"   // 主动出价：有人出价也出，没人出价也出
	TriggerFollow = "follow" // 跟价模式：必须有人先出价才跟
)

// --- 出价节奏（Pace）：决定出价金额的计算方式 ---

const (
	PaceMinStep = "min_step" // 最小步长：当前价 + 加价幅度
	PaceReserve = "reserve"  // 保留价优先：先一次出到保留价，再按最小步长跟
)

// --- 停止条件（StopAt）：决定何时停止出价 ---

const (
	StopAtBudget = "budget" // 到达预算上限时停止
	StopAtRatio  = "ratio"  // 到达预算的X%时停止（0-1）
)

// StrategySkill is the runtime contract for buyer-agent bidding. LLM and
// natural-language parsing may fill this shape, but only this deterministic
// template is allowed to drive bids.
type StrategySkill struct {
	Prompt          string   `json:"prompt,omitempty"`
	ProductKeywords []string `json:"productKeywords,omitempty"`
	BuyerID         uint64   `json:"buyerId"`
	RoomID          uint64   `json:"roomId,omitempty"`
	AuctionID       uint64   `json:"auctionId,omitempty"`
	MaxBudgetCents  int64    `json:"maxBudgetCents"`
	Strategy        string   `json:"strategy"` // 保留旧字段，兼容已存储数据
	MaxBidTimes     int      `json:"maxBidTimes"`
	MinIntervalMs   int64    `json:"minIntervalMs"`
	RequireHumanPay bool     `json:"requireHumanPay"`

	// --- 新3维度字段 ---

	// Trigger 出价触发方式：lead（主动出价，默认）或 follow（跟价模式）
	Trigger string `json:"trigger,omitempty"`
	// Pace 出价节奏：min_step（最小步长，默认）或 reserve（保留价优先）
	Pace string `json:"pace,omitempty"`
	// StopRatio 停止比例：0表示仅受预算硬约束，0-1表示到达预算的X%时停止
	StopRatio float64 `json:"stopRatio,omitempty"`

	// OverBudgetCents 仅 cap_only 策略：允许超出预算的额度（分）。已折算进 MaxBudgetCents 作为有效上限，此处仅用于展示。
	OverBudgetCents int64 `json:"overBudgetCents,omitempty"`
	// BaseBudgetCents 折算超预算前的原始预算（分），用于展示。
	BaseBudgetCents int64 `json:"baseBudgetCents,omitempty"`
	// CustomText 自定义策略的自然语言文本，用于展示与回放。
	CustomText string `json:"customText,omitempty"`

	// --- 旧字段（兼容已存储数据，读取时回填新字段） ---
	Custom CustomStrategySkill `json:"custom,omitempty"`
}

// ParsedStrategy is kept as an alias for the parser and old call sites.
type ParsedStrategy = StrategySkill

type CustomStrategySkill struct {
	BudgetRatio  float64 `json:"budgetRatio,omitempty"`
	FollowUp     bool    `json:"followUp,omitempty"`
	ReserveFirst bool    `json:"reserveFirst,omitempty"`
}

// resolveDimensionalFields 从旧 strategy + custom 字段解析出新的3维度字段。
// 只在对应维度字段为空时才回填，确保新数据优先。
func (s *StrategySkill) resolveDimensionalFields() {
	// 从旧 strategy + custom 字段回填
	switch s.Strategy {
	case StrategyConservative:
		if s.Trigger == "" {
			s.Trigger = TriggerLead
		}
		if s.Pace == "" {
			s.Pace = PaceMinStep
		}
		if s.StopRatio == 0 {
			s.StopRatio = 0.6
		}
	case StrategyFollowUp:
		if s.Trigger == "" {
			s.Trigger = TriggerFollow
		}
		if s.Pace == "" {
			s.Pace = PaceMinStep
		}
	case StrategyReserveThenFollow:
		if s.Trigger == "" {
			s.Trigger = TriggerFollow
		}
		if s.Pace == "" {
			s.Pace = PaceReserve
		}
	case StrategyCapOnly:
		if s.Trigger == "" {
			s.Trigger = TriggerLead
		}
		if s.Pace == "" {
			s.Pace = PaceMinStep
		}
	case StrategyCustom:
		if s.Trigger == "" {
			if s.Custom.FollowUp {
				s.Trigger = TriggerFollow
			} else {
				s.Trigger = TriggerLead
			}
		}
		if s.Pace == "" {
			if s.Custom.ReserveFirst {
				s.Pace = PaceReserve
			} else {
				s.Pace = PaceMinStep
			}
		}
		if s.StopRatio == 0 && s.Custom.BudgetRatio > 0 {
			s.StopRatio = s.Custom.BudgetRatio
		}
	default:
		// 未知策略，默认最安全：主动出价 + 最小步长
		if s.Trigger == "" {
			s.Trigger = TriggerLead
		}
		if s.Pace == "" {
			s.Pace = PaceMinStep
		}
	}
}

func buildStrategySkill(buyerID uint64, input CreateBuyerAgentInput, parsed ParsedStrategy) (StrategySkill, error) {
	if input.BuyerID != 0 && input.BuyerID != buyerID {
		return StrategySkill{}, service.ErrUnauthorized
	}
	if wantsAutoPay(input.Prompt) || (input.RequireHumanPay != nil && !*input.RequireHumanPay) {
		return StrategySkill{}, fmt.Errorf("%w: agent payment must require human approval", service.ErrInvalidInput)
	}

	skill := parsed
	skill.BuyerID = buyerID
	skill.RoomID = firstUint64(input.RoomID, parsed.RoomID)
	skill.AuctionID = firstUint64(input.AuctionID, parsed.AuctionID)
	baseBudget := firstInt64(input.MaxBudgetCents, parsed.MaxBudgetCents)
	skill.MaxBudgetCents = baseBudget

	// 新3维度优先级：API显式传入 > 解析结果 > 旧策略回填 > 默认值
	skill.Trigger = firstString(input.Trigger, parsed.Trigger)
	skill.Pace = firstString(input.Pace, parsed.Pace)
	if input.StopRatio > 0 {
		skill.StopRatio = input.StopRatio
	} else if parsed.StopRatio > 0 {
		skill.StopRatio = parsed.StopRatio
	}

	// 策略名称：优先用新维度字段推导；否则从用户显式传入或解析结果取
	// 注意：需要先用旧策略回填新维度字段，再推导策略名
	legacyStrategy := firstString(input.Strategy, parsed.Strategy)
	if legacyStrategy != "" && skill.Strategy == "" {
		skill.Strategy = legacyStrategy
	}
	// 回填新维度字段（从旧策略名推导）
	skill.resolveDimensionalFields()

	// 如果新维度字段已就位，用它们重新推导策略名；cap_only 必须保留旧策略名，
	// 因为它有 over-budget 语义，不能被 lead/min_step 覆盖。
	if legacyStrategy == StrategyCapOnly {
		skill.Strategy = StrategyCapOnly
	} else if skill.Trigger != "" && skill.Pace != "" {
		skill.Strategy = deriveStrategyName(skill.Trigger, skill.Pace, skill.StopRatio)
	} else if skill.Strategy == "" {
		skill.Strategy = DefaultStrategy
	}

	skill.MaxBidTimes = firstInt(input.MaxBidTimes, parsed.MaxBidTimes, DefaultMaxBidTimes)
	skill.MinIntervalMs = firstInt64(input.MinIntervalMs, parsed.MinIntervalMs, DefaultMinIntervalMs)
	skill.RequireHumanPay = true

	// cap_only：允许超出预算的额度折算进有效上限
	if skill.Strategy == StrategyCapOnly && input.OverBudgetCents > 0 {
		skill.BaseBudgetCents = baseBudget
		skill.OverBudgetCents = input.OverBudgetCents
		skill.MaxBudgetCents = baseBudget + input.OverBudgetCents
	}

	// 自定义文本保留
	if input.CustomText != "" {
		skill.CustomText = strings.TrimSpace(input.CustomText)
		if strings.Contains(skill.CustomText, "跟价") || strings.Contains(strings.ToLower(skill.CustomText), "follow") {
			skill.Custom.FollowUp = true
			skill.Trigger = TriggerFollow
		}
	} else if parsed.CustomText != "" {
		skill.CustomText = parsed.CustomText
	}

	// 确保维度字段完整
	skill.resolveDimensionalFields()

	if err := validateStrategySkill(skill); err != nil {
		return StrategySkill{}, err
	}
	return skill, nil
}

// deriveStrategyName 从3维度字段推导策略名称（兼容旧前端显示）
func deriveStrategyName(trigger, pace string, stopRatio float64) string {
	switch {
	case trigger == TriggerFollow && pace == PaceReserve:
		return StrategyReserveThenFollow
	case trigger == TriggerFollow:
		return StrategyFollowUp
	case trigger == TriggerLead && pace == PaceReserve:
		return StrategyReserveThenFollow
	case trigger == TriggerLead && stopRatio > 0 && stopRatio < 1:
		return StrategyConservative
	default:
		// lead + min_step + 无stopRatio = 最基础策略
		return StrategyCustom
	}
}

func decodeStrategySkill(agent *model.AgentProfile) (StrategySkill, error) {
	if agent == nil {
		return StrategySkill{}, fmt.Errorf("%w: missing agent", service.ErrInvalidInput)
	}
	var skill StrategySkill
	if err := json.Unmarshal([]byte(agent.StrategyJSON), &skill); err != nil {
		return StrategySkill{}, err
	}
	if skill.BuyerID == 0 {
		skill.BuyerID = agent.OwnerUserID
	}
	if skill.MaxBudgetCents <= 0 {
		skill.MaxBudgetCents = agent.MaxBudgetCents
	}
	if skill.Strategy == "" {
		skill.Strategy = DefaultStrategy
	}
	if skill.MaxBidTimes <= 0 {
		skill.MaxBidTimes = DefaultMaxBidTimes
	}
	if skill.MinIntervalMs <= 0 {
		skill.MinIntervalMs = DefaultMinIntervalMs
	}
	if !skill.RequireHumanPay {
		skill.RequireHumanPay = true
	}
	// 回填新维度字段
	skill.resolveDimensionalFields()
	return skill, validateStrategySkill(skill)
}

func validateStrategySkill(skill StrategySkill) error {
	if skill.BuyerID == 0 {
		return fmt.Errorf("%w: buyerId is required", service.ErrInvalidInput)
	}
	if skill.MaxBudgetCents <= 0 {
		return fmt.Errorf("%w: maxBudgetCents is required", service.ErrInvalidInput)
	}
	// 兼容旧策略名
	if !isSupportedStrategy(skill.Strategy) {
		return fmt.Errorf("%w: unsupported agent strategy %q", service.ErrInvalidInput, skill.Strategy)
	}
	if skill.MaxBidTimes <= 0 {
		return fmt.Errorf("%w: maxBidTimes must be positive", service.ErrInvalidInput)
	}
	if skill.MaxBidTimes > 100 {
		return fmt.Errorf("%w: maxBidTimes is too large", service.ErrInvalidInput)
	}
	if skill.MinIntervalMs < 0 {
		return fmt.Errorf("%w: minIntervalMs must be non-negative", service.ErrInvalidInput)
	}
	if !skill.RequireHumanPay {
		return fmt.Errorf("%w: requireHumanPay must be true", service.ErrInvalidInput)
	}
	// 验证新维度字段
	if skill.Trigger != TriggerLead && skill.Trigger != TriggerFollow {
		return fmt.Errorf("%w: invalid trigger %q, must be lead or follow", service.ErrInvalidInput, skill.Trigger)
	}
	if skill.Pace != PaceMinStep && skill.Pace != PaceReserve {
		return fmt.Errorf("%w: invalid pace %q, must be min_step or reserve", service.ErrInvalidInput, skill.Pace)
	}
	if skill.StopRatio < 0 || skill.StopRatio > 1 {
		return fmt.Errorf("%w: stopRatio must be between 0 and 1", service.ErrInvalidInput)
	}
	return nil
}

func strategyFromPrompt(prompt string) string {
	text := strings.ToLower(strings.TrimSpace(prompt))
	switch {
	case text == "":
		return ""
	case strings.Contains(text, "自定义") || strings.Contains(text, "定制") || strings.Contains(text, "个性化") || strings.Contains(text, "custom"):
		return StrategyCustom
	case strings.Contains(text, "保留价"):
		return StrategyReserveThenFollow
	case strings.Contains(text, "跟价") || strings.Contains(text, "追价") || strings.Contains(text, "被超过") || strings.Contains(text, "follow"):
		return StrategyFollowUp
	case strings.Contains(text, "封顶") || strings.Contains(text, "只保证不超过") || strings.Contains(text, "cap"):
		return StrategyCapOnly
	case strings.Contains(text, "保守") || strings.Contains(text, "conservative"):
		return StrategyConservative
	default:
		return ""
	}
}

// triggerFromPrompt 从自然语言解析触发方式
func triggerFromPrompt(prompt string) string {
	text := strings.ToLower(prompt)
	if strings.Contains(text, "跟价") || strings.Contains(text, "追价") || strings.Contains(text, "有人先出") ||
		strings.Contains(text, "跟着出") || strings.Contains(text, "follow") {
		return TriggerFollow
	}
	return TriggerLead
}

// paceFromPrompt 从自然语言解析出价节奏
func paceFromPrompt(prompt string) string {
	text := strings.ToLower(prompt)
	if strings.Contains(text, "保留价") || strings.Contains(text, "先到保留") {
		return PaceReserve
	}
	return PaceMinStep
}

func parseMaxBidTimes(prompt string) int {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`最多\s*([0-9]+)\s*次`),
		regexp.MustCompile(`不超过\s*([0-9]+)\s*次`),
		regexp.MustCompile(`([0-9]+)\s*次出价`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(prompt); len(match) == 2 {
			if v, err := strconv.Atoi(match[1]); err == nil {
				return v
			}
		}
	}
	return 0
}

func parseMinIntervalMs(prompt string) int64 {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`间隔\s*([0-9]+)\s*(毫秒|ms|秒|s)?`),
		regexp.MustCompile(`每\s*([0-9]+)\s*(毫秒|ms|秒|s)`),
	}
	for _, pattern := range patterns {
		if match := pattern.FindStringSubmatch(prompt); len(match) >= 2 {
			v, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil {
				continue
			}
			unit := ""
			if len(match) > 2 {
				unit = strings.ToLower(match[2])
			}
			if unit == "秒" || unit == "s" || unit == "" {
				return int64((time.Duration(v) * time.Second) / time.Millisecond)
			}
			return v
		}
	}
	return 0
}

func parseCustomSkill(prompt string, strategy string) CustomStrategySkill {
	text := strings.ToLower(prompt)
	custom := CustomStrategySkill{
		FollowUp:     strings.Contains(text, "跟") || strings.Contains(text, "追") || strings.Contains(text, "follow"),
		ReserveFirst: strings.Contains(text, "保留价"),
	}
	// 注意：不再默认 followUp = true
	// 如果用户没指定任何规则，followUp 保持 false
	custom.BudgetRatio = parseBudgetRatio(prompt)
	return custom
}

func normalizeCustomSkill(custom CustomStrategySkill, prompt string) CustomStrategySkill {
	if custom.BudgetRatio <= 0 {
		custom.BudgetRatio = parseBudgetRatio(prompt)
	}
	if custom.BudgetRatio > 1 {
		custom.BudgetRatio = 1
	}
	// 不再强制 followUp = true
	return custom
}

func parseBudgetRatio(prompt string) float64 {
	if match := budgetRatioPattern.FindStringSubmatch(prompt); len(match) == 3 {
		val, err := strconv.ParseFloat(match[1], 64)
		if err == nil && val > 0 {
			if match[2] == "成" {
				return val / 10
			}
			if val > 1 {
				return val / 100
			}
			return val
		}
	}
	return 0
}

func wantsAutoPay(prompt string) bool {
	text := strings.TrimSpace(prompt)
	return strings.Contains(text, "自动支付") || strings.Contains(text, "自动付款") || strings.Contains(strings.ToLower(text), "auto pay")
}

func isSupportedStrategy(strategy string) bool {
	switch strategy {
	case StrategyConservative, StrategyFollowUp, StrategyReserveThenFollow, StrategyCapOnly, StrategyCustom:
		return true
	default:
		return false
	}
}

func firstUint64(values ...uint64) uint64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func firstInt64(values ...int64) int64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func firstInt(values ...int) int {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func firstString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
