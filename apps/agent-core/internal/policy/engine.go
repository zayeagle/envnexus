package policy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/zayeagle/envnexus/apps/agent-core/internal/tools"
)

type ApprovalStatus string

const (
	StatusDrafted     ApprovalStatus = "drafted"
	StatusPending     ApprovalStatus = "pending_user"
	StatusApproved    ApprovalStatus = "approved"
	StatusDenied      ApprovalStatus = "denied"
	StatusExpired     ApprovalStatus = "expired"
	StatusExecuting   ApprovalStatus = "executing"
	StatusSucceeded   ApprovalStatus = "succeeded"
	StatusFailed      ApprovalStatus = "failed"
	StatusRolledBack  ApprovalStatus = "rolled_back"
)

type ApprovalRequest struct {
	ID              string                 `json:"id"`
	PlatformID      string                 `json:"platform_id,omitempty"`
	SessionID       string                 `json:"session_id,omitempty"`
	ToolName        string                 `json:"tool_name"`
	RiskLevel       string                 `json:"risk_level"`
	Params          map[string]interface{} `json:"params"`
	Status          ApprovalStatus         `json:"status"`
	CreatedAt       time.Time              `json:"created_at"`
	ExpiresAt       time.Time              `json:"expires_at"`
	ResultCh        chan bool              `json:"-"`
}

type PlatformClient struct {
	baseURL     string
	deviceToken string
	httpClient  *http.Client
}

func NewPlatformClient(baseURL, deviceToken string) *PlatformClient {
	return &PlatformClient{
		baseURL:     baseURL,
		deviceToken: deviceToken,
		httpClient:  &http.Client{Timeout: 15 * time.Second},
	}
}

type Engine struct {
	mu               sync.RWMutex
	pendingApprovals map[string]*ApprovalRequest
	platformClient   *PlatformClient
}

func NewEngine() *Engine {
	return &Engine{
		pendingApprovals: make(map[string]*ApprovalRequest),
	}
}

func (e *Engine) SetPlatformClient(pc *PlatformClient) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.platformClient = pc
}

func (e *Engine) Check(ctx context.Context, tool tools.Tool, params map[string]interface{}) (bool, error) {
	if tool.IsReadOnly() {
		return true, nil
	}

	reqID := generateID()
	req := &ApprovalRequest{
		ID:        reqID,
		ToolName:  tool.Name(),
		RiskLevel: tool.RiskLevel(),
		Params:    params,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		ResultCh:  make(chan bool, 1),
	}

	e.mu.Lock()
	e.pendingApprovals[reqID] = req
	e.mu.Unlock()

	slog.Info("[policy] Tool requires approval", "tool", tool.Name(), "risk", tool.RiskLevel(), "request_id", reqID)

	if e.platformClient != nil && req.SessionID != "" {
		go e.syncApprovalToPlatform(req)
	}

	select {
	case <-ctx.Done():
		e.removeRequest(reqID)
		return false, ctx.Err()
	case <-time.After(time.Until(req.ExpiresAt)):
		e.removeRequest(reqID)
		return false, fmt.Errorf("approval request expired")
	case approved := <-req.ResultCh:
		e.removeRequest(reqID)
		if !approved {
			return false, fmt.Errorf("execution denied by user")
		}
		return true, nil
	}
}

func (e *Engine) CheckWithSession(ctx context.Context, tool tools.Tool, params map[string]interface{}, sessionID string) (bool, error) {
	if tool.IsReadOnly() {
		return true, nil
	}

	reqID := generateID()
	req := &ApprovalRequest{
		ID:        reqID,
		SessionID: sessionID,
		ToolName:  tool.Name(),
		RiskLevel: tool.RiskLevel(),
		Params:    params,
		Status:    StatusPending,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
		ResultCh:  make(chan bool, 1),
	}

	e.mu.Lock()
	e.pendingApprovals[reqID] = req
	e.mu.Unlock()

	slog.Info("[policy] Tool requires approval", "tool", tool.Name(), "risk", tool.RiskLevel(), "session_id", sessionID, "request_id", reqID)

	if e.platformClient != nil {
		go e.syncApprovalToPlatform(req)
		go e.pollApprovalFromPlatform(ctx, req)
	}

	select {
	case <-ctx.Done():
		e.removeRequest(reqID)
		return false, ctx.Err()
	case <-time.After(time.Until(req.ExpiresAt)):
		e.removeRequest(reqID)
		return false, fmt.Errorf("approval request expired")
	case approved := <-req.ResultCh:
		if approved {
			e.notifyPlatformExecuting(req)
		}
		e.removeRequest(reqID)
		if !approved {
			return false, fmt.Errorf("execution denied by user")
		}
		return true, nil
	}
}

