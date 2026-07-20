package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/marketplace"
)

type MarketplaceHandler struct {
	svc *marketplace.Service
}

func NewMarketplaceHandler(svc *marketplace.Service) *MarketplaceHandler {
	return &MarketplaceHandler{svc: svc}
}

func (h *MarketplaceHandler) RegisterPublicRoutes(router *gin.RouterGroup) {
	router.GET("/ide-sync/extension/latest", h.GetLatestIDEExtension)
}

func (h *MarketplaceHandler) RegisterRoutes(router *gin.RouterGroup) {
	m := router.Group("/tenants/:tenantId/marketplace")
	{
		m.GET("/items", h.ListItems)
		m.GET("/items/:itemId/download", h.GetItemDownload)
		m.POST("/items", h.CreateItem)
		m.PUT("/items/:itemId", h.UpdateItem)
		m.DELETE("/items/:itemId", h.DeleteItem)
		m.GET("/subscriptions", h.ListSubscriptions)
		m.POST("/subscriptions", h.Subscribe)
		m.DELETE("/subscriptions/:itemId", h.Unsubscribe)
	}
}

func (h *MarketplaceHandler) GetLatestIDEExtension(c *gin.Context) {
	out, err := h.svc.GetLatestIDEExtensionInfo(c.Request.Context())
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, out)
}

func (h *MarketplaceHandler) requirePlatformSuperAdmin(c *gin.Context) bool {
	v, ok := c.Get("platform_super_admin")
	if !ok {
		mw.RespondErrorCode(c, http.StatusForbidden, "forbidden", "platform administrator access required")
		return false
	}
	b, ok := v.(bool)
	if !ok || !b {
		mw.RespondErrorCode(c, http.StatusForbidden, "forbidden", "platform administrator access required")
		return false
	}
	return true
}

func (h *MarketplaceHandler) requireTenantScope(c *gin.Context, tenantID string) bool {
	jwtTenant, ok := c.Get("tenant_id")
	var super bool
	if v, ok2 := c.Get("platform_super_admin"); ok2 {
		if b, ok3 := v.(bool); ok3 {
			super = b
		}
	}
	if !ok {
		mw.RespondErrorCode(c, http.StatusUnauthorized, "unauthorized", "missing tenant context")
		return false
	}
	jt, ok := jwtTenant.(string)
	if !ok {
		mw.RespondErrorCode(c, http.StatusUnauthorized, "unauthorized", "invalid tenant context")
		return false
	}
	if jt != tenantID && !super {
		mw.RespondErrorCode(c, http.StatusForbidden, "forbidden", "tenant scope mismatch")
		return false
	}
	return true
}

func (h *MarketplaceHandler) ListItems(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if !h.requireTenantScope(c, tenantID) {
		return
	}
	var itemType *domain.MarketplaceItemType
	if t := c.Query("type"); t != "" {
		v := domain.MarketplaceItemType(t)
		itemType = &v
	}
	var status *domain.MarketplaceItemStatus
	if s := c.Query("status"); s != "" {
		v := domain.MarketplaceItemStatus(s)
		status = &v
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	items, total, err := h.svc.ListItems(c.Request.Context(), itemType, status, page, pageSize)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": items, "total": total, "page": page, "page_size": pageSize})
}

func (h *MarketplaceHandler) ListSubscriptions(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if !h.requireTenantScope(c, tenantID) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	subs, total, err := h.svc.ListSubscriptions(c.Request.Context(), tenantID, page, pageSize)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"items": subs, "total": total, "page": page, "page_size": pageSize})
}

func (h *MarketplaceHandler) Subscribe(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if !h.requireTenantScope(c, tenantID) {
		return
	}
	var req dto.MarketplaceSubscribeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	out, err := h.svc.Subscribe(c.Request.Context(), tenantID, req.ItemID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, out)
}

func (h *MarketplaceHandler) Unsubscribe(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if !h.requireTenantScope(c, tenantID) {
		return
	}
	itemID := c.Param("itemId")
	if itemID == "" {
		mw.RespondValidationError(c, "itemId is required")
		return
	}
	if err := h.svc.Unsubscribe(c.Request.Context(), tenantID, itemID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *MarketplaceHandler) GetItemDownload(c *gin.Context) {
	tenantID := c.Param("tenantId")
	if !h.requireTenantScope(c, tenantID) {
		return
	}
	itemID := c.Param("itemId")
	if itemID == "" {
		mw.RespondValidationError(c, "itemId is required")
		return
	}
	out, err := h.svc.GetItemDownloadURL(c.Request.Context(), tenantID, itemID)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, out)
}

