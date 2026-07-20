package http

import (
	"net/http"

	"github.com/gin-gonic/gin"

	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/marketplace"
)

// IdeSyncHandler serves IDE clients authenticated with IDE access tokens.
type IdeSyncHandler struct {
	marketplaceSvc *marketplace.Service
}

func NewIdeSyncHandler(marketplaceSvc *marketplace.Service) *IdeSyncHandler {
	return &IdeSyncHandler{marketplaceSvc: marketplaceSvc}
}

// RegisterRoutes must be used on a group that already applies IdeAuth.
func (h *IdeSyncHandler) RegisterRoutes(router *gin.RouterGroup) {
	router.GET("/ide-sync/manifest", h.GetManifest)
}

// GetManifest returns all subscribed marketplace item payloads for the token's tenant.
func (h *IdeSyncHandler) GetManifest(c *gin.Context) {
	tenantID := c.GetString(mw.ContextIdeTenantID)
	if tenantID == "" {
		mw.RespondErrorCode(c, http.StatusUnauthorized, "unauthorized", "missing IDE tenant")
		return
	}
	items, err := h.marketplaceSvc.IdeSyncManifest(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": items})
}
