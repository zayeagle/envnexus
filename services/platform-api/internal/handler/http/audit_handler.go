package http

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/audit"
)

type AuditHandler struct {
	auditService *audit.Service
}

func NewAuditHandler(auditService *audit.Service) *AuditHandler {
	return &AuditHandler{auditService: auditService}
}

func (h *AuditHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.GET("/tenants/:tenantId/audit-events", h.List)
	router.GET("/tenants/:tenantId/audit-events/export", h.Export)
}

func (h *AuditHandler) List(c *gin.Context) {
	tenantID := c.Param("tenantId")
	filters := repository.AuditFilters{
		DeviceID:        c.Query("device_id"),
		SessionID:       c.Query("session_id"),
		EventType:       c.Query("event_type"),
		StartAt:         c.Query("start_at"),
		EndAt:           c.Query("end_at"),
		IncludeArchived: c.Query("include_archived") == "true",
	}

	// 处理分页参数
	page := 1
	pageSize := 10

	if p := c.Query("page"); p != "" {
		if parsedPage, err := strconv.Atoi(p); err == nil && parsedPage > 0 {
			page = parsedPage
		}
	}

	if ps := c.Query("page_size"); ps != "" {
		if parsedPageSize, err := strconv.Atoi(ps); err == nil && parsedPageSize > 0 && parsedPageSize <= 100 {
			pageSize = parsedPageSize
		}
	}

	events, total, err := h.auditService.ListEvents(c.Request.Context(), tenantID, filters, page, pageSize)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{
		"items":     events,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// Export returns audit events as NDJSON with PII redacted.
func (h *AuditHandler) Export(c *gin.Context) {
	tenantID := c.Param("tenantId")
	filters := repository.AuditFilters{
		DeviceID:        c.Query("device_id"),
		SessionID:       c.Query("session_id"),
		EventType:       c.Query("event_type"),
		StartAt:         c.Query("start_at"),
		EndAt:           c.Query("end_at"),
		IncludeArchived: c.Query("include_archived") == "true",
	}

	events, _, err := h.auditService.ListEvents(c.Request.Context(), tenantID, filters, 1, 1000)
	if err != nil {
		mw.RespondError(c, err)
		return
	}

	redact := c.Query("redact_pii") != "false"

	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Content-Disposition", "attachment; filename=audit-export-"+tenantID+".ndjson")

	encoder := json.NewEncoder(c.Writer)
	for _, evt := range events {
		exported := evt
		if redact {
			exported = redactAuditEvent(exported)
		}
		_ = encoder.Encode(exported)
	}
}

var (
	emailPattern = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	ipPattern    = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	tokenPattern = regexp.MustCompile(`(eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})`)
)

func redactAuditEvent(evt *dto.AuditEventResponse) *dto.AuditEventResponse {
	cp := *evt
	if cp.EventPayloadJSON != "" {
		cp.EventPayloadJSON = redactString(cp.EventPayloadJSON)
	}
	return &cp
}

func redactString(s string) string {
	s = emailPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := strings.SplitN(match, "@", 2)
		if len(parts) != 2 {
			return "***@***"
		}
		local := parts[0]
		if len(local) > 2 {
			local = string(local[0]) + "***" + string(local[len(local)-1])
		} else {
			local = "***"
		}
		return local + "@" + parts[1]
	})
	s = tokenPattern.ReplaceAllString(s, "***JWT_REDACTED***")
	s = ipPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := strings.SplitN(match, ".", 4)
		if len(parts) == 4 {
			return parts[0] + "." + parts[1] + ".xxx.xxx"
		}
		return "xxx.xxx.xxx.xxx"
	})
	return s
}
