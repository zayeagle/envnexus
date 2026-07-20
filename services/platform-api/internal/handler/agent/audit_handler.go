package agent

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/audit"
)

type AuditHandler struct {
	auditService *audit.Service
}

func NewAuditHandler(auditService *audit.Service) *AuditHandler {
	return &AuditHandler{auditService: auditService}
}

func (h *AuditHandler) RegisterRoutes(router *gin.RouterGroup) {
	agentGroup := router.Group("/agent/v1")
	{
		agentGroup.POST("/audit-events", h.ReportEvents)
	}
}

func (h *AuditHandler) ReportEvents(c *gin.Context) {
	var req dto.AgentAuditEventsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}

	tenantID, _ := c.Get("tenant_id")
	tid, _ := tenantID.(string)
	deviceID, _ := c.Get("device_id")
	did, _ := deviceID.(string)

	if tid == "" {
		tid = c.GetHeader("X-Tenant-ID")
	}
	if did == "" {
		did = c.GetHeader("X-Device-ID")
	}

	var events []*domain.AuditEvent
	for _, item := range req.Events {
		payloadBytes, _ := json.Marshal(item.EventPayload)
		var sessionPtr *string
		if item.SessionID != "" {
			s := item.SessionID
			sessionPtr = &s
		}
		var devicePtr *string
		if did != "" {
			d := did
			devicePtr = &d
		}
		events = append(events, &domain.AuditEvent{
			ID:               ulid.Make().String(),
			TenantID:         tid,
			DeviceID:         devicePtr,
			SessionID:        sessionPtr,
			EventType:        item.EventType,
			EventPayloadJSON: string(payloadBytes),
			CreatedAt:        time.Now(),
		})
	}

	if err := h.auditService.BulkCreate(c.Request.Context(), events); err != nil {
		mw.RespondError(c, domain.ErrInternalError)
		return
	}

	mw.RespondSuccess(c, http.StatusCreated, gin.H{
		"accepted": len(events),
	})
}
