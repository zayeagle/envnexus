package command

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/zayeagle/envnexus/services/platform-api/internal/domain"
	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/infrastructure"
	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
)

type Service struct {
	taskRepo      repository.CommandTaskRepository
	execRepo      repository.CommandExecutionRepository
	deviceRepo    repository.DeviceRepository
	policyService *ApprovalPolicyService
	auditRepo     repository.AuditRepository
	rbacService   PermissionChecker
	gatewayClient *infrastructure.GatewayClient
	userRepo      repository.UserRepository
}

type PermissionChecker interface {
	HasPermission(ctx context.Context, userID, permission string) (bool, error)
	UserHasRoleInTenant(ctx context.Context, tenantID, userID, roleID string) (bool, error)
	ListRoleIDsForUserInTenant(ctx context.Context, tenantID, userID string) ([]string, error)
}

func NewService(
	taskRepo repository.CommandTaskRepository,
	execRepo repository.CommandExecutionRepository,
	deviceRepo repository.DeviceRepository,
	policyService *ApprovalPolicyService,
	auditRepo repository.AuditRepository,
	rbacService PermissionChecker,
	gatewayClient *infrastructure.GatewayClient,
	userRepo repository.UserRepository,
) *Service {
	return &Service{
		taskRepo:      taskRepo,
		execRepo:      execRepo,
		deviceRepo:    deviceRepo,
		policyService: policyService,
		auditRepo:     auditRepo,
		rbacService:   rbacService,
		gatewayClient: gatewayClient,
		userRepo:      userRepo,
	}
}

func userDisplayName(u *domain.User) string {
	if u == nil {
		return ""
	}
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Email
}

