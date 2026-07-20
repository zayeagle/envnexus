package feishu

import (
	"context"
	"fmt"
	"strings"

	"github.com/zayeagle/envnexus/services/platform-api/internal/repository"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/session"
)

// RegisterDefaultCommands registers all built-in bot commands.
func RegisterDefaultCommands(
	bot *BotService,
	bridge *ChatBridge,
	deviceRepo repository.DeviceRepository,
	sessionService *session.Service,
	auditRepo repository.AuditRepository,
) {
	bot.RegisterCommand("help", func(ctx context.Context, cmd BotCommand) (string, error) {
		return bot.helpText(), nil
	})

	bot.RegisterCommand("bind", func(ctx context.Context, cmd BotCommand) (string, error) {
		deviceID := strings.TrimSpace(cmd.Args)
		if deviceID == "" {
			return "用法: `/bind <device_id>`\n\n绑定后，直接发送消息即可对该设备发起诊断。", nil
		}
		if deviceRepo == nil {
			return "⚠️ 数据库未连接", nil
		}

		device, err := deviceRepo.GetByID(ctx, deviceID)
		if err != nil || device == nil {
			return fmt.Sprintf("❌ 设备 `%s` 不存在，请检查 device_id", deviceID), nil
		}

		existing := bridge.GetBinding(ctx, cmd.ChatID)
		if existing != nil && existing.DeviceID == deviceID {
			return fmt.Sprintf("📎 当前群已绑定设备 `%s`，无需重复绑定", deviceID), nil
		}

		binding := bridge.Bind(ctx, cmd.ChatID, deviceID, device.TenantID, cmd.UserID)

		hostname := device.Platform
		if device.Hostname != nil {
			hostname = *device.Hostname
		}

		return fmt.Sprintf(
			"✅ 绑定成功！\n\n"+
				"**设备**: `%s` (%s / %s)\n"+
				"**状态**: %s\n\n"+
				"现在可以直接在群里发消息进行诊断，例如:\n"+
				"  → \"网络连不上了\"\n"+
				"  → \"磁盘空间不够了\"\n"+
				"  → \"DNS 解析失败\"\n\n"+
				"诊断结果和审批卡片会自动推送到本群。",
			binding.DeviceID, hostname, device.Arch, string(device.Status),
		), nil
	})

	bot.RegisterCommand("unbind", func(ctx context.Context, cmd BotCommand) (string, error) {
		existing := bridge.GetBinding(ctx, cmd.ChatID)
		if existing == nil {
			return "当前群未绑定任何设备", nil
		}
		deviceID := existing.DeviceID
		bridge.Unbind(ctx, cmd.ChatID)
		return fmt.Sprintf("✅ 已解绑设备 `%s`\n\n直接发消息将不再触发诊断。", deviceID), nil
	})

	bot.RegisterCommand("who", func(ctx context.Context, cmd BotCommand) (string, error) {
		binding := bridge.GetBinding(ctx, cmd.ChatID)
		if binding == nil {
			return "当前群未绑定任何设备。使用 `/bind <device_id>` 开始。", nil
		}
		return FormatBindingInfo(binding), nil
	})

	bot.RegisterCommand("status", func(ctx context.Context, cmd BotCommand) (string, error) {
		return "✅ EnvNexus 平台运行正常", nil
	})

	bot.RegisterCommand("devices", func(ctx context.Context, cmd BotCommand) (string, error) {
		if deviceRepo == nil {
			return "⚠️ 数据库未连接", nil
		}
		tenantID := cmd.TenantID
		if tenantID == "" {
			tenantID = "default"
		}
		devices, _, err := deviceRepo.ListByTenantID(ctx, tenantID, false, false, 0, 0)
		if err != nil {
			return "", fmt.Errorf("查询设备: %w", err)
		}
		if len(devices) == 0 {
			return "当前无已注册设备", nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📱 **已注册设备** (%d 台)\n\n", len(devices)))
		for i, d := range devices {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("... 及另外 %d 台\n", len(devices)-20))
				break
			}
			status := "🟢"
			if d.Status == "revoked" || d.Status == "retired" {
				status = "🔴"
			} else if d.Status == "quarantined" {
				status = "🟡"
			}
			hostname := d.Platform
			if d.Hostname != nil {
				hostname = *d.Hostname
			}
			sb.WriteString(fmt.Sprintf("%s `%s` — %s (%s)\n", status, d.ID, hostname, d.Platform))
		}
		sb.WriteString("\n使用 `/bind <device_id>` 绑定设备开始对话式诊断")
		return sb.String(), nil
	})

	bot.RegisterCommand("pending", func(ctx context.Context, cmd BotCommand) (string, error) {
		binding := bridge.GetBinding(ctx, cmd.ChatID)
		if binding != nil && binding.SessionID != "" && sessionService != nil {
			approval, err := sessionService.GetPendingApproval(ctx, binding.SessionID)
			if err == nil && approval != nil {
				return fmt.Sprintf(
					"📋 **当前会话待审批**\n\n"+
						"审批 ID: `%s`\n"+
						"风险等级: %s\n\n"+
						"回复 `/approve %s` 或 `/deny %s` 处理",
					approval.ID, approval.RiskLevel, approval.ID, approval.ID,
				), nil
			}
		}
		return "📋 当前无待审批请求。\n如有新的审批请求，会自动推送到本群。", nil
	})

	bot.RegisterCommand("approve", func(ctx context.Context, cmd BotCommand) (string, error) {
		approvalID := strings.TrimSpace(cmd.Args)
		if approvalID == "" {
			return "用法: `/approve <approval_id>`", nil
		}
		if sessionService == nil {
			return "⚠️ 会话服务不可用", nil
		}
		userID := cmd.UserID
		if userID == "" {
			userID = "feishu_user"
		}

		approval, err := sessionService.GetApprovalByID(ctx, approvalID)
		if err != nil {
			return "", fmt.Errorf("查询审批: %w", err)
		}

		err = sessionService.ApproveSession(ctx, approval.SessionID, approvalID, userID, "通过飞书审批")
		if err != nil {
			return "", fmt.Errorf("审批操作失败: %w", err)
		}
		return fmt.Sprintf("✅ 审批 `%s` 已批准", approvalID), nil
	})

	bot.RegisterCommand("deny", func(ctx context.Context, cmd BotCommand) (string, error) {
		parts := strings.SplitN(cmd.Args, " ", 2)
		if len(parts) == 0 || parts[0] == "" {
			return "用法: `/deny <approval_id> [原因]`", nil
		}
		approvalID := parts[0]
		reason := "通过飞书拒绝"
		if len(parts) > 1 {
			reason = parts[1]
		}
		if sessionService == nil {
			return "⚠️ 会话服务不可用", nil
		}

		approval, err := sessionService.GetApprovalByID(ctx, approvalID)
		if err != nil {
			return "", fmt.Errorf("查询审批: %w", err)
		}

		err = sessionService.DenySession(ctx, approval.SessionID, approvalID, reason)
		if err != nil {
			return "", fmt.Errorf("拒绝操作失败: %w", err)
		}
		return fmt.Sprintf("❌ 审批 `%s` 已拒绝（原因: %s）", approvalID, reason), nil
	})

	bot.RegisterCommand("audit", func(ctx context.Context, cmd BotCommand) (string, error) {
		deviceID := strings.TrimSpace(cmd.Args)
		// If no device_id provided, use the bound device
		if deviceID == "" {
			binding := bridge.GetBinding(ctx, cmd.ChatID)
			if binding != nil {
				deviceID = binding.DeviceID
			}
		}
		if deviceID == "" {
			return "用法: `/audit [device_id]`\n绑定设备后可省略 device_id", nil
		}
		if auditRepo == nil {
			return "⚠️ 数据库未连接", nil
		}

		tenantID := cmd.TenantID
		if tenantID == "" {
			tenantID = "default"
		}
		filters := repository.AuditFilters{DeviceID: deviceID}
		events, _, err := auditRepo.ListByTenant(ctx, tenantID, filters, 1, 10)
		if err != nil {
			return "", fmt.Errorf("查询审计: %w", err)
		}
		if len(events) == 0 {
			return fmt.Sprintf("设备 `%s` 无审计记录", deviceID), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📝 **设备 %s 最近审计事件**\n\n", deviceID))
		limit := 10
		if len(events) < limit {
			limit = len(events)
		}
		for _, evt := range events[:limit] {
			idShort := evt.ID
			if len(idShort) > 8 {
				idShort = idShort[:8]
			}
			sb.WriteString(fmt.Sprintf("• `%s` %s (%s)\n", evt.EventType, evt.CreatedAt.Format("01-02 15:04"), idShort))
		}
		if len(events) > 10 {
			sb.WriteString(fmt.Sprintf("\n... 共 %d 条记录", len(events)))
		}
		return sb.String(), nil
	})
}
