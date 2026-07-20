package api

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/zayeagle/envnexus/apps/agent-core/internal/remediation"
)

// PlanStore holds active remediation plans for the local API.
type PlanStore struct {
	mu    sync.RWMutex
	plans map[string]*remediation.RemediationPlan
}

func NewPlanStore() *PlanStore {
	return &PlanStore{
		plans: make(map[string]*remediation.RemediationPlan),
	}
}

func (ps *PlanStore) Store(plan *remediation.RemediationPlan) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.plans[plan.PlanID] = plan
}

func (ps *PlanStore) Get(planID string) (*remediation.RemediationPlan, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	p, ok := ps.plans[planID]
	return p, ok
}

func (ps *PlanStore) Delete(planID string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.plans, planID)
}

// RegisterPlanRoutes adds remediation plan API endpoints to the router group.
// This is called from server.go Start() — appended to existing routes.
func (s *LocalServer) RegisterPlanRoutes(api *gin.RouterGroup) {
	api.GET("/plan/:id", s.handleGetPlan)
	api.POST("/plan/approve", s.handleApprovePlan)
	api.POST("/plan/reject", s.handleRejectPlan)
	api.POST("/plan/step/confirm", s.handleStepConfirm)
	api.POST("/plan/step/approve", s.handleStepApprove)
}

func (s *LocalServer) handleGetPlan(c *gin.Context) {
	planID := c.Param("id")
	if s.planStore == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "plan store not initialized"})
		return
	}

	plan, ok := s.planStore.Get(planID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "plan not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"plan": plan})
}

type PlanActionRequest struct {
	PlanID string `json:"plan_id" binding:"required"`
}

func (s *LocalServer) handleApprovePlan(c *gin.Context) {
	var req PlanActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.policyEngine.Resolve(req.PlanID, true); err != nil {
		val, ok := s.planApprovals.Load(req.PlanID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "plan approval not found"})
			return
		}
		ch := val.(chan bool)
		select {
		case ch <- true:
			c.JSON(http.StatusOK, gin.H{"status": "approved"})
		default:
			c.JSON(http.StatusConflict, gin.H{"error": "already resolved"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "approved"})
}

func (s *LocalServer) handleRejectPlan(c *gin.Context) {
	var req PlanActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := s.policyEngine.Resolve(req.PlanID, false); err != nil {
		val, ok := s.planApprovals.Load(req.PlanID)
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": "plan approval not found"})
			return
		}
		ch := val.(chan bool)
		select {
		case ch <- false:
			c.JSON(http.StatusOK, gin.H{"status": "rejected"})
		default:
			c.JSON(http.StatusConflict, gin.H{"error": "already resolved"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "rejected"})
}

type StepActionRequest struct {
	PlanID  string `json:"plan_id" binding:"required"`
	StepID  int    `json:"step_id" binding:"required"`
	Approved bool  `json:"approved"`
}

func (s *LocalServer) handleStepConfirm(c *gin.Context) {
	var req StepActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	key := stepApprovalKey(req.PlanID, req.StepID)
	val, ok := s.stepApprovals.Load(key)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "step confirmation not found"})
		return
	}

	ch := val.(chan bool)
	select {
	case ch <- req.Approved:
		c.JSON(http.StatusOK, gin.H{"status": "resolved"})
	default:
		c.JSON(http.StatusConflict, gin.H{"error": "already resolved"})
	}
}

func (s *LocalServer) handleStepApprove(c *gin.Context) {
	var req StepActionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	key := stepApprovalKey(req.PlanID, req.StepID)
	val, ok := s.stepApprovals.Load(key)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "step approval not found"})
		return
	}

	ch := val.(chan bool)
	select {
	case ch <- req.Approved:
		c.JSON(http.StatusOK, gin.H{"status": "resolved"})
	default:
		c.JSON(http.StatusConflict, gin.H{"error": "already resolved"})
	}
}

func stepApprovalKey(planID string, stepID int) string {
	return fmt.Sprintf("%s:%d", planID, stepID)
}
