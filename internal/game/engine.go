package game

import (
	"fmt"
	"time"

	"game-live-room/internal/bilibili"
	"game-live-room/internal/config"
	"game-live-room/internal/game/gift"
	"game-live-room/internal/game/quiz"
	"game-live-room/internal/game/sc"
	"game-live-room/internal/game/vote"
	"game-live-room/internal/hub"
	"game-live-room/internal/model"
	"game-live-room/internal/store"

	"go.uber.org/zap"
)

// Engine dispatches live messages to game handlers and processes admin commands.
type Engine struct {
	store  *store.Store
	hub    *hub.Hub
	config *config.GameConfig
	logger *zap.Logger

	gift *gift.Handler
	sc   *sc.Handler
	quiz *quiz.Handler
	vote *vote.Handler

	activeGame string // "" / "quiz" / "vote"
}

// New creates a new game Engine.
func New(s *store.Store, h *hub.Hub, cfg *config.GameConfig, logger *zap.Logger) *Engine {
	return &Engine{
		store:  s,
		hub:    h,
		config: cfg,
		logger: logger,
		gift:   gift.New(h, &cfg.GiftTrigger, logger),
		sc:     sc.New(h, &cfg.SCTrigger, logger),
		quiz:   quiz.New(s, h, &cfg.Quiz, logger),
		vote:   vote.New(s, h, &cfg.Vote, logger),
	}
}

// HandleLiveMessage dispatches a live message to the appropriate game handlers.
func (e *Engine) HandleLiveMessage(msg bilibili.LiveMessage) {
	event := &model.LiveEvent{
		Cmd:        msg.Cmd,
		UID:        msg.UID,
		Username:   msg.Username,
		Text:       msg.Text,
		GiftName:   msg.GiftName,
		GiftCount:  msg.GiftCount,
		Price:      msg.Price,
		GuardLevel: msg.GuardLevel,
	}
	if err := e.store.SaveEvent(event); err != nil {
		e.logger.Warn("save event failed", zap.Error(err))
	}

	switch msg.Cmd {
	case "DANMU_MSG":
		e.quiz.HandleDanmaku(msg)
	case "SEND_GIFT":
		e.gift.HandleGift(msg)
		e.vote.HandleGift(msg)
	case "SUPER_CHAT_MESSAGE":
		e.sc.HandleSuperChat(msg)
	}
}

// HandleAdminCmd processes a command from the admin panel.
func (e *Engine) HandleAdminCmd(action string, params map[string]interface{}) error {
	switch action {
	case "start_quiz":
		return e.startQuiz(params)
	case "stop_quiz":
		e.quiz.Stop()
		e.activeGame = ""
		return nil
	case "start_vote":
		return e.startVote(params)
	case "stop_vote":
		err := e.vote.Stop()
		if err == nil {
			e.activeGame = ""
		}
		return err
	case "reset_vote":
		e.vote.Reset()
		return nil
	default:
		return fmt.Errorf("unknown action: %s", action)
	}
}

func (e *Engine) startQuiz(params map[string]interface{}) error {
	idVal, ok := params["question_id"]
	if !ok {
		return fmt.Errorf("missing question_id")
	}
	var questionID uint64
	switch v := idVal.(type) {
	case float64:
		questionID = uint64(v)
	case uint64:
		questionID = v
	default:
		return fmt.Errorf("invalid question_id type")
	}
	if err := e.quiz.Start(questionID); err != nil {
		return err
	}
	e.activeGame = "quiz"
	return nil
}

func (e *Engine) startVote(params map[string]interface{}) error {
	sessionID, _ := params["session_id"].(string)
	if sessionID == "" {
		sessionID = fmt.Sprintf("vote_%d", time.Now().Unix())
	}
	if err := e.vote.Start(sessionID); err != nil {
		return err
	}
	e.activeGame = "vote"
	return nil
}

// ActiveGame returns the name of the currently active game.
func (e *Engine) ActiveGame() string {
	return e.activeGame
}

// VoteHandler returns the vote handler for direct access.
func (e *Engine) VoteHandler() *vote.Handler {
	return e.vote
}
