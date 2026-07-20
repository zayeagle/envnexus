package http

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/infrastructure"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type IMProviderHandler struct {
	providerRepo repository.IMProviderRepository
	channelRepo  repository.UserNotificationChannelRepository
	crypto       *infrastructure.CryptoService
}

func NewIMProviderHandler(
	providerRepo repository.IMProviderRepository,
	channelRepo repository.UserNotificationChannelRepository,
	crypto *infrastructure.CryptoService,
) *IMProviderHandler {
	return &IMProviderHandler{
		providerRepo: providerRepo,
		channelRepo:  channelRepo,
		crypto:       crypto,
	}
}

func (h *IMProviderHandler) RegisterRoutes(router *gin.RouterGroup) {
	providers := router.Group("/tenants/:tenantId/im-providers")
	{
		providers.POST("", h.CreateProvider)
		providers.GET("", h.ListProviders)
		providers.PUT("/:providerId", h.UpdateProvider)
		providers.DELETE("/:providerId", h.DeleteProvider)
	}
	channels := router.Group("/me/notification-channels")
	{
		channels.GET("", h.ListMyChannels)
		channels.POST("", h.CreateChannel)
		channels.DELETE("/:channelId", h.DeleteChannel)
	}
}

func (h *IMProviderHandler) CreateProvider(c *gin.Context) {
	tenantID := c.Param("tenantId")
	var req dto.CreateIMProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	configMap := map[string]string{
		"app_id":     req.AppID,
		"app_secret": req.AppSecret,
	}
	configJSON, _ := json.Marshal(configMap)
	encrypted := string(configJSON)
	if h.crypto != nil {
		var err error
		encrypted, err = h.crypto.Encrypt(string(configJSON))
		if err != nil {
			mw.RespondErrorCode(c, http.StatusInternalServerError, "encryption_failed", "failed to encrypt credentials")
			return
		}
	}
	provider := &domain.IMProvider{
		ID:         ulid.Make().String(),
		TenantID:   tenantID,
		Provider:   domain.IMProviderType(req.Provider),
		Name:       req.Name,
		ConfigJSON: encrypted,
		WebhookURL: req.WebhookURL,
		Status:     "active",
	}
	if err := h.providerRepo.Create(c.Request.Context(), provider); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, dto.IMProviderResponse{
		ID:         provider.ID,
		TenantID:   provider.TenantID,
		Provider:   string(provider.Provider),
		Name:       provider.Name,
		AppID:      req.AppID,
		WebhookURL: provider.WebhookURL,
		Status:     provider.Status,
		CreatedAt:  provider.CreatedAt,
		UpdatedAt:  provider.UpdatedAt,
	})
}

func (h *IMProviderHandler) ListProviders(c *gin.Context) {
	tenantID := c.Param("tenantId")
	providers, err := h.providerRepo.ListByTenant(c.Request.Context(), tenantID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	result := make([]dto.IMProviderResponse, 0, len(providers))
	for _, p := range providers {
		appID := h.extractAppID(p)
		result = append(result, dto.IMProviderResponse{
			ID:         p.ID,
			TenantID:   p.TenantID,
			Provider:   string(p.Provider),
			Name:       p.Name,
			AppID:      appID,
			WebhookURL: p.WebhookURL,
			Status:     p.Status,
			CreatedAt:  p.CreatedAt,
			UpdatedAt:  p.UpdatedAt,
		})
	}
	mw.RespondSuccess(c, http.StatusOK, result)
}

func (h *IMProviderHandler) UpdateProvider(c *gin.Context) {
	providerID := c.Param("providerId")
	var req dto.UpdateIMProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	provider, err := h.providerRepo.GetByID(c.Request.Context(), providerID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	if provider == nil {
		mw.RespondError(c, domain.ErrIMProviderNotFound)
		return
	}
	if req.Name != nil {
		provider.Name = *req.Name
	}
	if req.WebhookURL != nil {
		provider.WebhookURL = *req.WebhookURL
	}
	if req.Status != nil {
		provider.Status = *req.Status
	}
	if req.AppSecret != nil && *req.AppSecret != "" {
		configMap := map[string]string{"app_id": "", "app_secret": *req.AppSecret}
		if req.AppID != nil {
			configMap["app_id"] = *req.AppID
		}
		configJSON, _ := json.Marshal(configMap)
		encrypted := string(configJSON)
		if h.crypto != nil {
			encrypted, _ = h.crypto.Encrypt(string(configJSON))
		}
		provider.ConfigJSON = encrypted
	}
	if err := h.providerRepo.Update(c.Request.Context(), provider); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, dto.IMProviderResponse{
		ID:         provider.ID,
		TenantID:   provider.TenantID,
		Provider:   string(provider.Provider),
		Name:       provider.Name,
		AppID:      h.extractAppID(provider),
		WebhookURL: provider.WebhookURL,
		Status:     provider.Status,
		CreatedAt:  provider.CreatedAt,
		UpdatedAt:  provider.UpdatedAt,
	})
}

func (h *IMProviderHandler) DeleteProvider(c *gin.Context) {
	providerID := c.Param("providerId")
	if err := h.providerRepo.Delete(c.Request.Context(), providerID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "deleted"})
}

