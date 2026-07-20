package feishu

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	mw "github.com/zayeagle/envnexus/services/platform-api/internal/middleware"
)

// Handler handles Feishu event callbacks, interactive card actions,
// and an internal API for pushing session events to Feishu chats.
type Handler struct {
	bot               *BotService
	eventSink         *EventSink
	verificationToken string
}

func NewHandler(bot *BotService, eventSink *EventSink, verificationToken string) *Handler {
	return &Handler{
		bot:               bot,
		eventSink:         eventSink,
		verificationToken: verificationToken,
	}
}

// RegisterRoutes registers Feishu webhook routes and the internal event push endpoint.
func (h *Handler) RegisterRoutes(router *gin.RouterGroup) {
	router.POST("/feishu/event", h.HandleEvent)
	router.POST("/feishu/card", h.HandleCardAction)
	router.POST("/feishu/push", h.HandleEventPush)
}

// HandleEvent processes Feishu event subscription callbacks.
func (h *Handler) HandleEvent(c *gin.Context) {
	var raw map[string]interface{}
	if err := c.ShouldBindJSON(&raw); err != nil {
		mw.RespondValidationError(c, "invalid json")
		return
	}

	// URL verification challenge — Feishu requires the exact {"challenge":"..."} format.
	if challenge, ok := raw["challenge"].(string); ok {
		if token, ok := raw["token"].(string); ok && token == h.verificationToken {
			c.JSON(http.StatusOK, gin.H{"challenge": challenge})
			return
		}
		mw.RespondErrorCode(c, http.StatusForbidden, "token_mismatch", "verification token mismatch")
		return
	}

	schemaVer, _ := raw["schema"].(string)
	if schemaVer == "2.0" {
		h.handleV2Event(c, raw)
		return
	}

	h.handleV1Event(c, raw)
}

func (h *Handler) handleV2Event(c *gin.Context, raw map[string]interface{}) {
	headerRaw, _ := raw["header"].(map[string]interface{})
	eventType, _ := headerRaw["event_type"].(string)

	if token, _ := headerRaw["token"].(string); token != h.verificationToken {
		mw.RespondErrorCode(c, http.StatusForbidden, "token_mismatch", "verification token mismatch")
		return
	}

	eventData, _ := raw["event"].(map[string]interface{})

	switch eventType {
	case "im.message.receive_v1":
		h.handleMessageEvent(c, eventData)
	default:
		slog.Info("[feishu] Unhandled v2 event type", "event_type", eventType)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func (h *Handler) handleV1Event(c *gin.Context, raw map[string]interface{}) {
	eventData, _ := raw["event"].(map[string]interface{})
	eventType, _ := eventData["type"].(string)

	switch eventType {
	case "message":
		h.handleMessageEvent(c, eventData)
	default:
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func (h *Handler) handleMessageEvent(c *gin.Context, event map[string]interface{}) {
	message, _ := event["message"].(map[string]interface{})
	sender, _ := event["sender"].(map[string]interface{})

	chatID := ""
	userID := ""
	text := ""

	if message != nil {
		chatID, _ = message["chat_id"].(string)
		msgType, _ := message["message_type"].(string)
		if msgType == "text" {
			contentStr, _ := message["content"].(string)
			var content map[string]string
			if json.Unmarshal([]byte(contentStr), &content) == nil {
				text = content["text"]
			}
		}
	}

	if sender != nil {
		senderID, _ := sender["sender_id"].(map[string]interface{})
		if senderID != nil {
			userID, _ = senderID["open_id"].(string)
		}
	}

	// v1 fallback
	if text == "" {
		text, _ = event["text"].(string)
	}
	if chatID == "" {
		chatID, _ = event["open_chat_id"].(string)
	}
	if userID == "" {
		userID, _ = event["open_id"].(string)
	}

	if text == "" {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	reply := h.bot.HandleMessage(c.Request.Context(), chatID, userID, text)

	if reply != "" && chatID != "" {
		go func() {
			_ = h.bot.client.SendTextMessage(c.Request.Context(), "chat_id", chatID, reply)
		}()
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// HandleCardAction processes interactive card button clicks (approve/deny).
func (h *Handler) HandleCardAction(c *gin.Context) {
	var payload struct {
		OpenID        string `json:"open_id"`
		UserID        string `json:"user_id"`
		OpenMessageID string `json:"open_message_id"`
		Token         string `json:"token"`
		Action        struct {
			Value map[string]string `json:"value"`
			Tag   string            `json:"tag"`
		} `json:"action"`
	}

	if err := c.ShouldBindJSON(&payload); err != nil {
		mw.RespondValidationError(c, "invalid json")
		return
	}

	if payload.Token != h.verificationToken {
		mw.RespondErrorCode(c, http.StatusForbidden, "token_mismatch", "verification token mismatch")
		return
	}

	actionValue := payload.Action.Value
	action := actionValue["action"]
	approvalID := actionValue["approval_id"]
	sessionID := actionValue["session_id"]

	slog.Info("[feishu] Card action received",
		"action", action,
		"approval_id", approvalID,
		"session_id", sessionID,
		"user", payload.OpenID,
	)

	var cmdName string
	switch action {
	case "approve":
		cmdName = "approve"
	case "deny":
		cmdName = "deny"
	default:
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	handler, ok := h.bot.handlers[cmdName]
	if !ok {
		c.JSON(http.StatusOK, gin.H{})
		return
	}

	cmd := BotCommand{
		Command: cmdName,
		Args:    approvalID,
		UserID:  payload.OpenID,
	}

	_, err := handler(c.Request.Context(), cmd)
	if err != nil {
		slog.Error("[feishu] Card action handler failed", "action", action, "error", err)
	}

	statusText := "✅ 已批准"
	statusColor := "green"
	if action == "deny" {
		statusText = "❌ 已拒绝"
		statusColor = "red"
	}
	if err != nil {
		statusText = "⚠️ 操作失败: " + err.Error()
		statusColor = "orange"
	}

	c.JSON(http.StatusOK, gin.H{
		"config": gin.H{"wide_screen_mode": true},
		"header": gin.H{
			"title":    gin.H{"tag": "plain_text", "content": statusText},
			"template": statusColor,
		},
		"elements": []gin.H{
			{"tag": "div", "text": gin.H{
				"tag":     "lark_md",
				"content": fmt.Sprintf("审批 `%s` 已由飞书用户处理\n会话: %s", approvalID, sessionID),
			}},
		},
	})
}

// HandleEventPush is an internal endpoint for platform-api services to push
// session events to Feishu chats.
func (h *Handler) HandleEventPush(c *gin.Context) {
	var evt SessionEventPayload
	if err := c.ShouldBindJSON(&evt); err != nil {
		mw.RespondValidationError(c, "invalid json")
		return
	}

	if h.eventSink != nil {
		go h.eventSink.HandleEvent(c.Request.Context(), evt)
	}

	mw.RespondSuccess(c, http.StatusAccepted, gin.H{"status": "accepted"})
}

// VerifySignature checks the Feishu event callback signature.
func VerifySignature(timestamp, nonce, encryptKey, bodyStr, signature string) bool {
	content := timestamp + nonce + encryptKey + bodyStr
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", hash) == signature
}
