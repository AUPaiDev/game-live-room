package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
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

	// Prefer DB cookie over config cookie at startup.
	startCookie := cfg.Bilibili.Cookie
	if dbCookie, err := st.GetActiveCookie(); err == nil && dbCookie != nil {
		startCookie = bilibili.BuildCookieHeader(dbCookie)
		logger.Info("using DB cookie for danmaku", zap.String("uname", dbCookie.Label))
	}

	danmakuClient := bilibili.NewDanmakuClient(
		cfg.Bilibili.RoomID,
		startCookie,
		cfg.Bilibili.UserAgent,
		cfg.Bilibili.DanmakuWSURL,
		logger,
	)
	// Dynamically pick up newly saved cookies on reconnect.
	danmakuClient.SetCookieFunc(func() string {
		if c, err := st.GetActiveCookie(); err == nil && c != nil {
			return bilibili.BuildCookieHeader(c)
		}
		return ""
	})
	danmakuClient.OnDanmaku = func(msg bilibili.LiveMessage) {
		engine.HandleLiveMessage(msg)
	}
	go danmakuClient.Connect(ctx)

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: buildRouter(h, engine, st, danmakuClient, logger, cfg.Bilibili.UserAgent, cfg.Server.AdminToken),
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

func buildRouter(h *hub.Hub, engine *game.Engine, st *store.Store, dc *bilibili.DanmakuClient, logger *zap.Logger, userAgent string, adminToken string) http.Handler {
	mux := http.NewServeMux()

	// /ws: overlay connections are unauthenticated; admin connections require token.
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("type") != "overlay" {
			if adminToken != "" {
				token := ""
				if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
					token = strings.TrimPrefix(auth, "Bearer ")
				} else if q := r.URL.Query().Get("token"); q != "" {
					token = q
				}
				if token != adminToken {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
					return
				}
			}
		}
		h.ServeHTTP(w, r)
	})

	mux.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.Dir("web/admin"))))
	mux.Handle("/overlay/", http.StripPrefix("/overlay/", http.FileServer(http.Dir("web/overlay"))))

	// All /api/* routes are protected by adminAuth.
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/health", handleHealth)
	apiMux.HandleFunc("/api/game/state", handleGameState(engine))
	apiMux.HandleFunc("/api/game/start", handleGameStart(engine))
	apiMux.HandleFunc("/api/game/stop", handleGameStop(engine))
	apiMux.HandleFunc("/api/quiz/questions", handleQuizQuestions(st))
	apiMux.HandleFunc("/api/quiz/questions/", handleQuizQuestion(st))
	apiMux.HandleFunc("/api/vote/scores", handleVoteScores(engine, st))
	apiMux.HandleFunc("/api/vote/reset", handleVoteReset(engine))
	apiMux.HandleFunc("/api/events", handleEvents(st))
	apiMux.HandleFunc("/api/gift/trigger", handleGiftTrigger(engine))
	apiMux.HandleFunc("/api/sc/config", handleSCConfig(engine))

	apiMux.HandleFunc("/api/auth/danmaku-token", handleDanmakuToken(dc))

	// Auth: B站 QR code login
	apiMux.HandleFunc("/api/auth/qr/generate", handleQRCodeGenerate(userAgent))
	apiMux.HandleFunc("/api/auth/qr/poll", handleQRCodePoll(st, userAgent))
	apiMux.HandleFunc("/api/auth/cookies", handleCookies(st))
	apiMux.HandleFunc("/api/auth/cookies/", handleCookieOp(st))

	mux.Handle("/api/", adminAuth(adminToken, apiMux))

	return mux
}