func (e *Engine) ReportExecutionResult(reqID string, succeeded bool) {
	e.mu.RLock()
	req, exists := e.pendingApprovals[reqID]
	pc := e.platformClient
	e.mu.RUnlock()

	if !exists || pc == nil || req.PlatformID == "" {
		return
	}

	endpoint := "/agent/v1/approvals/" + req.PlatformID + "/succeeded"
	if !succeeded {
		endpoint = "/agent/v1/approvals/" + req.PlatformID + "/failed"
	}

	_ = pc.post(endpoint, nil)
}

func (e *Engine) syncApprovalToPlatform(req *ApprovalRequest) {
	e.mu.RLock()
	pc := e.platformClient
	e.mu.RUnlock()

	if pc == nil {
		return
	}

	body := map[string]interface{}{
		"session_id": req.SessionID,
		"tool_name":  req.ToolName,
		"risk_level": req.RiskLevel,
		"params":     req.Params,
	}

	resp, err := pc.postJSON("/agent/v1/approvals", body)
	if err != nil {
		slog.Error("[policy] Failed to sync approval to platform", "error", err)
		return
	}

	if data, ok := resp["data"].(map[string]interface{}); ok {
		if approvalID, ok := data["approval_id"].(string); ok {
			e.mu.Lock()
			req.PlatformID = approvalID
			e.mu.Unlock()
			slog.Info("[policy] Approval synced to platform", "request_id", req.ID, "approval_id", approvalID)
		}
	}
}

func (e *Engine) pollApprovalFromPlatform(ctx context.Context, req *ApprovalRequest) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(req.ExpiresAt)):
			return
		case <-ticker.C:
			e.mu.RLock()
			platformID := req.PlatformID
			pc := e.platformClient
			e.mu.RUnlock()

			if platformID == "" || pc == nil {
				continue
			}

			resp, err := pc.get("/agent/v1/approvals/" + platformID)
			if err != nil {
				continue
			}

			data, ok := resp["data"].(map[string]interface{})
			if !ok {
				continue
			}

			status, _ := data["status"].(string)
			switch ApprovalStatus(status) {
			case StatusApproved:
				slog.Info("[policy] Platform approved", "platform_id", platformID)
				select {
				case req.ResultCh <- true:
				default:
				}
				return
			case StatusDenied:
				slog.Warn("[policy] Platform denied", "platform_id", platformID)
				select {
				case req.ResultCh <- false:
				default:
				}
				return
			case StatusExpired:
				slog.Warn("[policy] Platform expired", "platform_id", platformID)
				select {
				case req.ResultCh <- false:
				default:
				}
				return
			}
		}
	}
}

func (e *Engine) notifyPlatformExecuting(req *ApprovalRequest) {
	e.mu.RLock()
	pc := e.platformClient
	e.mu.RUnlock()

	if pc == nil || req.PlatformID == "" {
		return
	}

	_ = pc.post("/agent/v1/approvals/"+req.PlatformID+"/executing", nil)
}

func (e *Engine) removeRequest(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pendingApprovals, id)
}

func (e *Engine) Resolve(id string, approved bool) error {
	e.mu.Lock()
	req, exists := e.pendingApprovals[id]
	e.mu.Unlock()

	if !exists {
		return fmt.Errorf("approval request not found or already resolved")
	}

	if approved {
		req.Status = StatusApproved
	} else {
		req.Status = StatusDenied
	}

	req.ResultCh <- approved
	return nil
}

func (e *Engine) GetPending() []*ApprovalRequest {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var list []*ApprovalRequest
	for _, req := range e.pendingApprovals {
		list = append(list, req)
	}
	return list
}

// PlatformClient HTTP helpers

func (pc *PlatformClient) postJSON(path string, body interface{}) (map[string]interface{}, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", pc.baseURL+path, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if pc.deviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+pc.deviceToken)
	}

	resp, err := pc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result, nil
}

func (pc *PlatformClient) get(path string) (map[string]interface{}, error) {
	req, err := http.NewRequest("GET", pc.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if pc.deviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+pc.deviceToken)
	}

	resp, err := pc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result, nil
}

func (pc *PlatformClient) post(path string, body interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, _ := json.Marshal(body)
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest("POST", pc.baseURL+path, bodyReader)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if pc.deviceToken != "" {
		req.Header.Set("Authorization", "Bearer "+pc.deviceToken)
	}

	resp, err := pc.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func generateID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
