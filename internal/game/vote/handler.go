package vote

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"game-live-room/internal/bilibili"
	"game-live-room/internal/config"
	"game-live-room/internal/hub"
	"game-live-room/internal/model"
	"game-live-room/internal/store"

	"go.uber.org/zap"
)

// Handler manages the gift vote game logic.
type Handler struct {
	store  *store.Store
	hub    *hub.Hub
	config *config.VoteConfig
	logger *zap.Logger

	mu        sync.Mutex
	active    bool
	sessionID string
	scores    map[string]int // slotID -> score (in-memory cache)
}

// New creates a new vote Handler.
func New(s *store.Store, h *hub.Hub, cfg *config.VoteConfig, logger *zap.Logger) *Handler {
	return &Handler{
		store:  s,
		hub:    h,
		config: cfg,
		logger: logger,
		scores: make(map[string]int),
	}
}

// Start begins a new vote session.
func (h *Handler) Start(sessionID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.active {
		return fmt.Errorf("vote already active")
	}

	cfgJSON, _ := json.Marshal(h.config)
	sess := &model.VoteSession{
		ID:        sessionID,
		Status:    "active",
		Config:    string(cfgJSON),
		StartedAt: time.Now(),
	}
	if err := h.store.CreateVoteSession(sess); err != nil {
		return fmt.Errorf("create vote session: %w", err)
	}

	h.active = true
	h.sessionID = sessionID
	h.scores = make(map[string]int)

	h.broadcastScores()
	return nil
}

// Stop ends the current vote session.
func (h *Handler) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active {
		return fmt.Errorf("no active vote session")
	}

	now := time.Now()
	sess := &model.VoteSession{
		ID:      h.sessionID,
		Status:  "ended",
		EndedAt: &now,
	}
	if err := h.store.UpdateVoteSession(sess); err != nil {
		return fmt.Errorf("update vote session: %w", err)
	}

	h.active = false
	h.broadcastScores()
	return nil
}

// Reset clears all scores for the current session.
func (h *Handler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.scores = make(map[string]int)
	h.broadcastScores()
}

// HandleGift processes a SEND_GIFT message and adds score to the matching slot.
func (h *Handler) HandleGift(msg bilibili.LiveMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active || !h.config.Enabled {
		return
	}

	for _, slot := range h.config.Slots {
		if slot.GiftName == msg.GiftName {
			score := msg.GiftCount
			h.scores[slot.ID] += score

			record := &model.VoteRecord{
				SessionID: h.sessionID,
				SlotID:    slot.ID,
				UID:       msg.UID,
				Username:  msg.Username,
				GiftName:  msg.GiftName,
				GiftCount: msg.GiftCount,
				Score:     score,
				CreatedAt: time.Now(),
			}
			if err := h.store.SaveVoteRecord(record); err != nil {
				h.logger.Warn("save vote record failed", zap.Error(err))
			}

			h.broadcastScores()
			return
		}
	}
}

// GetScores returns a copy of the current scores.
func (h *Handler) GetScores() map[string]int {
	h.mu.Lock()
	defer h.mu.Unlock()
	result := make(map[string]int, len(h.scores))
	for k, v := range h.scores {
		result[k] = v
	}
	return result
}

// IsActive returns whether a vote session is currently active.
func (h *Handler) IsActive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active
}

// SessionID returns the current session ID.
func (h *Handler) SessionID() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sessionID
}

func (h *Handler) broadcastScores() {
	payload, _ := json.Marshal(map[string]interface{}{
		"session_id": h.sessionID,
		"active":     h.active,
		"scores":     h.scores,
		"slots":      h.config.Slots,
	})
	h.hub.Broadcast(hub.Message{
		Type:    "game_state",
		Payload: json.RawMessage(fmt.Sprintf(`{"game":"vote","state":%s}`, payload)),
	})
}