func adminAuth(adminToken string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if adminToken == "" {
			next.ServeHTTP(w, r)
			return
		}
		token := ""
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			token = strings.TrimPrefix(auth, "Bearer ")
		} else if q := r.URL.Query().Get("token"); q != "" {
			token = q
		}
		if token != adminToken {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
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
		const maxLimit = 500
		limitStr := r.URL.Query().Get("limit")
		limit := 100
		if limitStr != "" {
			if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
				if l > maxLimit {
					l = maxLimit
				}
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

func handleGiftTrigger(engine *game.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			GiftName string `json:"gift_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp(err))
			return
		}
		if body.GiftName == "" {
			writeJSON(w, http.StatusBadRequest, errResp(fmt.Errorf("missing gift_name")))
			return
		}
		engine.TriggerGift(body.GiftName)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleSCConfig(engine *game.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			MinPrice int `json:"min_price"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp(err))
			return
		}
		engine.SetSCMinPrice(body.MinPrice)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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

func handleQRCodeGenerate(userAgent string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := bilibili.GenerateQRCode(userAgent)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp(err))
			return
		}
		writeJSON(w, http.StatusOK, data)
	}
}

func handleQRCodePoll(st *store.Store, userAgent string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			writeJSON(w, http.StatusBadRequest, errResp(fmt.Errorf("missing key")))
			return
		}
		result, err := bilibili.PollQRCode(key, userAgent)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp(err))
			return
		}
		// On success, verify and persist the cookie.
		if result.Status == 0 && result.SESSDATA != "" {
			info, err := bilibili.VerifyCookie(result.SESSDATA, result.BiliJCT, result.DedeUserID, userAgent)
			if err == nil && info.IsValid {
				// Fetch a buvid3 fingerprint to include with the cookie.
				// This helps bypass B站 risk control when calling APIs from a server IP.
				buvid3 := bilibili.FetchBuvid3(userAgent)
				c := &model.BiliCookie{
					Label:      info.Uname,
					SESSDATA:   result.SESSDATA,
					BiliJCT:    result.BiliJCT,
					DedeUserID: result.DedeUserID,
					Face:       info.Face,
					Buvid3:     buvid3,
				}
				_ = st.SaveCookie(c)
			}
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// cookieSafeView is a DTO that exposes only non-sensitive cookie fields.
type cookieSafeView struct {
	ID         uint64 `json:"ID"`
	Label      string `json:"Label"`
	Face       string `json:"Face"`
	DedeUserID string `json:"DedeUserID"`
	IsActive   bool   `json:"IsActive"`
	IsValid    bool   `json:"IsValid"`
}

func handleCookies(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookies, err := st.ListCookies()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp(err))
			return
		}
		views := make([]cookieSafeView, 0, len(cookies))
		for _, c := range cookies {
			views = append(views, cookieSafeView{
				ID:         c.ID,
				Label:      c.Label,
				Face:       c.Face,
				DedeUserID: c.DedeUserID,
				IsActive:   c.IsActive,
				IsValid:    c.IsValid,
			})
		}
		writeJSON(w, http.StatusOK, views)
	}
}

func handleCookieOp(st *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Path is either /api/auth/cookies/{id} or /api/auth/cookies/{id}/activate
		suffix := r.URL.Path[len("/api/auth/cookies/"):]
		if strings.HasSuffix(suffix, "/activate") {
			idStr := strings.TrimSuffix(suffix, "/activate")
			id, err := strconv.ParseUint(idStr, 10, 64)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, errResp(fmt.Errorf("invalid id")))
				return
			}
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			if err := st.ActivateCookie(uint(id)); err != nil {
				writeJSON(w, http.StatusInternalServerError, errResp(err))
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			return
		}
		// Plain /api/auth/cookies/{id} — only DELETE supported
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id, err := strconv.ParseUint(suffix, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errResp(fmt.Errorf("invalid id")))
			return
		}
		if err := st.DeleteCookie(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, errResp(err))
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleDanmakuToken accepts a manually-provided danmaku token.
// Used when the server IP is blocked from calling getDanmuInfo (-352).
// The token can be obtained from the browser's network tab when visiting the live room.
func handleDanmakuToken(dc *bilibili.DanmakuClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errResp(err))
			return
		}
		if body.Token == "" {
			writeJSON(w, http.StatusBadRequest, errResp(fmt.Errorf("missing token")))
			return
		}
		dc.SetManualToken(body.Token)
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