func (s *Service) buildCreatorNameMap(ctx context.Context, tasks []*domain.CommandTask) map[string]string {
	if s.userRepo == nil || len(tasks) == 0 {
		return nil
	}
	ids := make(map[string]struct{})
	for _, t := range tasks {
		if t != nil && t.CreatedByUserID != "" {
			ids[t.CreatedByUserID] = struct{}{}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	out := make(map[string]string, len(ids))
	for id := range ids {
		u, err := s.userRepo.GetByID(ctx, id)
		if err != nil || u == nil {
			continue
		}
		out[id] = userDisplayName(u)
	}
	return out
}

func (s *Service) resolveCreatedByName(ctx context.Context, userID string, creatorNames map[string]string) string {
	if userID == "" {
		return ""
	}
	if creatorNames != nil {
		return creatorNames[userID]
	}
	if s.userRepo == nil {
		return ""
	}
	u, err := s.userRepo.GetByID(ctx, userID)
	if err != nil || u == nil {
		return ""
	}
	return userDisplayName(u)
}

func draftExpiresAt(policy *domain.ApprovalPolicy) time.Time {
	d := time.Now().Add(7 * 24 * time.Hour)
	if policy != nil && policy.ExpiresMinutes > 0 {
		p := time.Now().Add(time.Duration(policy.ExpiresMinutes) * time.Minute)
		if p.After(d) {
			return p
		}
	}
	return d
}

func normOSArch(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// validateCommandDeviceTargets ensures each target exists, belongs to the tenant, is active, and when multiple targets are selected they share the same platform and architecture (shell commands must be portable).
func (s *Service) validateCommandDeviceTargets(ctx context.Context, tenantID string, deviceIDs []string) error {
	if s.deviceRepo == nil || len(deviceIDs) == 0 {
		return nil
	}
	var resolved []*domain.Device
	for _, id := range deviceIDs {
		dev, err := s.deviceRepo.GetByID(ctx, id)
		if err != nil {
			return err
		}
		if dev == nil {
			return domain.NewAppError("command_target_device_invalid", fmt.Sprintf("device %s not found or deleted", id), 400)
		}
		if dev.TenantID != tenantID {
			return domain.NewAppError("command_target_device_invalid", fmt.Sprintf("device %s does not belong to this tenant", id), 400)
		}
		if dev.Status != domain.DeviceStatusActive {
			return domain.NewAppError("command_target_device_invalid", fmt.Sprintf("device %s is not active (status=%s)", id, dev.Status), 400)
		}
		if strings.TrimSpace(dev.Platform) == "" || strings.TrimSpace(dev.Arch) == "" {
			return domain.NewAppError("command_target_device_invalid", fmt.Sprintf("device %s is missing platform or architecture metadata", id), 400)
		}
		resolved = append(resolved, dev)
	}
	if len(resolved) > 1 {
		p0, a0 := normOSArch(resolved[0].Platform), normOSArch(resolved[0].Arch)
		for i := 1; i < len(resolved); i++ {
			p, a := normOSArch(resolved[i].Platform), normOSArch(resolved[i].Arch)
			if p != p0 || a != a0 {
				return domain.NewAppError("command_target_device_invalid",
					fmt.Sprintf("all target devices must share the same OS and architecture (cannot mix %s/%s with %s/%s)",
						resolved[0].Platform, resolved[0].Arch, resolved[i].Platform, resolved[i].Arch), 400)
			}
		}
	}
	return nil
}

func (s *Service) CreateTask(ctx context.Context, tenantID, userID string, req dto.CreateCommandTaskRequest) (*dto.CommandTaskResponse, error) {
	systemRisk := EvaluateRisk(req.CommandType, req.CommandPayload)
	effectiveRisk := EffectiveRisk(req.RiskLevel, systemRisk)

	if err := s.validateCommandDeviceTargets(ctx, tenantID, req.DeviceIDs); err != nil {
		return nil, err
	}

	deviceIDsJSON, _ := json.Marshal(req.DeviceIDs)
	policy, _ := s.policyService.FindPolicy(ctx, tenantID, effectiveRisk)

	task := &domain.CommandTask{
		ID:              ulid.Make().String(),
		TenantID:        tenantID,
		CreatedByUserID: userID,
		Title:           req.Title,
		CommandType:     req.CommandType,
		CommandPayload:  req.CommandPayload,
		DeviceIDsJSON:   string(deviceIDsJSON),
		RiskLevel:       req.RiskLevel,
		EffectiveRisk:   effectiveRisk,
		Emergency:       req.Emergency,
		BypassReason:    req.BypassReason,
		TargetEnv:       req.TargetEnv,
		ChangeTicket:    req.ChangeTicket,
		BusinessApp:     req.BusinessApp,
		Note:            req.Note,
		Status:          domain.CommandTaskDraft,
		ExpiresAt:       draftExpiresAt(policy),
	}

	if err := s.taskRepo.Create(ctx, task); err != nil {
		return nil, err
	}

	s.recordAudit(ctx, tenantID, task.ID, userID, "command.task_created", map[string]interface{}{
		"risk_level":   effectiveRisk,
		"device_count": len(req.DeviceIDs),
		"command_type": req.CommandType,
		"draft":        true,
	})

	return s.taskToResponse(ctx, task, nil)
}

func (s *Service) SubmitTask(ctx context.Context, tenantID, userID, taskID string) (*dto.CommandTaskResponse, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil || task.TenantID != tenantID {
		return nil, domain.ErrCommandTaskNotFound
	}
	if task.Status != domain.CommandTaskDraft {
		return nil, domain.ErrCommandTaskInvalidState
	}
	if task.CreatedByUserID != userID {
		return nil, domain.ErrInsufficientPermission
	}

	req := dto.CreateCommandTaskRequest{
		Title:          task.Title,
		CommandType:    task.CommandType,
		CommandPayload: task.CommandPayload,
		DeviceIDs:      task.ParseDeviceIDs(),
		RiskLevel:      task.RiskLevel,
		Emergency:      task.Emergency,
		BypassReason:   task.BypassReason,
		TargetEnv:      task.TargetEnv,
		ChangeTicket:   task.ChangeTicket,
		BusinessApp:    task.BusinessApp,
		Note:           task.Note,
	}
	if len(req.DeviceIDs) < 1 {
		return nil, domain.ErrCommandTaskInvalidState
	}

	if err := s.validateCommandDeviceTargets(ctx, tenantID, req.DeviceIDs); err != nil {
		return nil, err
	}

	systemRisk := EvaluateRisk(req.CommandType, req.CommandPayload)
	effectiveRisk := EffectiveRisk(req.RiskLevel, systemRisk)
	task.EffectiveRisk = effectiveRisk

	deviceIDsJSON, _ := json.Marshal(req.DeviceIDs)
	task.DeviceIDsJSON = string(deviceIDsJSON)

	policy, _ := s.policyService.FindPolicy(ctx, tenantID, effectiveRisk)
	expiresMinutes := 30
	if policy != nil && policy.ExpiresMinutes > 0 {
		expiresMinutes = policy.ExpiresMinutes
	}
	task.ExpiresAt = time.Now().Add(time.Duration(expiresMinutes) * time.Minute)

	if policy != nil {
		task.PolicySnapshotID = &policy.ID
	} else {
		task.PolicySnapshotID = nil
	}

	if err := s.execRepo.DeleteByTaskID(ctx, task.ID); err != nil {
		return nil, fmt.Errorf("delete old executions: %w", err)
	}
	for _, deviceID := range req.DeviceIDs {
		exec := &domain.CommandExecution{
			ID:       ulid.Make().String(),
			TaskID:   task.ID,
			DeviceID: deviceID,
			Status:   domain.ExecutionPending,
		}
		if err := s.execRepo.Create(ctx, exec); err != nil {
			return nil, fmt.Errorf("create execution record: %w", err)
		}
	}

	bypass := s.shouldBypassApproval(ctx, tenantID, userID, req, effectiveRisk, policy)

	if bypass {
		task.BypassApproval = true
		task.Status = domain.CommandTaskApproved
		now := time.Now()
		task.ApprovedAt = &now
		task.ApproverUserID = nil
		task.ApproverRoleID = nil
		s.recordAudit(ctx, tenantID, task.ID, userID, "command.emergency_bypass", map[string]interface{}{
			"bypass_reason": req.BypassReason,
			"risk_level":    effectiveRisk,
			"device_count":  len(req.DeviceIDs),
		})
	} else if policy != nil && policy.AutoApprove {
		task.BypassApproval = false
		task.Status = domain.CommandTaskApproved
		now := time.Now()
		task.ApprovedAt = &now
		task.ApproverUserID = nil
		task.ApproverRoleID = nil
	} else {
		task.BypassApproval = false
		approverUserID, approverRoleID := s.resolveApprover(ctx, policy)
		task.ApproverUserID = approverUserID
		task.ApproverRoleID = approverRoleID
		task.Status = domain.CommandTaskPendingApproval
	}

	if err := s.taskRepo.Update(ctx, task); err != nil {
		return nil, err
	}

	s.recordAudit(ctx, tenantID, task.ID, userID, "command.task_submitted", map[string]interface{}{
		"risk_level":    effectiveRisk,
		"device_count":  len(req.DeviceIDs),
		"command_type":  req.CommandType,
		"result_status": string(task.Status),
	})

	if task.Status == domain.CommandTaskApproved {
		go s.dispatchToDevices(context.Background(), task)
	}
	return s.taskToResponse(ctx, task, nil)
}

func (s *Service) updateDraftTask(ctx context.Context, tenantID, userID string, task *domain.CommandTask, req dto.UpdateCommandTaskRequest) (*dto.CommandTaskResponse, error) {
	systemRisk := EvaluateRisk(req.CommandType, req.CommandPayload)
	effectiveRisk := EffectiveRisk(req.RiskLevel, systemRisk)
	policy, _ := s.policyService.FindPolicy(ctx, tenantID, effectiveRisk)

	if err := s.validateCommandDeviceTargets(ctx, tenantID, req.DeviceIDs); err != nil {
		return nil, err
	}

	task.Title = req.Title
	task.CommandType = req.CommandType
	task.CommandPayload = req.CommandPayload
	task.SetDeviceIDs(req.DeviceIDs)
	task.RiskLevel = req.RiskLevel
	task.EffectiveRisk = effectiveRisk
	task.Emergency = req.Emergency
	task.BypassReason = req.BypassReason
	task.TargetEnv = req.TargetEnv
	task.ChangeTicket = req.ChangeTicket
	task.BusinessApp = req.BusinessApp
	task.Note = req.Note
	task.ExpiresAt = draftExpiresAt(policy)

	task.BypassApproval = false
	task.PolicySnapshotID = nil
	task.ApproverUserID = nil
	task.ApproverRoleID = nil
	task.ApprovedByID = nil
	task.ApprovalNote = ""
	task.ApprovedAt = nil

	if err := s.taskRepo.Update(ctx, task); err != nil {
		return nil, err
	}

	s.recordAudit(ctx, tenantID, task.ID, userID, "command.task_updated", map[string]interface{}{
		"risk_level":   effectiveRisk,
		"device_count": len(req.DeviceIDs),
		"command_type": req.CommandType,
		"draft":        true,
	})

	return s.taskToResponse(ctx, task, nil)
}

func (s *Service) UpdateTask(ctx context.Context, tenantID, userID, taskID string, req dto.UpdateCommandTaskRequest) (*dto.CommandTaskResponse, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil || task.TenantID != tenantID {
		return nil, domain.ErrCommandTaskNotFound
	}
	if task.Status != domain.CommandTaskDraft && task.Status != domain.CommandTaskPendingApproval {
		return nil, domain.ErrCommandTaskInvalidState
	}
	if task.CreatedByUserID != userID {
		return nil, domain.ErrInsufficientPermission
	}

	if task.Status == domain.CommandTaskDraft {
		return s.updateDraftTask(ctx, tenantID, userID, task, req)
	}

	if time.Now().After(task.ExpiresAt) {
		task.Status = domain.CommandTaskExpired
		_ = s.taskRepo.Update(ctx, task)
		return nil, domain.ErrCommandTaskExpired
	}

	systemRisk := EvaluateRisk(req.CommandType, req.CommandPayload)
	effectiveRisk := EffectiveRisk(req.RiskLevel, systemRisk)

	policy, _ := s.policyService.FindPolicy(ctx, tenantID, effectiveRisk)
	expiresMinutes := 30
	if policy != nil && policy.ExpiresMinutes > 0 {
		expiresMinutes = policy.ExpiresMinutes
	}

	// Update task fields
	task.Title = req.Title
	task.CommandType = req.CommandType
	task.CommandPayload = req.CommandPayload
	task.SetDeviceIDs(req.DeviceIDs)
	task.RiskLevel = req.RiskLevel
	task.EffectiveRisk = effectiveRisk
	task.Emergency = req.Emergency
	task.BypassReason = req.BypassReason
	task.TargetEnv = req.TargetEnv
	task.ChangeTicket = req.ChangeTicket
	task.BusinessApp = req.BusinessApp
	task.Note = req.Note
	task.ExpiresAt = time.Now().Add(time.Duration(expiresMinutes) * time.Minute)

	// Reset approval-related fields
	task.BypassApproval = false
	task.PolicySnapshotID = nil
	task.ApproverUserID = nil
	task.ApproverRoleID = nil
	task.ApprovedByID = nil
	task.ApprovalNote = ""
	task.ApprovedAt = nil

	if policy != nil {
		task.PolicySnapshotID = &policy.ID
	}

	if err := s.validateCommandDeviceTargets(ctx, tenantID, req.DeviceIDs); err != nil {
		return nil, err
	}

	// Rebuild executions for new device list
	if err := s.execRepo.DeleteByTaskID(ctx, task.ID); err != nil {
		return nil, fmt.Errorf("delete old executions: %w", err)
	}
	for _, deviceID := range req.DeviceIDs {
		exec := &domain.CommandExecution{
			ID:       ulid.Make().String(),
			TaskID:   task.ID,
			DeviceID: deviceID,
			Status:   domain.ExecutionPending,
		}
		if err := s.execRepo.Create(ctx, exec); err != nil {
			return nil, fmt.Errorf("create execution record: %w", err)
		}
	}

	bypass := s.shouldBypassApproval(ctx, tenantID, userID, dto.CreateCommandTaskRequest{
		Title:          req.Title,
		CommandType:    req.CommandType,
		CommandPayload: req.CommandPayload,
		DeviceIDs:      req.DeviceIDs,
		RiskLevel:      req.RiskLevel,
		Emergency:      req.Emergency,
		BypassReason:   req.BypassReason,
		TargetEnv:      req.TargetEnv,
		ChangeTicket:   req.ChangeTicket,
		BusinessApp:    req.BusinessApp,
		Note:           req.Note,
	}, effectiveRisk, policy)

	if bypass {
		task.BypassApproval = true
		task.Status = domain.CommandTaskApproved
		now := time.Now()
		task.ApprovedAt = &now
	} else if policy != nil && policy.AutoApprove {
		task.Status = domain.CommandTaskApproved
		now := time.Now()
		task.ApprovedAt = &now
	} else {
		approverUserID, approverRoleID := s.resolveApprover(ctx, policy)
		task.ApproverUserID = approverUserID
		task.ApproverRoleID = approverRoleID
		task.Status = domain.CommandTaskPendingApproval
	}

	if err := s.taskRepo.Update(ctx, task); err != nil {
		return nil, err
	}

	s.recordAudit(ctx, tenantID, task.ID, userID, "command.task_updated", map[string]interface{}{
		"risk_level":   effectiveRisk,
		"device_count": len(req.DeviceIDs),
		"command_type": req.CommandType,
	})

	if task.Status == domain.CommandTaskApproved {
		go s.dispatchToDevices(context.Background(), task)
	}
	return s.taskToResponse(ctx, task, nil)
}

func (s *Service) DeleteTask(ctx context.Context, tenantID, userID, taskID string) error {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil || task.TenantID != tenantID {
		return domain.ErrCommandTaskNotFound
	}
	if task.CreatedByUserID != userID {
		return domain.ErrInsufficientPermission
	}
	// B-mode: hard delete only when the task has not been executed; otherwise archive (hide from default list).
	if task.Status == domain.CommandTaskDraft || task.Status == domain.CommandTaskPendingApproval || task.Status == domain.CommandTaskDenied || task.Status == domain.CommandTaskCancelled {
		if err := s.execRepo.DeleteByTaskID(ctx, task.ID); err != nil {
			return fmt.Errorf("delete executions: %w", err)
		}
		if err := s.taskRepo.Delete(ctx, task.ID); err != nil {
			return err
		}
		s.recordAudit(ctx, tenantID, task.ID, userID, "command.task_deleted", map[string]interface{}{
			"status": string(task.Status),
		})
		return nil
	}

	if task.ArchivedAt == nil {
		now := time.Now()
		task.ArchivedAt = &now
		if err := s.taskRepo.Update(ctx, task); err != nil {
			return err
		}
		s.recordAudit(ctx, tenantID, task.ID, userID, "command.task_archived", map[string]interface{}{
			"status": string(task.Status),
		})
	}
	return nil
}

func (s *Service) shouldBypassApproval(ctx context.Context, tenantID, userID string, req dto.CreateCommandTaskRequest, effectiveRisk string, policy *domain.ApprovalPolicy) bool {
	if req.Emergency {
		if s.rbacService != nil {
			has, _ := s.rbacService.HasPermission(ctx, userID, domain.PermCommandEmergency)
			return has
		}
		return false
	}
	if s.rbacService != nil {
		has, _ := s.rbacService.HasPermission(ctx, userID, domain.PermCommandBypassApproval)
		if has {
			return true
		}
	}
	return false
}

func (s *Service) resolveApprover(ctx context.Context, policy *domain.ApprovalPolicy) (*string, *string) {
	if policy == nil {
		return nil, nil
	}
	return policy.ApproverUserID, policy.ApproverRoleID
}

func (s *Service) mergePendingTasksForApprover(ctx context.Context, tenantID, approverUserID string) ([]*domain.CommandTask, error) {
	byUser, err := s.taskRepo.ListPendingByApprover(ctx, tenantID, approverUserID)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]*domain.CommandTask)
	for _, t := range byUser {
		seen[t.ID] = t
	}
	if s.rbacService != nil {
		roleIDs, err := s.rbacService.ListRoleIDsForUserInTenant(ctx, tenantID, approverUserID)
		if err != nil {
			return nil, err
		}
		for _, rid := range roleIDs {
			roleTasks, err := s.taskRepo.ListPendingByApproverRole(ctx, tenantID, rid)
			if err != nil {
				return nil, err
			}
			for _, t := range roleTasks {
				if _, ok := seen[t.ID]; !ok {
					seen[t.ID] = t
				}
			}
		}
	}
	out := make([]*domain.CommandTask, 0, len(seen))
	for _, t := range seen {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Service) canApproverActOnTask(ctx context.Context, task *domain.CommandTask, approverUserID string) error {
	if task.ApproverUserID != nil && *task.ApproverUserID == approverUserID {
		return nil
	}
	if task.ApproverRoleID != nil && *task.ApproverRoleID != "" && s.rbacService != nil {
		ok, err := s.rbacService.UserHasRoleInTenant(ctx, task.TenantID, approverUserID, *task.ApproverRoleID)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return domain.ErrInsufficientPermission
}

func (s *Service) ApproveTask(ctx context.Context, pathTenantID, taskID, approverUserID, note string, isPlatformSuperAdmin bool) error {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return domain.ErrCommandTaskNotFound
	}
	if !isPlatformSuperAdmin && task.TenantID != pathTenantID {
		return domain.ErrCommandTaskNotFound
	}
	if task.Status != domain.CommandTaskPendingApproval {
		return domain.ErrCommandTaskInvalidState
	}
	if time.Now().After(task.ExpiresAt) {
		task.Status = domain.CommandTaskExpired
		s.taskRepo.Update(ctx, task)
		return domain.ErrCommandTaskExpired
	}
	if !isPlatformSuperAdmin {
		if err := s.canApproverActOnTask(ctx, task, approverUserID); err != nil {
			return err
		}
	}
	// Platform super admins may approve any task for the tenant (including self-created),
	// per product intent; SoD still applies to normal tenant operators.
	if !isPlatformSuperAdmin && task.CreatedByUserID == approverUserID {
		policy, _ := s.policyService.FindPolicy(ctx, task.TenantID, task.EffectiveRisk)
		if policy != nil && policy.SeparationOfDuty {
			return domain.ErrSeparationOfDutyViolation
		}
	}

	if err := s.validateCommandDeviceTargets(ctx, task.TenantID, task.ParseDeviceIDs()); err != nil {
		return err
	}

	task.Status = domain.CommandTaskApproved
	task.ApprovedByID = &approverUserID
	task.ApprovalNote = note
	now := time.Now()
	task.ApprovedAt = &now

	if err := s.taskRepo.Update(ctx, task); err != nil {
		return err
	}

	s.recordAudit(ctx, task.TenantID, task.ID, approverUserID, "command.task_approved", map[string]interface{}{
		"note": note,
	})

	go s.dispatchToDevices(context.Background(), task)
	return nil
}

func (s *Service) DenyTask(ctx context.Context, pathTenantID, taskID, approverUserID, reason string, isPlatformSuperAdmin bool) error {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return domain.ErrCommandTaskNotFound
	}
	if !isPlatformSuperAdmin && task.TenantID != pathTenantID {
		return domain.ErrCommandTaskNotFound
	}
	if task.Status != domain.CommandTaskPendingApproval {
		return domain.ErrCommandTaskInvalidState
	}
	if !isPlatformSuperAdmin {
		if err := s.canApproverActOnTask(ctx, task, approverUserID); err != nil {
			return err
		}
	}

	task.Status = domain.CommandTaskDenied
	task.ApprovedByID = &approverUserID
	task.ApprovalNote = reason

	if err := s.taskRepo.Update(ctx, task); err != nil {
		return err
	}

	s.recordAudit(ctx, task.TenantID, task.ID, approverUserID, "command.task_denied", map[string]interface{}{
		"reason": reason,
	})

	return nil
}

func (s *Service) CancelTask(ctx context.Context, tenantID, taskID, userID string) error {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return domain.ErrCommandTaskNotFound
	}
	if task.TenantID != tenantID {
		return domain.ErrCommandTaskNotFound
	}
	if task.Status != domain.CommandTaskPendingApproval && task.Status != domain.CommandTaskDraft {
		return domain.ErrCommandTaskInvalidState
	}
	if task.CreatedByUserID != userID {
		return domain.ErrInsufficientPermission
	}

	task.Status = domain.CommandTaskCancelled
	return s.taskRepo.Update(ctx, task)
}

