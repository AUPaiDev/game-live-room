package quiz

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"game-live-room/internal/bilibili"
	"game-live-room/internal/config"
	"game-live-room/internal/hub"
	"game-live-room/internal/model"
	"game-live-room/internal/store"

	"go.uber.org/zap"
)

// Handler manages the quiz game logic.
type Handler struct {
	store  *store.Store
	hub    *hub.Hub
	config *config.QuizConfig
	logger *zap.Logger

	mu       sync.Mutex
	active   bool
	currentQ *model.QuizQuestion
	session  *model.QuizSession
	answers  map[uint64]int
	timer    *time.Timer
}

// New creates a new quiz Handler.
func New(s *store.Store, h *hub.Hub, cfg *config.QuizConfig, logger *zap.Logger) *Handler {
	return &Handler{
		store:  s,
		hub:    h,
		config: cfg,
		logger: logger,
	}
}

// Start begins a quiz round with the given question ID.
func (h *Handler) Start(questionID uint64) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.active {
		return fmt.Errorf("quiz already active")
	}

	q, err := h.store.GetQuizQuestionByID(questionID)
	if err != nil {
		return fmt.Errorf("get question: %w", err)
	}

	sess := &model.QuizSession{
		QuestionID: questionID,
		StartedAt:  time.Now(),
	}
	if err := h.store.SaveQuizSession(sess); err != nil {
		return fmt.Errorf("save session: %w", err)
	}

	h.active = true
	h.currentQ = q
	h.session = sess
	h.answers = make(map[uint64]int)

	h.broadcastQuestion()

	timeout := time.Duration(h.config.AnswerTimeout) * time.Second
	h.timer = time.AfterFunc(timeout, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.active {
			h.endRound()
		}
	})
	return nil
}

// Stop forcefully ends the current quiz round.
func (h *Handler) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.active {
		return
	}
	if h.timer != nil {
		h.timer.Stop()
	}
	h.endRound()
}

// HandleDanmaku processes a danmaku message as a quiz answer.
func (h *Handler) HandleDanmaku(msg bilibili.LiveMessage) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.active || h.currentQ == nil {
		return
	}
	if _, already := h.answers[msg.UID]; already {
		return
	}

	idx := parseAnswer(msg.Text)
	if idx < 0 {
		return
	}
	h.answers[msg.UID] = idx
}

// endRound finalizes the quiz round. Must be called with h.mu held.
func (h *Handler) endRound() {
	now := time.Now()
	h.session.EndedAt = &now
	h.session.TotalAnswers = len(h.answers)

	correct := 0
	for _, ans := range h.answers {
		if ans == h.currentQ.Answer {
			correct++
		}
	}
	h.session.CorrectCount = correct

	_ = h.store.UpdateQuizSession(h.session)

	h.broadcastResult()
	h.active = false
}

func (h *Handler) broadcastQuestion() {
	var options []string
	if err := json.Unmarshal([]byte(h.currentQ.Options), &options); err != nil || options == nil {
		options = []string{}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"id":       h.currentQ.ID,
		"type":     h.currentQ.Type,
		"question": h.currentQ.Question,
		"options":  options,
		"timeout":  h.config.AnswerTimeout,
		"media":    h.currentQ.MediaPath,
	})
	h.hub.Broadcast(hub.Message{
		Type:    "game_state",
		Payload: json.RawMessage(fmt.Sprintf(`{"game":"quiz","state":%s}`, payload)),
	})

	togglePayload, _ := json.Marshal(map[string]interface{}{
		"module":  "quiz-display",
		"visible": true,
	})
	h.hub.BroadcastTo(hub.ClientOverlay, hub.Message{
		Type:    "module_toggle",
		Payload: json.RawMessage(togglePayload),
	})
}

func (h *Handler) broadcastResult() {
	total := h.session.TotalAnswers
	correct := h.session.CorrectCount
	ratio := 0.0
	if total > 0 {
		ratio = float64(correct) / float64(total)
	}

	var options []string
	if err := json.Unmarshal([]byte(h.currentQ.Options), &options); err != nil || options == nil {
		options = []string{}
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"question":      h.currentQ.Question,
		"answer":        h.currentQ.Answer,
		"options":       options,
		"explanation":   h.currentQ.Explanation,
		"total_answers": total,
		"correct_count": correct,
		"correct_ratio": ratio,
	})
	h.hub.Broadcast(hub.Message{
		Type:    "game_state",
		Payload: json.RawMessage(fmt.Sprintf(`{"game":"quiz_result","state":%s}`, payload)),
	})

	togglePayload, _ := json.Marshal(map[string]interface{}{
		"module":  "quiz-display",
		"visible": false,
	})
	h.hub.BroadcastTo(hub.ClientOverlay, hub.Message{
		Type:    "module_toggle",
		Payload: json.RawMessage(togglePayload),
	})

	resultPayload, _ := json.Marshal(map[string]interface{}{
		"module":  "quiz-result",
		"visible": true,
	})
	h.hub.BroadcastTo(hub.ClientOverlay, hub.Message{
		Type:    "module_toggle",
		Payload: json.RawMessage(resultPayload),
	})
}

// parseAnswer converts a danmaku text to a 0-based option index.
// Accepts "1"/"2"/"3"/"4" or "A"/"B"/"C"/"D" (case-insensitive).
func parseAnswer(text string) int {
	text = strings.TrimSpace(text)
	switch strings.ToUpper(text) {
	case "1", "A":
		return 0
	case "2", "B":
		return 1
	case "3", "C":
		return 2
	case "4", "D":
		return 3
	}
	return -1
}

// IsActive returns whether a quiz round is currently active.
func (h *Handler) IsActive() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.active
}
