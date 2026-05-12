package sc

import (
	"encoding/json"

	"game-live-room/internal/bilibili"
	"game-live-room/internal/config"
	"game-live-room/internal/hub"

	"go.uber.org/zap"
)

// Handler processes SuperChat trigger events.
type Handler struct {
	hub    *hub.Hub
	config *config.SCTriggerConfig
	logger *zap.Logger
}

// New creates a new SC Handler.
func New(h *hub.Hub, cfg *config.SCTriggerConfig, logger *zap.Logger) *Handler {
	return &Handler{hub: h, config: cfg, logger: logger}
}

// SetMinPrice updates the minimum SC price threshold at runtime.
func (h *Handler) SetMinPrice(price int) {
	h.config.MinPrice = price
}

// HandleSuperChat processes a SUPER_CHAT_MESSAGE and triggers display if above threshold.
func (h *Handler) HandleSuperChat(msg bilibili.LiveMessage) {
	if !h.config.Enabled {
		return
	}
	if msg.Price < h.config.MinPrice {
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"uid":      msg.UID,
		"username": msg.Username,
		"text":     msg.Text,
		"price":    msg.Price,
	})
	h.hub.Broadcast(hub.Message{
		Type:    "live_event",
		Payload: json.RawMessage(payload),
	})

	togglePayload, _ := json.Marshal(map[string]interface{}{
		"module":  "sc-alert",
		"visible": true,
	})
	h.hub.BroadcastTo(hub.ClientOverlay, hub.Message{
		Type:    "module_toggle",
		Payload: json.RawMessage(togglePayload),
	})

	h.logger.Info("SC trigger fired",
		zap.String("user", msg.Username),
		zap.Int("price", msg.Price))
}
