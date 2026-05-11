package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"game-live-room/internal/bilibili"
	"game-live-room/internal/config"
	"game-live-room/internal/game"
	"game-live-room/internal/hub"
	"game-live-room/internal/model"
	"game-live-room/internal/store"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "init logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	db, err := gorm.Open(mysql.Open(cfg.MySQL.DSN()), &gorm.Config{})
	if err != nil {
		logger.Fatal("connect mysql", zap.Error(err))
	}

	st := store.New(db)
	if err := st.AutoMigrate(); err != nil {
		logger.Fatal("auto migrate", zap.Error(err))
	}

	h := hub.New()
	engine := game.New(st, h, &cfg.Game, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go h.Run(ctx)

	danmakuClient := bilibili.NewDanmakuClient(
		cfg.Bilibili.RoomID,
		cfg.Bilibili.Cookie,
		cfg.Bilibili.UserAgent,
		cfg.Bilibili.DanmakuWSURL,
		logger,
	)
	danmakuClient.OnDanmaku = func(msg bilibili.LiveMessage) {
		engine.HandleLiveMessage(msg)
	}
	go danmakuClient.Connect(ctx)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: buildRouter(h, engine, st, logger),
	}

	go func() {
		logger.Info("server starting", zap.Int("port", cfg.Server.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down")
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", zap.Error(err))
	}
}

func buildRouter(h *hub.Hub, engine *game.Engine, st *store.Store, logger *zap.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("/ws", h)
	mux.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.Dir("web/admin"))))
	mux.Handle("/overlay/", http.StripPrefix("/overlay/", http.FileServer(http.Dir("web/overlay"))))

	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/game/state", handleGameState(engine))
	mux.HandleFunc("/api/game/start", handleGameStart(engine))
	mux.HandleFunc("/api/game/stop", handleGameStop(engine))
	mux.HandleFunc("/api/quiz/questions", handleQuizQuestions(st))
	mux.HandleFunc("/api/quiz/questions/", handleQuizQuestion(st))
	mux.HandleFunc("/api/vote/scores", handleVoteScores(engine, st))
	mux.HandleFunc("/api/vote/reset", handleVoteReset(engine))
	mux.HandleFunc("/api/events", handleEvents(st))

	return mux
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleGameState(engine *game.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"active_game": engine.ActiveGame(),
		})
	}
}

func handleGameStart(engine *game.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Game   string                 `json:"game"`
			Params map[string]interface{} `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp(err))
			return
		}
		if err := engine.HandleAdminCmd("start_"+body.Game, body.Params); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp(err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleGameStop(engine *game.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		active := engine.ActiveGame()
		if active == "" {
			writeJSON(w, http.StatusOK, map[string]string{"status": "no active game"})
			return
		}
		if err := engine.HandleAdminCmd("stop_"+active, nil); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp(err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleQuizQuestions(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			qType := r.URL.Query().Get("type")
			questions, err := st.GetQuizQuestions(qType, 0)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, errResp(err))
				return
			}
			writeJSON(w, http.StatusOK, questions)
		case http.MethodPost:
			var q model.QuizQuestion
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
				writeJSON(w, http.StatusBadRequest, errResp(err))
				return
			}
			if err := st.CreateQuizQuestion(&q); err != nil {
				writeJSON(w, http.StatusInternalServerError, errResp(err))
				return
			}
			writeJSON(w, http.StatusOK, q)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func handleQuizQuestion(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.URL.Path[len("/api/quiz/questions/"):]
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp(fmt.Errorf("invalid id")))
			return
		}
		switch r.Method {
		case http.MethodPut:
			var q model.QuizQuestion
			if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
				writeJSON(w, http.StatusBadRequest, errResp(err))
				return
			}
			q.ID = id
			if err := st.UpdateQuizQuestion(&q); err != nil {
				writeJSON(w, http.StatusInternalServerError, errResp(err))
				return
			}
			writeJSON(w, http.StatusOK, q)
		case http.MethodDelete:
			if err := st.DeleteQuizQuestion(id); err != nil {
				writeJSON(w, http.StatusInternalServerError, errResp(err))
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

func handleVoteScores(engine *game.Engine, st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		vh := engine.VoteHandler()
		if vh.IsActive() {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"session_id": vh.SessionID(),
				"scores":     vh.GetScores(),
			})
			return
		}
		sessionID := r.URL.Query().Get("session_id")
		if sessionID == "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{"scores": map[string]int{}})
			return
		}
		scores, err := st.GetVoteScores(sessionID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp(err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"session_id": sessionID,
			"scores":     scores,
		})
	}
}

func handleVoteReset(engine *game.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		engine.VoteHandler().Reset()
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleEvents(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limitStr := r.URL.Query().Get("limit")
		limit := 100
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				limit = l
			}
		}
		cmd := r.URL.Query().Get("cmd")
		events, err := st.GetRecentEvents(limit, cmd)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp(err))
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func errResp(err error) map[string]string {
	return map[string]string{"error": err.Error()}
}
