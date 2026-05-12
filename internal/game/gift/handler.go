package gift

import (
	"encoding/json"

	"game-live-room/internal/bilibili"
	"game-live-room/internal/config"
	"game-live-room/internal/hub"

	"go.uber.org/zap"
)

// Handler processes gift trigger events.
type Handler struct {
	hub    *hub.Hub
	config *config.GiftTriggerConfig
	logger *zap.Logger
}

// New creates a new gift Handler.
func New(h *hub.Hub, cfg *config.GiftTriggerConfig, logger *zap.Logger) *Handler {
	return &Handler{hub: h, config: cfg, logger: logger}
}

// HandleGift processes a SEND_GIFT message and triggers configured actions.
func (h *Handler) HandleGift(msg bilibili.LiveMessage) {
	if !h.config.Enabled {
		return
	}
	for _, rule := range h.config.Rules {
		if rule.GiftName == msg.GiftName {
			h.triggerAction(msg, rule)
			return
		}
	}
}

// TriggerManual fires the configured action for the given gift name without a live event.
func (h *Handler) TriggerManual(giftName string) {
	if !h.config.Enabled {
		return
	}
	for _, rule := range h.config.Rules {
		if rule.GiftName == giftName {
			h.triggerAction(bilibili.LiveMessage{
				GiftName: giftName,
				Username: "手动触发",
			}, rule)
			return
		}
	}
}

func (h *Handler) triggerAction(msg bilibili.LiveMessage, rule config.GiftRule) {
	payload, _ := json.Marshal(map[string]interface{}{
		"uid":       msg.UID,
		"username":  msg.Username,
		"gift_name": msg.GiftName,
		"count":     msg.GiftCount,
		"price":     msg.Price,
		"action":    rule.Action,
		"params":    rule.Params,
	})
	h.hub.Broadcast(hub.Message{
		Type:    "live_event",
		Payload: json.RawMessage(payload),
	})

	togglePayload, _ := json.Marshal(map[string]interface{}{
		"module":  "gift-alert",
		"visible": true,
	})
	h.hub.BroadcastTo(hub.ClientOverlay, hub.Message{
		Type:    "module_toggle",
		Payload: json.RawMessage(togglePayload),
	})

	h.logger.Info("gift trigger fired",
		zap.String("gift", msg.GiftName),
		zap.String("action", rule.Action),
		zap.String("user", msg.Username))
}