func (s *Service) GetTask(ctx context.Context, tenantID, taskID string) (*dto.CommandTaskResponse, error) {
	task, err := s.taskRepo.GetByID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil || task.TenantID != tenantID {
		return nil, domain.ErrCommandTaskNotFound
	}
	return s.taskToResponse(ctx, task, nil)
}

func (s *Service) ListTasks(ctx context.Context, tenantID string, filters repository.CommandTaskFilters, limit, offset int) (*dto.CommandTaskListResponse, error) {
	if limit <= 0 {
		limit = 20
	}
	tasks, total, err := s.taskRepo.ListByTenant(ctx, tenantID, filters, limit, offset)
	if err != nil {
		return nil, err
	}
	creatorNames := s.buildCreatorNameMap(ctx, tasks)
	items := make([]dto.CommandTaskResponse, 0, len(tasks))
	for _, t := range tasks {
		resp, err := s.taskToResponse(ctx, t, creatorNames)
		if err != nil {
			continue
		}
		items = append(items, *resp)
	}
	return &dto.CommandTaskListResponse{Tasks: items, Total: total}, nil
}

func (s *Service) ListPendingApprovals(ctx context.Context, tenantID, approverUserID string, isPlatformSuperAdmin bool) ([]dto.CommandTaskResponse, error) {
	var tasks []*domain.CommandTask
	var err error
	if isPlatformSuperAdmin {
		tasks, err = s.taskRepo.ListPendingInTenant(ctx, tenantID)
	} else {
		tasks, err = s.mergePendingTasksForApprover(ctx, tenantID, approverUserID)
	}
	if err != nil {
		return nil, err
	}
	creatorNames := s.buildCreatorNameMap(ctx, tasks)
	items := make([]dto.CommandTaskResponse, 0, len(tasks))
	for _, t := range tasks {
		resp, err := s.taskToResponse(ctx, t, creatorNames)
		if err != nil {
			continue
		}
		items = append(items, *resp)
	}
	return items, nil
}