const maxMarketplaceMultipartMemory = 32 << 20

func parseMarketplaceForm(c *gin.Context) error {
	if err := c.Request.ParseMultipartForm(maxMarketplaceMultipartMemory); err != nil {
		if err2 := c.Request.ParseForm(); err2 != nil {
			return err
		}
	}
	return nil
}

// CreateItem creates a marketplace item (platform super admin). multipart/form-data: type, name, version, optional description, author, status, optional file, optional payload (JSON for non-plugin).
func (h *MarketplaceHandler) CreateItem(c *gin.Context) {
	if !h.requirePlatformSuperAdmin(c) {
		return
	}
	if err := parseMarketplaceForm(c); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	typ := domain.MarketplaceItemType(strings.TrimSpace(c.PostForm("type")))
	in := marketplace.CreateMarketplaceItemInput{
		Type:        typ,
		Name:        strings.TrimSpace(c.PostForm("name")),
		Description: strings.TrimSpace(c.PostForm("description")),
		Version:     strings.TrimSpace(c.PostForm("version")),
		Author:      strings.TrimSpace(c.PostForm("author")),
		PayloadJSON: c.PostForm("payload"),
	}
	if s := strings.TrimSpace(c.PostForm("status")); s != "" {
		in.Status = domain.MarketplaceItemStatus(s)
	}
	if fh, err := c.FormFile("file"); err == nil && fh != nil {
		f, err := fh.Open()
		if err != nil {
			mw.RespondErrorCode(c, http.StatusBadRequest, "invalid_file", "could not read uploaded file")
			return
		}
		defer f.Close()
		in.File = f
		in.FileSize = fh.Size
		in.Filename = fh.Filename
		in.ContentType = fh.Header.Get("Content-Type")
	}
	out, err := h.svc.CreateMarketplaceItem(c.Request.Context(), in)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusCreated, out)
}

// UpdateItem updates a marketplace item (platform super admin). multipart/form-data with optional file and fields; empty string keeps existing values.
func (h *MarketplaceHandler) UpdateItem(c *gin.Context) {
	if !h.requirePlatformSuperAdmin(c) {
		return
	}
	itemID := c.Param("itemId")
	if itemID == "" {
		mw.RespondValidationError(c, "itemId is required")
		return
	}
	if err := parseMarketplaceForm(c); err != nil {
		mw.RespondValidationError(c, err.Error())
		return
	}
	in := marketplace.UpdateMarketplaceItemInput{
		Type:        c.PostForm("type"),
		Name:        c.PostForm("name"),
		Description: c.PostForm("description"),
		Version:     c.PostForm("version"),
		Author:      c.PostForm("author"),
		Status:      c.PostForm("status"),
		PayloadJSON: c.PostForm("payload"),
	}
	if fh, err := c.FormFile("file"); err == nil && fh != nil {
		f, err := fh.Open()
		if err != nil {
			mw.RespondErrorCode(c, http.StatusBadRequest, "invalid_file", "could not read uploaded file")
			return
		}
		defer f.Close()
		in.File = f
		in.FileSize = fh.Size
		in.Filename = fh.Filename
		in.ContentType = fh.Header.Get("Content-Type")
	}
	out, err := h.svc.UpdateMarketplaceItem(c.Request.Context(), itemID, in)
	if err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, out)
}

// DeleteItem removes a marketplace item (platform super admin).
func (h *MarketplaceHandler) DeleteItem(c *gin.Context) {
	if !h.requirePlatformSuperAdmin(c) {
		return
	}
	itemID := c.Param("itemId")
	if itemID == "" {
		mw.RespondValidationError(c, "itemId is required")
		return
	}
	if err := h.svc.DeleteMarketplaceItem(c.Request.Context(), itemID); err != nil {
		mw.RespondError(c, err)
		return
	}
	mw.RespondSuccess(c, http.StatusOK, gin.H{"status": "ok"})
}
