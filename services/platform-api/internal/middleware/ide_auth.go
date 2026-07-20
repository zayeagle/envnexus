package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/service/device_auth"
)

// Context keys set by IdeAuth.
const (
	ContextIdeClientTokenID = "ide_client_token_id"
	ContextIdeUserID        = "ide_user_id"
	ContextIdeTenantID      = "ide_tenant_id"
)

// IdeAuth validates the raw IDE access token (stored as SHA-256) against ide_client_tokens.
func IdeAuth(svc *device_auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			RespondErrorCode(c, http.StatusUnauthorized, "unauthorized", "missing authorization header")
			c.Abort()
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			RespondErrorCode(c, http.StatusUnauthorized, "unauthorized", "invalid authorization header format")
			c.Abort()
			return
		}
		pr, err := svc.ValidateIdeAccessToken(c.Request.Context(), parts[1])
		if err != nil {
			RespondError(c, err)
			c.Abort()
			return
		}
		c.Set(ContextIdeClientTokenID, pr.TokenID)
		c.Set(ContextIdeUserID, pr.UserID)
		c.Set(ContextIdeTenantID, pr.TenantID)
		// Let handlers reuse generic tenant scoping if needed.
		c.Set("tenant_id", pr.TenantID)
		c.Set("user_id", pr.UserID)
		c.Next()
	}
}
