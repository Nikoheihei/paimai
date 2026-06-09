package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	agentpkg "paimai/internal/agent"
	"paimai/internal/service"
	"paimai/pkg/response"
)

// AgentHandler exposes buyer-agent, Pact, audit, and merchant-ops wrapper routes.
type AgentHandler struct {
	agentService *agentpkg.Service
	adminService *service.AdminService
}

func RegisterAgentRoutes(r gin.IRouter, agentService *agentpkg.Service) {
	h := &AgentHandler{agentService: agentService}
	r.POST("/api/agent/buyer-agents", h.createBuyerAgent)
	r.GET("/api/agent/buyer-agents", h.listBuyerAgents)
	r.PATCH("/api/agent/buyer-agents/:id/activate", h.activateAgent)
	r.PATCH("/api/agent/buyer-agents/:id/pause", h.pauseAgent)
	r.POST("/api/agent/buyer-agents/:id/bids", h.submitAgentBid)
	r.GET("/api/agent/buyer-agents/:id/audit", h.listAgentAudit)

	r.GET("/api/agent/pacts", h.listPacts)
	r.GET("/api/agent/pacts/:id", h.getPact)
	r.POST("/api/agent/pacts/:id/approve", h.approvePact)
	r.POST("/api/agent/pacts/:id/reject", h.rejectPact)
	r.POST("/api/agent/pact-observer/from-win", h.createPactFromWin)

	r.POST("/api/agent/product-release/orders/:id/record", h.recordProductReleased)
}

func RegisterMerchantAgentRoutes(r gin.IRouter, agentService *agentpkg.Service, adminService *service.AdminService) {
	h := &AgentHandler{agentService: agentService, adminService: adminService}
	r.POST("/agent/merchant-agents", h.createMerchantAgent)
	r.GET("/agent/merchant-agents", h.listMerchantAgents)
	r.POST("/agent/merchant/reports", h.createMerchantReport)
	r.POST("/agent/merchant/products/:id/relist", h.relistProductByMerchantAgent)
	r.POST("/agent/merchant/products/:id/offline", h.offlineProductByMerchantAgent)
}



func (h *AgentHandler) createBuyerAgent(c *gin.Context) {
	var input agentpkg.CreateBuyerAgentInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.agentService.CreateBuyerAgent(c.Request.Context(), mustGetUserID(c), input)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) listBuyerAgents(c *gin.Context) {
	result, err := h.agentService.ListAgents(c.Request.Context(), mustGetUserID(c), agentpkg.AgentTypeBuyer)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) createMerchantAgent(c *gin.Context) {
	var input agentpkg.CreateMerchantAgentInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.agentService.CreateMerchantOpsAgent(c.Request.Context(), mustGetUserID(c), input)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) listMerchantAgents(c *gin.Context) {
	result, err := h.agentService.ListAgents(c.Request.Context(), mustGetUserID(c), agentpkg.AgentTypeMerchantOps)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) activateAgent(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	result, err := h.agentService.ActivateAgent(c.Request.Context(), mustGetUserID(c), id)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) pauseAgent(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	result, err := h.agentService.PauseAgent(c.Request.Context(), mustGetUserID(c), id)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) submitAgentBid(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input agentpkg.AgentBidInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.agentService.SubmitBuyerBid(c.Request.Context(), mustGetUserID(c), id, input)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) listAgentAudit(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	limit, ok := optionalIntQuery(c, "limit")
	if !ok {
		return
	}
	result, err := h.agentService.ListAuditLogs(c.Request.Context(), mustGetUserID(c), id, limit)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) listPacts(c *gin.Context) {
	result, err := h.agentService.ListPacts(c.Request.Context(), mustGetUserID(c))
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) getPact(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	result, err := h.agentService.GetPact(c.Request.Context(), mustGetUserID(c), id)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) approvePact(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input agentpkg.PactApprovalInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.agentService.ApprovePact(c.Request.Context(), mustGetUserID(c), id, input)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) rejectPact(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	result, err := h.agentService.RejectPact(c.Request.Context(), mustGetUserID(c), id)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) createPactFromWin(c *gin.Context) {
	var input struct {
		AgentID   uint64 `json:"agentId"`
		AuctionID uint64 `json:"auctionId"`
		TraceID   string `json:"traceId"`
	}
	if !bindJSON(c, &input) {
		return
	}
	if input.AgentID == 0 || input.AuctionID == 0 {
		response.Error(c, http.StatusBadRequest, 400, "agentId and auctionId are required")
		return
	}
	result, err := h.agentService.CreatePactFromWin(c.Request.Context(), input.AgentID, input.AuctionID, input.TraceID)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) recordProductReleased(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	err := h.agentService.RecordProductReleased(c.Request.Context(), id, mustGetUserID(c))
	writeAgentResult(c, gin.H{"recorded": err == nil}, err)
}

func (h *AgentHandler) createMerchantReport(c *gin.Context) {
	var input agentpkg.MerchantReportInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.agentService.CreateMerchantReportJob(c.Request.Context(), mustGetUserID(c), input)
	writeAgentResult(c, result, err)
}

func (h *AgentHandler) relistProductByMerchantAgent(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input service.AuctionInput
	if !bindJSON(c, &input) {
		return
	}
	auction, err := h.adminService.RelistProduct(c.Request.Context(), mustGetUserID(c), id, input)
	if err == nil {
		err = h.agentService.RecordMerchantProductRelisted(c.Request.Context(), mustGetUserID(c), id, auction.ID)
	}
	writeAgentResult(c, auction, err)
}

func (h *AgentHandler) offlineProductByMerchantAgent(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	product, err := h.adminService.OfflineProduct(c.Request.Context(), mustGetUserID(c), id)
	if err == nil {
		err = h.agentService.RecordMerchantProductOffline(c.Request.Context(), mustGetUserID(c), id)
	}
	writeAgentResult(c, product, err)
}

func writeAgentResult(c *gin.Context, data interface{}, err error) {
	switch {
	case err == nil:
		response.Success(c, data)
	case errors.Is(err, agentpkg.ErrAgentForbidden):
		response.Error(c, http.StatusForbidden, 403, err.Error())
	case errors.Is(err, agentpkg.ErrAuditRequired):
		response.Error(c, http.StatusInternalServerError, 500, err.Error())
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.Error(c, http.StatusNotFound, 404, "resource not found")
	default:
		writeResult(c, data, err)
	}
}
