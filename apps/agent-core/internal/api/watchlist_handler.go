package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/governance/watchlist"
)

// RegisterWatchlistRoutes adds watchlist API endpoints to the router group.
func (s *LocalServer) RegisterWatchlistRoutes(api *gin.RouterGroup) {
	wl := api.Group("/watchlist")
	{
		wl.POST("/create", s.handleWatchlistCreate)
		wl.POST("/confirm", s.handleWatchlistConfirm)
		wl.GET("", s.handleWatchlistList)
		wl.GET("/:id", s.handleWatchlistGet)
		wl.PUT("/:id", s.handleWatchlistUpdate)
		wl.DELETE("/:id", s.handleWatchlistDelete)
		wl.GET("/alerts", s.handleWatchlistAlerts)
	}
	api.GET("/health/score", s.handleHealthScore)
}

func (s *LocalServer) getWatchlistManager() *watchlist.Manager {
	if s.governanceEngine == nil {
		return nil
	}
	return s.governanceEngine.GetWatchlistManager()
}

type WatchlistCreateRequest struct {
	Input string `json:"input" binding:"required"`
}

func (s *LocalServer) handleWatchlistCreate(c *gin.Context) {
	wm := s.getWatchlistManager()
	if wm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "watchlist not available"})
		return
	}

	var req WatchlistCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	result, err := wm.Decompose(ctx, req.Input)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"items":     result.Items,
		"suggested": result.Suggested,
	})
}

type WatchlistConfirmRequest struct {
	Items []watchlist.WatchItem `json:"items" binding:"required"`
}

func (s *LocalServer) handleWatchlistConfirm(c *gin.Context) {
	wm := s.getWatchlistManager()
	if wm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "watchlist not available"})
		return
	}

	var req WatchlistConfirmRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	if err := wm.ConfirmItems(ctx, req.Items); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "confirmed", "count": len(req.Items)})
}

func (s *LocalServer) handleWatchlistList(c *gin.Context) {
	wm := s.getWatchlistManager()
	if wm == nil {
		c.JSON(http.StatusOK, gin.H{"items": []interface{}{}})
		return
	}

	source := c.Query("source")
	items, err := wm.ListItems(source)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if items == nil {
		items = []*watchlist.WatchItem{}
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (s *LocalServer) handleWatchlistGet(c *gin.Context) {
	wm := s.getWatchlistManager()
	if wm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "watchlist not available"})
		return
	}

	id := c.Param("id")
	item, err := wm.GetItem(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "watch item not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"item": item})
}

type WatchlistUpdateRequest struct {
	Name        *string        `json:"name,omitempty"`
	Description *string        `json:"description,omitempty"`
	Enabled     *bool          `json:"enabled,omitempty"`
	Interval    *time.Duration `json:"interval,omitempty"`
}

func (s *LocalServer) handleWatchlistUpdate(c *gin.Context) {
	wm := s.getWatchlistManager()
	if wm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "watchlist not available"})
		return
	}

	id := c.Param("id")
	item, err := wm.GetItem(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if item == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "watch item not found"})
		return
	}

	var req WatchlistUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Name != nil {
		item.Name = *req.Name
	}
	if req.Description != nil {
		item.Description = *req.Description
	}
	if req.Interval != nil {
		item.Interval = *req.Interval
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}

	ctx := c.Request.Context()
	if err := wm.UpdateItem(ctx, item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated", "item": item})
}

func (s *LocalServer) handleWatchlistDelete(c *gin.Context) {
	wm := s.getWatchlistManager()
	if wm == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "watchlist not available"})
		return
	}

	id := c.Param("id")
	if err := wm.DeleteItem(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

func (s *LocalServer) handleWatchlistAlerts(c *gin.Context) {
	wm := s.getWatchlistManager()
	if wm == nil {
		c.JSON(http.StatusOK, gin.H{"alerts": []interface{}{}})
		return
	}

	var resolved *bool
	if r := c.Query("resolved"); r != "" {
		b := r == "true"
		resolved = &b
	}

	limit := 100
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	alerts, err := wm.ListAlerts(resolved, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if alerts == nil {
		alerts = []*watchlist.WatchAlert{}
	}
	c.JSON(http.StatusOK, gin.H{"alerts": alerts})
}

func (s *LocalServer) handleHealthScore(c *gin.Context) {
	if s.governanceEngine == nil {
		c.JSON(http.StatusOK, gin.H{"score": 100, "total_items": 0, "healthy": 0, "warning": 0, "critical": 0})
		return
	}

	score, err := s.governanceEngine.GetHealthScore()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, score)
}