func (s *Service) CountPendingApprovals(ctx context.Context, tenantID, approverUserID string, isPlatformSuperAdmin bool) (int64, error) {
	if isPlatformSuperAdmin {
		return s.taskRepo.CountPendingInTenant(ctx, tenantID)
	}
	tasks, err := s.mergePendingTasksForApprover(ctx, tenantID, approverUserID)
	if err != nil {
		return 0, err
	}
	return int64(len(tasks)), nil
}

func (s *Service) HandleExecutionResult(ctx context.Context, executionID string, status string, output *string, exitCode *int, durationMs *int) error {
	exec, err := s.execRepo.GetByID(ctx, executionID)
	if err != nil || exec == nil {
		return fmt.Errorf("execution not found: %s", executionID)
	}

	exec.Status = domain.CommandExecutionStatus(status)
	exec.Output = output
	exec.ExitCode = exitCode
	exec.DurationMs = durationMs
	now := time.Now()
	exec.FinishedAt = &now

	if err := s.execRepo.Update(ctx, exec); err != nil {
		return err
	}

	s.updateTaskStatus(ctx, exec.TaskID)
	return nil
}

func (s *Service) updateTaskStatus(ctx context.Context, taskID string) {
	execs, err := s.execRepo.ListByTaskID(ctx, taskID)
	if err != nil {
		return
	}

	allDone := true
	anyFailed := false
	anySucceeded := false
	for _, e := range execs {
		switch e.Status {
		case domain.ExecutionSucceeded:
			anySucceeded = true
		case domain.ExecutionFailed, domain.ExecutionTimeout:
			anyFailed = true
		case domain.ExecutionSkipped:
			// skip
		default:
			allDone = false
		}
	}

	if !allDone {
		task, _ := s.taskRepo.GetByID(ctx, taskID)
		if task != nil && task.Status == domain.CommandTaskApproved {
			task.Status = domain.CommandTaskExecuting
			s.taskRepo.Update(ctx, task)
		}
		return
	}

	task, _ := s.taskRepo.GetByID(ctx, taskID)
	if task == nil {
		return
	}

	now := time.Now()
	task.CompletedAt = &now

	if anyFailed && anySucceeded {
		task.Status = domain.CommandTaskPartialDone
	} else if anyFailed {
		task.Status = domain.CommandTaskFailed
	} else {
		task.Status = domain.CommandTaskCompleted
	}

	s.taskRepo.Update(ctx, task)
}

