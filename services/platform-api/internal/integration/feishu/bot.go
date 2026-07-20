package feishu

import (
	"context"
	"log/slog"
	"strings"

	"github.com/zayeagle/envnexus/services/platform-api/internal/dto"
	"github.com/zayeagle/envnexus/services/platform-api/internal/service/session"
)

// BotCommand represents a parsed command from a Feishu chat message.
type BotCommand struct {
	Command  string
	Args     string
	ChatID   string
	UserID   string
	TenantID string
}

// CommandHandler processes bot commands and returns a text response.
type CommandHandler func(ctx context.Context, cmd BotCommand) (string, error)

// BotService manages conversational interaction between Feishu chats and
// EnvNexus agents. When a chat is bound to a device, any non-command message
// is treated as a natural-language diagnosis request.
type BotService struct {
	client         *Client
	bridge         *ChatBridge
	sessionService *session.Service
	handlers       map[string]CommandHandler
}

func NewBotService(client *Client, bridge *ChatBridge, sessionService *session.Service) *BotService {
	return &BotService{
		client:         client,
		bridge:         bridge,
		sessionService: sessionService,
		handlers:       make(map[string]CommandHandler),
	}
}

// RegisterCommand registers a slash command handler.
func (b *BotService) RegisterCommand(name string, handler CommandHandler) {
	b.handlers[strings.ToLower(name)] = handler
}

// HandleMessage processes an incoming chat message.
// Slash commands (/bind, /help, ...) are routed to handlers.
// Plain text in a bound chat is forwarded as a diagnosis request.
func (b *BotService) HandleMessage(ctx context.Context, chatID, userID, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	// Strip bot @mention prefix
	if strings.HasPrefix(text, "@") {
		if idx := strings.Index(text, " "); idx > 0 {
			text = strings.TrimSpace(text[idx:])
		}
	}

	// Check if it's a slash command
	if strings.HasPrefix(text, "/") {
		return b.dispatchCommand(ctx, chatID, userID, text)
	}

	// Not a command — try conversational diagnosis
	return b.handleConversation(ctx, chatID, userID, text)
}

func (b *BotService) dispatchCommand(ctx context.Context, chatID, userID, text string) string {
	parts := strings.SplitN(text, " ", 2)
	cmdName := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	handler, ok := b.handlers[cmdName]
	if !ok {
		return b.helpText()
	}

	cmd := BotCommand{
		Command: cmdName,
		Args:    args,
		ChatID:  chatID,
		UserID:  userID,
	}

	resp, err := handler(ctx, cmd)
	if err != nil {
		slog.Error("[feishu-bot] Command failed", "command", cmdName, "error", err)
		return "❌ 命令执行失败: " + err.Error()
	}
	return resp
}

// handleConversation treats plain text as a diagnosis request when the chat
// is bound to a device. Creates a session, sends the message as the initial
// diagnosis request, and lets EventSink push results back asynchronously.
func (b *BotService) handleConversation(ctx context.Context, chatID, userID, text string) string {
	binding := b.bridge.GetBinding(ctx, chatID)
	if binding == nil {
		return "💡 当前聊天未绑定设备，无法发送诊断指令。\n\n" +
			"请先使用 `/bind <device_id>` 绑定设备，之后直接发消息即可开始诊断。\n\n" +
			"输入 `/help` 查看所有命令。"
	}

	if b.sessionService == nil {
		return "⚠️ 会话服务不可用"
	}

	result, err := b.sessionService.CreateSession(ctx, dto.CreateSessionRequest{
		DeviceID:       binding.DeviceID,
		Transport:      "websocket",
		InitiatorType:  "feishu",
		InitialMessage: text,
	})
	if err != nil {
		slog.Error("[feishu-bot] Create session failed", "device_id", binding.DeviceID, "error", err)
		return "❌ 创建诊断会话失败: " + err.Error()
	}

	b.bridge.TrackSession(ctx, chatID, result.Session.ID)

	slog.Info("[feishu-bot] Conversational diagnosis started",
		"chat_id", chatID,
		"device_id", binding.DeviceID,
		"session_id", result.Session.ID,
		"message", text,
	)

	return "🚀 已向设备 `" + binding.DeviceID + "` 发起诊断\n" +
		"会话: `" + result.Session.ID + "`\n\n" +
		"**你的问题**: " + text + "\n\n" +
		"_诊断进行中，结果会自动推送到本群..._"
}

func (b *BotService) helpText() string {
	lines := []string{
		"🤖 **EnvNexus Bot**",
		"",
		"**对话模式** — 绑定设备后，直接发消息即可诊断:",
		"  `/bind <device_id>` — 绑定设备到当前群",
		"  `/unbind` — 解绑当前设备",
		"  `/who` — 查看当前绑定信息",
		"  然后直接输入问题，例如: \"网络连不上了\"",
		"",
		"**管理命令**:",
		"  `/status` — 查看平台运行状态",
		"  `/devices` — 列出已注册设备",
		"  `/pending` — 查看待审批请求",
		"  `/approve <id>` — 批准修复操作",
		"  `/deny <id> [原因]` — 拒绝修复操作",
		"  `/audit <device_id>` — 查看审计事件",
		"  `/help` — 显示此帮助信息",
	}
	return strings.Join(lines, "\n")
}

// NotifyApprovalRequest sends an approval card to the chat bound to the device.
func (b *BotService) NotifyApprovalRequest(ctx context.Context, tenantID, deviceID, sessionID, approvalID, toolName, riskLevel, actionDesc string) {
	if b.client == nil || !b.client.Enabled() {
		return
	}
	chatID := b.bridge.ChatForSession(ctx, sessionID)
	if chatID == "" {
		chatID = b.bridge.ChatForDevice(ctx, deviceID)
	}
	if chatID == "" {
		return
	}
	card := NewApprovalCard(tenantID, deviceID, sessionID, approvalID, toolName, riskLevel, actionDesc)
	if err := b.client.SendInteractiveCard(ctx, "chat_id", chatID, card); err != nil {
		slog.Error("[feishu-bot] Failed to send approval card", "approval_id", approvalID, "error", err)
	}
}

// NotifyAlert sends an alert card to the chat bound to the device.
func (b *BotService) NotifyAlert(ctx context.Context, deviceID, alertType, message string) {
	if b.client == nil || !b.client.Enabled() {
		return
	}
	chatID := b.bridge.ChatForDevice(ctx, deviceID)
	if chatID == "" {
		return
	}
	card := NewAlertCard(deviceID, alertType, message)
	if err := b.client.SendInteractiveCard(ctx, "chat_id", chatID, card); err != nil {
		slog.Error("[feishu-bot] Failed to send alert card", "device_id", deviceID, "error", err)
	}
}
