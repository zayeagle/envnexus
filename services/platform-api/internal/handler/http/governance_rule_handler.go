package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/governance"
)

type GovernanceRuleHandler struct {
	svc *governance.RuleService
}

func NewGovernanceRuleHandler(svc *governance.RuleService) *GovernanceRuleHandler {
	return &GovernanceRuleHandler{svc: svc}
}

func (h *GovernanceRuleHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.POST("/tenants/:tenantId/governance-rules", h.CreateRule)
	router.GET("/tenants/:tenantId/governance-rules", h.ListRules)
	router.GET("/tenants/:tenantId/governance-rules/:ruleId", h.GetRule)
	router.PUT("/tenants/:tenantId/governance-rules/:ruleId", h.UpdateRule)
	router.DELETE("/tenants/:tenantId/governance-rules/:ruleId", h.DeleteRule)

	router.POST("/tenants/:tenantId/tool-permissions", h.CreateToolPermission)
	router.GET("/tenants/:tenantId/tool-permissions", h.ListToolPermissions)
	router.GET("/tenants/:tenantId/tool-permissions/:permId", h.GetToolPermission)
	router.PUT("/tenants/:tenantId/tool-permissions/:permId", h.UpdateToolPermission)
	router.DELETE("/tenants/:tenantId/tool-permissions/:permId", h.DeleteToolPermission)
}

type createRuleReq struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	RuleType    string `json:"rule_type" binding:"required"`
	Condition   string `json:"condition" binding:"required"`
	Action      string `json:"action"`
	Severity    string `json:"severity"`
}

func (h *GovernanceRuleHandler) CreateRule(c *gin.Context) {
	tenantID := c.Param("tenantId")
	var req createRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	userID, _ := c.Get("user_id")
	uid, _ := userID.(string)
	severity := req.Severity
	if severity == "" {
		severity = "warning"
	}
	rule, err := h.svc.CreateRule(c.Request.Context(), tenantID, req.Name, req.Description, req.RuleType, req.Condition, req.Action, severity, uid)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, rule)
}

func (h *GovernanceRuleHandler) ListRules(c *gin.Context) {
	tenantID := c.Param("tenantId")
	rules, err := h.svc.ListRules(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": rules})
}

func (h *GovernanceRuleHandler) GetRule(c *gin.Context) {
	ruleID := c.Param("ruleId")
	rule, err := h.svc.GetRule(c.Request.Context(), ruleID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, rule)
}

type updateRuleReq struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Condition   string `json:"condition"`
	Action      string `json:"action"`
	Severity    string `json:"severity"`
	Enabled     *bool  `json:"enabled"`
}

func (h *GovernanceRuleHandler) UpdateRule(c *gin.Context) {
	ruleID := c.Param("ruleId")
	var req updateRuleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	rule, err := h.svc.UpdateRule(c.Request.Context(), ruleID, req.Name, req.Description, req.Condition, req.Action, req.Severity, req.Enabled)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, rule)
}

func (h *GovernanceRuleHandler) DeleteRule(c *gin.Context) {
	ruleID := c.Param("ruleId")
	if err := h.svc.DeleteRule(c.Request.Context(), ruleID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"deleted": true})
}

type createToolPermReq struct {
	ToolName string  `json:"tool_name" binding:"required"`
	RoleID   *string `json:"role_id"`
	Allowed  bool    `json:"allowed"`
	MaxRisk  string  `json:"max_risk"`
}

type updateToolPermReq struct {
	ToolName string  `json:"tool_name"`
	RoleID   *string `json:"role_id"`
	Allowed  bool    `json:"allowed"`
	MaxRisk  string  `json:"max_risk"`
}

func (h *GovernanceRuleHandler) CreateToolPermission(c *gin.Context) {
	tenantID := c.Param("tenantId")
	var req createToolPermReq
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	perm, err := h.svc.CreateToolPermission(c.Request.Context(), tenantID, req.ToolName, req.RoleID, req.Allowed, req.MaxRisk)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, perm)
}

func (h *GovernanceRuleHandler) ListToolPermissions(c *gin.Context) {
	tenantID := c.Param("tenantId")
	perms, err := h.svc.ListToolPermissions(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": perms})
}

func (h *GovernanceRuleHandler) DeleteToolPermission(c *gin.Context) {
	permID := c.Param("permId")
	if err := h.svc.DeleteToolPermission(c.Request.Context(), permID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"deleted": true})
}

func (h *GovernanceRuleHandler) UpdateToolPermission(c *gin.Context) {
	permID := c.Param("permId")
	var req updateToolPermReq
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	perm, err := h.svc.UpdateToolPermission(c.Request.Context(), permID, req.ToolName, req.RoleID, req.Allowed, req.MaxRisk)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, perm)
}

func (h *GovernanceRuleHandler) GetToolPermission(c *gin.Context) {
	permID := c.Param("permId")
	perm, err := h.svc.GetToolPermission(c.Request.Context(), permID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, perm)
}