func (s *Service) skipExecution(ctx context.Context, exec *domain.CommandExecution, reason string) {
	exec.Status = domain.ExecutionSkipped
	exec.ErrorMessage = &reason
	_ = s.execRepo.Update(ctx, exec)
}

func (s *Service) dispatchToDevices(ctx context.Context, task *domain.CommandTask) {
	deviceIDs := task.ParseDeviceIDs()
	for _, deviceID := range deviceIDs {
		exec, _ := s.execRepo.GetByTaskAndDevice(ctx, task.ID, deviceID)
		if exec == nil {
			continue
		}

		if s.deviceRepo != nil {
			dev, err := s.deviceRepo.GetByID(ctx, deviceID)
			if err != nil {
				slog.Error("[command] device lookup failed", "device_id", deviceID, "error", err)
				s.skipExecution(ctx, exec, fmt.Sprintf("device lookup failed: %v", err))
				continue
			}
			if dev == nil {
				s.skipExecution(ctx, exec, "device not found or deleted")
				continue
			}
			if dev.TenantID != task.TenantID {
				s.skipExecution(ctx, exec, "device tenant mismatch")
				continue
			}
			if dev.Status != domain.DeviceStatusActive {
				s.skipExecution(ctx, exec, fmt.Sprintf("device not active: status=%s", dev.Status))
				continue
			}
		}

		exec.Status = domain.ExecutionSent
		now := time.Now()
		exec.SentAt = &now
		s.execRepo.Update(ctx, exec)

		if s.gatewayClient != nil {
			evt := infrastructure.SessionEvent{
				EventID:   fmt.Sprintf("cmd_%s", exec.ID),
				EventType: "command.execute",
				TenantID:  task.TenantID,
				DeviceID:  deviceID,
				SessionID: task.ID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Payload: map[string]interface{}{
					"task_id":         task.ID,
					"execution_id":    exec.ID,
					"command_type":    task.CommandType,
					"command_payload": task.CommandPayload,
				},
			}
			if err := s.gatewayClient.SendToDevice(ctx, deviceID, evt); err != nil {
				slog.Error("[command] Failed to dispatch to device", "device_id", deviceID, "error", err)
				exec.Status = domain.ExecutionSkipped
				errMsg := err.Error()
				exec.ErrorMessage = &errMsg
				s.execRepo.Update(ctx, exec)
			}
		}
	}
	s.updateTaskStatus(ctx, task.ID)
}

