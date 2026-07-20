package http

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/webhook"
)

type WebhookHandler struct {
	svc *webhook.Service
}

func NewWebhookHandler(svc *webhook.Service) *WebhookHandler {
	return &WebhookHandler{svc: svc}
}

func (h *WebhookHandler) RegisterRoutes(rg *gin.RouterGroup) {
	g := rg.Group("/tenants/:tenantId/webhooks")
	g.GET("", h.List)
	g.POST("", h.Create)
	g.DELETE("/:webhookId", h.Delete)
}

func (h *WebhookHandler) List(c *gin.Context) {
	tenantID := c.Param("tenantId")
	subs, err := h.svc.ListSubscriptions(c.Request.Context(), tenantID)
	if err != nil {
		middleware.RespondErrorCode(c, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	middleware.RespondSuccess(c, http.StatusOK, gin.H{"items": subs, "total": len(subs)})
}

func (h *WebhookHandler) Create(c *gin.Context) {
	tenantID := c.Param("tenantId")
	var req struct {
		Name       string   `json:"name"        binding:"required"`
		URL        string   `json:"url"         binding:"required,url"`
		Secret     string   `json:"secret"      binding:"required,min=16"`
		EventTypes []string `json:"event_types" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.RespondErrorCode(c, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	sub, err := h.svc.CreateSubscription(c.Request.Context(), tenantID, req.Name, req.URL, req.Secret, req.EventTypes)
	if err != nil {
		middleware.RespondErrorCode(c, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	middleware.RespondSuccess(c, http.StatusOK, sub)
}

func (h *WebhookHandler) Delete(c *gin.Context) {
	webhookID := c.Param("webhookId")
	if err := h.svc.DeleteSubscription(c.Request.Context(), webhookID); err != nil {
		middleware.RespondErrorCode(c, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	middleware.RespondSuccess(c, http.StatusOK, gin.H{"deleted": webhookID})
}

