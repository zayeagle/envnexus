package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/zy-eagle/envnexus/services/platform-api/internal/service/api_token"
)

const (
	ContextApiTokenID     = "api_token_id"
	ContextApiTokenScopes = "api_token_scopes"

	ScopeReadOnly = "read-only"
)

// applyApiToken sets principal context and enforces scope constraints.
func applyApiToken(c *gin.Context, pr *api_token.ApiTokenPrincipal) {
	c.Set(ContextApiTokenID, pr.TokenID)
	c.Set(ContextApiTokenScopes, pr.Scopes)
	c.Set("user_id", pr.UserID)
	c.Set("tenant_id", pr.TenantID)

	if hasScope(pr.Scopes, ScopeReadOnly) && !isReadOnlyMethod(c.Request.Method) {
		RespondErrorCode(c, http.StatusForbidden, "scope_violation",
			"this API token has read-only scope and cannot perform write operations")
		c.Abort()
		return
	}

	c.Next()
}

func hasScope(scopes []string, target string) bool {
	for _, s := range scopes {
		if s == target {
			return true
		}
	}
	return false
}

func isReadOnlyMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// ApiTokenAuth validates a Bearer token that starts with "enx_" against the
// api_tokens table. On success it populates user_id, tenant_id, and
// api_token_id in the Gin context so downstream handlers are agnostic of
// the authentication method.
func ApiTokenAuth(svc *api_token.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.Next()
			return
		}
		raw := parts[1]
		if !strings.HasPrefix(raw, "enx_") {
			c.Next()
			return
		}

		pr, err := svc.Validate(c.Request.Context(), raw)
		if err != nil {
			RespondError(c, err)
			c.Abort()
			return
		}
		applyApiToken(c, pr)
	}
}

// JWTOrApiTokenAuth tries API-token first (if "enx_" prefix), otherwise falls
// through to JWT validation. This allows a single route group to accept both.
func JWTOrApiTokenAuth(jwtSecret string, apiTokenSvc *api_token.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "unauthorized", "message": "missing authorization header"},
			})
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": gin.H{"code": "unauthorized", "message": "invalid authorization header format"},
			})
			return
		}
		raw := parts[1]

		if strings.HasPrefix(raw, "enx_") && apiTokenSvc != nil {
			pr, err := apiTokenSvc.Validate(c.Request.Context(), raw)
			if err != nil {
				RespondError(c, err)
				c.Abort()
				return
			}
			applyApiToken(c, pr)
			return
		}

		// Fall through to standard JWT validation.
		jwtMiddleware := JWTAuth(jwtSecret)
		jwtMiddleware(c)
	}
}