func (h *IMProviderHandler) extractAppID(provider *domain.IMProvider) string {
	plaintext := provider.ConfigJSON
	if h.crypto != nil {
		decrypted, err := h.crypto.Decrypt(provider.ConfigJSON)
		if err == nil {
			plaintext = decrypted
		}
	}
	var config map[string]string
	if json.Unmarshal([]byte(plaintext), &config) == nil {
		appID := config["app_id"]
		if len(appID) > 8 {
			return appID[:8] + "****"
		}
		return appID
	}
	return ""
}

func (h *IMProviderHandler) ListMyChannels(c *gin.Context) {
	userID := c.GetString("user_id")
	channels, err := h.channelRepo.ListByUserID(c.Request.Context(), userID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	result := make([]dto.NotificationChannelResponse, 0, len(channels))
	for _, ch := range channels {
		result = append(result, channelToResponse(ch))
	}
	mw.RespondSuccess(c, http.StatusOK, result)
}

func (h *IMProviderHandler) CreateChannel(c *gin.Context) {
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")
	var req dto.CreateNotificationChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	provider, err := h.providerRepo.GetByID(c.Request.Context(), req.ProviderID)
	if err != nil || provider == nil {
		mw.RespondError(c, domain.ErrIMProviderNotFound)
		return
	}
	channel := &domain.UserNotificationChannel{
		ID:           ulid.Make().String(),
		UserID:       userID,
		TenantID:     tenantID,
		ProviderID:   req.ProviderID,
		Provider:     string(provider.Provider),
		ExternalID:   req.ExternalID,
		ExternalName: req.ExternalName,
		ChatID:       req.ChatID,
		Status:       "active",
		Verified:     false,
	}
	if err := h.channelRepo.Create(c.Request.Context(), channel); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, channelToResponse(channel))
}

func (h *IMProviderHandler) DeleteChannel(c *gin.Context) {
	channelID := c.Param("channelId")
	userID := c.GetString("user_id")
	channel, err := h.channelRepo.GetByID(c.Request.Context(), channelID)
	if err != nil || channel == nil {
		mw.RespondError(c, domain.ErrNotificationChannelNotFound)
		return
	}
	if channel.UserID != userID {
		mw.RespondError(c, domain.ErrInsufficientPermission)
		return
	}
	if err := h.channelRepo.Delete(c.Request.Context(), channelID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "deleted"})
}

func channelToResponse(ch *domain.UserNotificationChannel) dto.NotificationChannelResponse {
	return dto.NotificationChannelResponse{
		ID:           ch.ID,
		UserID:       ch.UserID,
		TenantID:     ch.TenantID,
		ProviderID:   ch.ProviderID,
		Provider:     ch.Provider,
		ExternalID:   ch.ExternalID,
		ExternalName: ch.ExternalName,
		ChatID:       ch.ChatID,
		Priority:     ch.Priority,
		Verified:     ch.Verified,
		Status:       ch.Status,
		CreatedAt:    ch.CreatedAt,
		UpdatedAt:    ch.UpdatedAt,
	}
}