func (s *Service) recordAudit(ctx context.Context, tenantID, taskID, actorID, eventType string, data map[string]interface{}) {
	if s.auditRepo == nil {
		return
	}
	data["task_id"] = taskID
	payloadJSON, _ := json.Marshal(data)
	event := &domain.AuditEvent{
		ID:               ulid.Make().String(),
		TenantID:         tenantID,
		SessionID:        &taskID,
		EventType:        eventType,
		EventPayloadJSON: string(payloadJSON),
	}
	if err := s.auditRepo.Create(ctx, event); err != nil {
		slog.Error("[command] Failed to record audit", "event_type", eventType, "error", err)
	}
}

func (s *Service) taskToResponse(ctx context.Context, task *domain.CommandTask, creatorNames map[string]string) (*dto.CommandTaskResponse, error) {
	resp := &dto.CommandTaskResponse{
		ID:             task.ID,
		TenantID:       task.TenantID,
		CreatedBy:      task.CreatedByUserID,
		CreatedByName:  s.resolveCreatedByName(ctx, task.CreatedByUserID, creatorNames),
		ApproverID:     task.ApproverUserID,
		ApprovedBy:     task.ApprovedByID,
		PolicySnapshotID: task.PolicySnapshotID,
		Title:          task.Title,
		CommandType:    task.CommandType,
		CommandPayload: task.CommandPayload,
		DeviceIDs:      task.ParseDeviceIDs(),
		RiskLevel:      task.RiskLevel,
		EffectiveRisk:  task.EffectiveRisk,
		BypassApproval: task.BypassApproval,
		Emergency:      task.Emergency,
		TargetEnv:      task.TargetEnv,
		ChangeTicket:   task.ChangeTicket,
		BusinessApp:    task.BusinessApp,
		Note:           task.Note,
		Status:         string(task.Status),
		ApprovalNote:   task.ApprovalNote,
		ExpiresAt:      task.ExpiresAt,
		ApprovedAt:     task.ApprovedAt,
		CompletedAt:    task.CompletedAt,
		ArchivedAt:     task.ArchivedAt,
		CreatedAt:      task.CreatedAt,
		UpdatedAt:      task.UpdatedAt,
	}

	execs, _ := s.execRepo.ListByTaskID(ctx, task.ID)
	for _, e := range execs {
		resp.Executions = append(resp.Executions, dto.CommandExecutionResponse{
			ID:         e.ID,
			TaskID:     e.TaskID,
			DeviceID:   e.DeviceID,
			Status:     string(e.Status),
			Output:     e.Output,
			Error:      e.ErrorMessage,
			ExitCode:   e.ExitCode,
			DurationMs: e.DurationMs,
			SentAt:     e.SentAt,
			StartedAt:  e.StartedAt,
			FinishedAt: e.FinishedAt,
			CreatedAt:  e.CreatedAt,
		})
	}
	return resp, nil
}
