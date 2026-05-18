package mic

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

// Participant is the in-memory representation of a mic queue participant.
type Participant struct {
	UID      uint64 `json:"uid"`
	Username string `json:"username"`
	Avatar   string `json:"avatar"`
	Score    int    `json:"score"`
	OnMic    bool   `json:"on_mic"`
	MicCount int    `json:"mic_count"`
}

// Handler manages the mic queue karaoke game logic.
type Handler struct {
	store  *store.Store
	hub    *hub.Hub
	config *config.MicConfig
	logger *zap.Logger

	mu            sync.Mutex
	active        bool
	sessionID     string
	keyword       string
	giftName      string
	participants  map[uint64]*Participant
	currentSinger *Participant
}

// New creates a new mic Handler.
func New(s *store.Store, h *hub.Hub, cfg *config.MicConfig, logger *zap.Logger) *Handler {
	return &Handler{
		store:        s,
		hub:          h,
		config:       cfg,
		logger:       logger,
		participants: make(map[uint64]*Participant),
	}
}

// Start begins a new mic session with the given parameters.
func (h *Handler) Start(sessionID, keyword, giftName string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.active {
		return fmt.Errorf("mic session already active")
	}

	sess := &model.MicSession{
		ID:        sessionID,
		Status:    "active",
		Keyword:   keyword,
		GiftName:  giftName,
		StartedAt: time.Now(),
	}
	if err := h.store.CreateMicSession(sess); err != nil {
		return fmt.Errorf("create mic session: %w", err)
	}

	h.active = true
	h.sessionID = sessionID
	h.keyword = keyword
	h.giftName = giftName
	h.participants = make(map[uint64]*Participant)
	h.currentSinger = nil

	h.broadcastState()
	return nil
}

// Stop ends the current mic session.
func (h *Handler) Stop() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active {
		return fmt.Errorf("no active mic session")
	}

	now := time.Now()
	sess := &model.MicSession{
		ID:      h.sessionID,
		Status:  "ended",
		EndedAt: &now,
	}
	if err := h.store.UpdateMicSession(sess); err != nil {
		return fmt.Errorf("update mic session: %w", err)
	}

	h.active = false
	h.currentSinger = nil
	h.broadcastState()
	return nil
}

// HandleDanmaku processes a DANMU_MSG and registers new participants by keyword.
func (h *Handler) HandleDanmaku(msg bilibili.LiveMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active || msg.Text != h.keyword {
		return
	}
	if _, exists := h.participants[msg.UID]; exists {
		return
	}

	p := &Participant{
		UID:      msg.UID,
		Username: msg.Username,
	}
	h.participants[msg.UID] = p

	dbP := &model.MicParticipant{
		SessionID:    h.sessionID,
		UID:          msg.UID,
		Username:     msg.Username,
		RegisteredAt: time.Now(),
	}
	if err := h.store.UpsertMicParticipant(dbP); err != nil {
		h.logger.Warn("upsert mic participant failed", zap.Error(err))
	}

	h.broadcastState()
}

// HandleGift processes a SEND_GIFT and adds score to the current singer.
func (h *Handler) HandleGift(msg bilibili.LiveMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active || h.currentSinger == nil || msg.GiftName != h.giftName {
		return
	}

	scoreAdd := msg.GiftCount
	h.currentSinger.Score += scoreAdd
	if p, ok := h.participants[h.currentSinger.UID]; ok {
		p.Score = h.currentSinger.Score
	}

	if err := h.store.UpdateMicParticipantScore(h.sessionID, h.currentSinger.UID, scoreAdd); err != nil {
		h.logger.Warn("update mic participant score failed", zap.Error(err))
	}

	record := &model.MicGiftRecord{
		SessionID:     h.sessionID,
		SingerUID:     h.currentSinger.UID,
		DonorUID:      msg.UID,
		DonorUsername: msg.Username,
		GiftName:      msg.GiftName,
		GiftCount:     msg.GiftCount,
		Score:         scoreAdd,
		CreatedAt:     time.Now(),
	}
	if err := h.store.SaveMicGiftRecord(record); err != nil {
		h.logger.Warn("save mic gift record failed", zap.Error(err))
	}

	h.broadcastState()
}

// AssignSinger sets the given uid as the current on-mic singer.
func (h *Handler) AssignSinger(uid uint64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active {
		return fmt.Errorf("no active mic session")
	}
	p, ok := h.participants[uid]
	if !ok {
		return fmt.Errorf("uid %d not in participant list", uid)
	}

	if err := h.store.ClearMicOnMic(h.sessionID); err != nil {
		return fmt.Errorf("clear on_mic: %w", err)
	}
	for _, participant := range h.participants {
		participant.OnMic = false
	}

	if err := h.store.SetMicOnMic(h.sessionID, uid, true, true); err != nil {
		return fmt.Errorf("set on_mic: %w", err)
	}
	p.OnMic = true
	p.MicCount++
	h.currentSinger = p

	h.broadcastState()
	return nil
}

// UnassignSinger clears the current on-mic singer.
func (h *Handler) UnassignSinger() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active {
		return fmt.Errorf("no active mic session")
	}

	if err := h.store.ClearMicOnMic(h.sessionID); err != nil {
		return fmt.Errorf("clear on_mic: %w", err)
	}
	for _, p := range h.participants {
		p.OnMic = false
	}
	h.currentSinger = nil

	h.broadcastState()
	return nil
}

// KickParticipant removes a participant from the queue.
func (h *Handler) KickParticipant(uid uint64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active {
		return fmt.Errorf("no active mic session")
	}
	if _, ok := h.participants[uid]; !ok {
		return fmt.Errorf("uid %d not in participant list", uid)
	}

	if h.currentSinger != nil && h.currentSinger.UID == uid {
		if err := h.store.ClearMicOnMic(h.sessionID); err != nil {
			h.logger.Warn("clear on_mic on kick failed", zap.Error(err))
		}
		h.currentSinger = nil
	}
	delete(h.participants, uid)

	h.broadcastState()
	return nil
}

// IsActive returns whether a mic session is currently active.
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

// GetState returns the full current state for API/WS consumption.
func (h *Handler) GetState() map[string]interface{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.buildState()
}

func (h *Handler) buildState() map[string]interface{} {
	participants := make([]*Participant, 0, len(h.participants))
	for _, p := range h.participants {
		participants = append(participants, p)
	}
	return map[string]interface{}{
		"active":         h.active,
		"session_id":     h.sessionID,
		"keyword":        h.keyword,
		"gift_name":      h.giftName,
		"current_singer": h.currentSinger,
		"participants":   participants,
		"queue_count":    len(participants),
	}
}

func (h *Handler) broadcastState() {
	state := h.buildState()
	stateJSON, _ := json.Marshal(state)
	h.hub.Broadcast(hub.Message{
		Type:    "game_state",
		Payload: json.RawMessage(fmt.Sprintf(`{"game":"mic","state":%s}`, stateJSON)),
	})
}
